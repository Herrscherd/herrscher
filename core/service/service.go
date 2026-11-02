// Package service installs the `herrscher serve` daemon as a native, boot-started
// background service on Linux (systemd user unit), macOS (launchd LaunchAgent),
// and Windows (Task Scheduler onlogon task).
//
// The design separates a pure planner (BuildPlan / BuildUninstall, testable on
// any OS) from the executor (Install / Uninstall, which writes files and runs
// the platform commands). Secrets never live in the generated unit: every
// platform sources an env file (mode 0600) that holds the gateway secrets et al.,
// and the planner only ever creates that file as an empty template. It never
// overwrites an existing one and never echoes a token.
package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

const (
	linuxUnitName = "herrscher.service"
	macLabel      = "com.vskstudio.herrscher"
	winTaskName   = "herrscher"
)

// Config describes the service to install.
type Config struct {
	GOOS       string
	BinPath    string
	Home       string
	User       string
	EnvFile    string
	HealthAddr string
	ExtraArgs  []string
	SkipStart  bool

	ConfigPath string
	DefaultCmd string

	// Path is the PATH the daemon, and so every agent it spawns, runs with.
	// Captured from the installing shell, because a launcher supplies almost
	// nothing (systemd hands a user service /usr/local/bin:/usr/bin) and an agent
	// cannot invoke a tool it cannot find. Empty falls back to the usual dirs.
	Path string

	// EnvVars are the secret env vars the daemon needs, declared by the compiled-in
	// gateways (from their manifests) plus the core owner id. The planner renders
	// the secrets template and the ready-to-start check from these, so the service
	// package never names a concrete gateway's variables itself. DefaultConfig
	// populates it from the plugin registry.
	EnvVars []EnvVar
}

// EnvVar is one secret the daemon reads from its env file, sourced from a
// gateway manifest's declared config. The planner turns each into a template
// line; Required ones gate whether install starts the service immediately.
type EnvVar struct {
	Key      string
	Help     string
	Required bool
}

// FileWrite is one file the plan writes. Template files are written only when
// missing (so an install never clobbers the user's secrets).
type FileWrite struct {
	Path     string
	Content  string
	Mode     os.FileMode
	Template bool
}

// Command is one shell command the plan runs. IgnoreErr commands are best-effort
// (e.g. unloading a service that isn't loaded yet).
type Command struct {
	Argv      []string
	IgnoreErr bool
}

// Plan is the full set of side effects for an install or uninstall.
type Plan struct {
	Files    []FileWrite
	Commands []Command
	Notes    []string
}

func goos(c Config) string {
	if c.GOOS != "" {
		return c.GOOS
	}
	return runtime.GOOS
}

func serveArgs(c Config) []string {
	args := []string{"serve"}
	if c.EnvFile != "" {
		args = append(args, "--env-file", c.EnvFile)
	}
	return append(args, c.ExtraArgs...)
}

const (
	privateFileMode = 0o600
	privateDirMode  = 0o750
	unitFileMode    = 0o644
)

var (
	errUnsupportedOS     = errors.New("unsupported OS")
	errNoSourceDir       = errors.New("no source dir set")
	errNotSourceCheckout = errors.New("not a herrscher source checkout")
	errNoGoToolchain     = errors.New("go toolchain not found in PATH")
	errNoBinaryPath      = errors.New("no binary path to smoke-test")
)

var coreEnvVars = []EnvVar{
	{Key: "HERRSCHER_OWNER_ID", Help: "owner id (per-daemon instance-id fallback)"},
}

func renderEnvTemplate(vars []EnvVar) string {
	var b strings.Builder
	b.WriteString("# herrscher daemon secrets: keep private (chmod 600), never commit.\n")
	b.WriteString("# Fill these in, then restart the service.\n")
	for _, v := range vars {
		if v.Help != "" {
			fmt.Fprintf(&b, "# %s\n", v.Help)
		}
		fmt.Fprintf(&b, "%s=\n", v.Key)
	}
	return b.String()
}

func envFileWrite(c Config) FileWrite {
	return FileWrite{Path: c.EnvFile, Content: renderEnvTemplate(c.EnvVars), Mode: privateFileMode, Template: true}
}

// BuildPlan returns the install plan for c's target OS. Every platform also
// scaffolds the declarative config.json (template, never clobbered).
func BuildPlan(c Config) (Plan, error) {
	var p Plan
	switch goos(c) {
	case "linux":
		p = linuxPlan(c)
	case "darwin":
		p = macPlan(c)
	case "windows":
		p = windowsPlan(c)
	default:
		return Plan{}, fmt.Errorf("%w %q", errUnsupportedOS, goos(c))
	}
	if c.ConfigPath != "" {
		p.Files = append(p.Files, configFileWrite(c))
	}
	return p, nil
}

func configFileWrite(c Config) FileWrite {
	return FileWrite{Path: c.ConfigPath, Content: config.Template(c.DefaultCmd, c.HealthAddr), Mode: unitFileMode, Template: true}
}

// BuildUninstall returns the uninstall plan for c's target OS.
func BuildUninstall(c Config) (Plan, error) {
	switch goos(c) {
	case "linux":
		unit := filepath.Join(c.Home, ".config", "systemd", "user", linuxUnitName)
		return Plan{Commands: []Command{
			{Argv: []string{"systemctl", "--user", "disable", "--now", linuxUnitName}, IgnoreErr: true},
			{Argv: []string{"rm", "-f", unit}},
			{Argv: []string{"systemctl", "--user", "daemon-reload"}, IgnoreErr: true},
		}}, nil
	case "darwin":
		plist := filepath.Join(c.Home, "Library", "LaunchAgents", macLabel+".plist")
		return Plan{Commands: []Command{
			{Argv: []string{"launchctl", "unload", "-w", plist}, IgnoreErr: true},
			{Argv: []string{"rm", "-f", plist}},
		}}, nil
	case "windows":
		return Plan{Commands: []Command{
			{Argv: []string{"schtasks", "/delete", "/tn", winTaskName, "/f"}},
		}}, nil
	default:
		return Plan{}, fmt.Errorf("%w %q", errUnsupportedOS, goos(c))
	}
}

// StatusCommand returns the command that reports whether the service is active.
func StatusCommand(c Config) (Command, error) {
	switch goos(c) {
	case "linux":
		return Command{Argv: []string{"systemctl", "--user", "status", linuxUnitName}, IgnoreErr: true}, nil
	case "darwin":
		return Command{Argv: []string{"launchctl", "list", macLabel}, IgnoreErr: true}, nil
	case "windows":
		return Command{Argv: []string{"schtasks", "/query", "/tn", winTaskName, "/v"}, IgnoreErr: true}, nil
	default:
		return Command{}, fmt.Errorf("%w %q", errUnsupportedOS, goos(c))
	}
}

// RestartCommands returns the platform commands that restart the running
// service. Some platforms need more than one (stop then start).
func RestartCommands(c Config) ([]Command, error) {
	switch goos(c) {
	case "linux":
		return []Command{{Argv: []string{"systemctl", "--user", "restart", linuxUnitName}}}, nil
	case "darwin":
		target := fmt.Sprintf("gui/%d/%s", os.Getuid(), macLabel)
		return []Command{{Argv: []string{"launchctl", "kickstart", "-k", target}}}, nil
	case "windows":
		return []Command{
			{Argv: []string{"schtasks", "/end", "/tn", winTaskName}, IgnoreErr: true},
			{Argv: []string{"schtasks", "/run", "/tn", winTaskName}},
		}, nil
	default:
		return nil, fmt.Errorf("%w %q", errUnsupportedOS, goos(c))
	}
}

// Restart restarts the service inline (for a separate caller process, e.g. the
// `herrscher service restart` CLI, never the daemon restarting itself).
func Restart(ctx context.Context, c Config) error {
	cmds, err := RestartCommands(c)
	if err != nil {
		return err
	}
	for _, cmd := range cmds {
		if err := runCommand(ctx, cmd); err != nil {
			return err
		}
	}
	return nil
}

// RestartDetached restarts the service out-of-band so it survives the caller
// being killed mid-restart, required when the daemon restarts *itself* (e.g.
// from /service restart). On Linux the daemon shares the unit's cgroup, which
// systemd kills on stop, so the restart is scheduled as a transient timer unit
// (systemd-run) that lives outside that cgroup. launchd/Task Scheduler manage
// the service from a separate process domain, so a normal restart already
// survives there.
func RestartDetached(ctx context.Context, c Config) error {
	if goos(c) == "linux" {
		return runCommand(ctx, Command{Argv: []string{
			"systemd-run", "--user", "--on-active=3",
			"--timer-property=AccuracySec=100ms",
			"systemctl", "--user", "restart", linuxUnitName,
		}})
	}
	return Restart(ctx, c)
}

func validateSource(src string) error {
	if src == "" {
		return errNoSourceDir
	}
	data, err := os.ReadFile(filepath.Clean(filepath.Join(src, "go.mod")))
	if err != nil {
		return fmt.Errorf("%w (no go.mod): %s: %w", errNotSourceCheckout, src, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "module github.com/Herrscherd/herrscher" {
			return nil
		}
	}
	return fmt.Errorf("%w (wrong module): %s", errNotSourceCheckout, src)
}

// Pull fast-forwards the source checkout. --ff-only fails loudly rather than
// creating a merge commit when local and remote have diverged.
func Pull(ctx context.Context, src string) error {
	if err := validateSource(src); err != nil {
		return err
	}
	if out, err := runCapture(ctx, src, "git", "pull", "--ff-only"); err != nil {
		return fmt.Errorf("git pull: %w: %s", err, redact.Output(out))
	}
	return nil
}

// Build compiles the herrscher binary from src to binPath. `go build -o` writes a
// new file and renames it into place, so replacing the live binary is safe while
// the daemon runs (the restart then picks up the new file).
func Build(ctx context.Context, src, binPath string) error {
	if err := validateSource(src); err != nil {
		return err
	}
	if _, err := exec.LookPath("go"); err != nil {
		return errNoGoToolchain
	}
	if out, err := runCapture(ctx, src, "go", "build", "-o", binPath, "."); err != nil {
		return fmt.Errorf("go build: %w: %s", err, redact.Output(out))
	}
	return nil
}

// BuildFor compiles the herrscher binary from src to binPath for another
// platform. It is Build with GOOS/GOARCH pinned and cgo off: cross-compiling a
// cgo binary would need a cross toolchain, and herrscher needs none. This is how
// a host is provisioned, because the releases carry no binary assets.
func BuildFor(ctx context.Context, src, binPath, goos, goarch string) error {
	if err := validateSource(src); err != nil {
		return err
	}
	if _, err := exec.LookPath("go"); err != nil {
		return errNoGoToolchain
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build (%s/%s): %w: %s", goos, goarch, err, redact.Output(out))
	}
	return nil
}

// SourceVersion returns the short commit of the source checkout, or "" if it
// can't be determined (best-effort, for user-facing messages only).
func SourceVersion(ctx context.Context, src string) string {
	out, err := runCapture(ctx, src, "git", "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Smoke runs the freshly built binary with `--help`, which prints usage and
// exits 0 without touching the network. A non-zero exit means the new binary is
// broken (won't even parse its CLI), so the caller must not restart into it. The
// daemon keeps running its old in-memory image; only the on-disk file changed.
func Smoke(ctx context.Context, binPath string) error {
	if binPath == "" {
		return errNoBinaryPath
	}
	if out, err := runCapture(ctx, "", binPath, "--help"); err != nil {
		return fmt.Errorf("new binary failed smoke test (%s --help): %w: %s", binPath, err, redact.Output(out))
	}
	return nil
}

// InstalledBinPath returns the binary path baked into the installed service
// unit's ExecStart, so `service update` rebuilds the binary the service actually
// runs, not whatever binary happened to invoke the CLI (running it from a build
// dir would otherwise rebuild that throwaway binary and leave the daemon stale).
// Returns ("", false) when it can't be determined: unit absent, or an OS whose
// launcher this doesn't parse (only the linux systemd unit is read).
func InstalledBinPath(c Config) (string, bool) {
	if goos(c) != "linux" {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(c.Home, ".config", "systemd", "user", linuxUnitName))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "ExecStart=")
		if !ok {
			continue
		}
		if bin := firstToken(rest); bin != "" {
			return bin, true
		}
	}
	return "", false
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if s[0] == '"' {
		if i := strings.IndexByte(s[1:], '"'); i >= 0 {
			return s[1 : 1+i]
		}
		return s[1:]
	}
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

// Update pulls (optional), rebuilds, smoke-tests, and restarts the service
// inline. Used by the `herrscher service update` CLI (a separate process from the
// daemon).
func Update(ctx context.Context, c Config, src string, pull bool) error {
	if pull {
		if err := Pull(ctx, src); err != nil {
			return err
		}
	}
	if err := Build(ctx, src, c.BinPath); err != nil {
		return err
	}
	if err := Smoke(ctx, c.BinPath); err != nil {
		return err
	}
	changed, err := syncUnitPath(c)
	if err != nil {
		return err
	}
	if changed {
		if err := runCommand(ctx, Command{Argv: []string{"systemctl", "--user", "daemon-reload"}}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "herrscher service: unit PATH updated so agents can find tools under your home")
	}
	return Restart(ctx, c)
}

func syncUnitPath(c Config) (bool, error) {
	if goos(c) != "linux" {
		return false, nil
	}
	path := filepath.Clean(filepath.Join(c.Home, ".config", "systemd", "user", linuxUnitName))
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	want := unitPathLine(c)
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if !isUnitPathLine(l) {
			continue
		}
		if l == want {
			return false, nil
		}
		lines[i] = want
		return true, os.WriteFile(path, []byte(strings.Join(lines, "\n")), unitFileMode)
	}
	for i, l := range lines {
		if strings.TrimSpace(l) != "[Service]" {
			continue
		}
		lines = append(lines[:i+1], append([]string{want}, lines[i+1:]...)...)
		return true, os.WriteFile(path, []byte(strings.Join(lines, "\n")), unitFileMode)
	}
	return false, nil
}

func unitPathLine(c Config) string {
	return "Environment=" + systemdQuote("PATH="+daemonPath(c))
}

func isUnitPathLine(l string) bool {
	return strings.HasPrefix(l, "Environment=PATH=") || strings.HasPrefix(l, `Environment="PATH=`)
}

func daemonPath(c Config) string {
	var out []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" || seen[dir] || strings.ContainsAny(dir, "\n\r") || !volatileFree(dir) {
			return
		}
		seen[dir] = true
		out = append(out, dir)
	}
	if c.BinPath != "" {
		add(filepath.Dir(c.BinPath))
	}
	if c.Home != "" {
		add(filepath.Join(c.Home, ".local", "bin"))
	}
	for _, dir := range strings.Split(c.Path, ":") {
		add(dir)
	}
	for _, dir := range []string{"/usr/local/bin", "/usr/bin", "/bin"} {
		add(dir)
	}
	return strings.Join(out, ":")
}

func volatileFree(dir string) bool {
	if !strings.HasPrefix(dir, "/") {
		return false
	}
	clean := filepath.Clean(dir)
	for _, bad := range []string{"/tmp", "/var/tmp", "/dev/shm", "/run/user"} {
		if clean == bad || strings.HasPrefix(clean, bad+"/") {
			return false
		}
	}
	return true
}

func linuxPlan(c Config) Plan {
	unit := filepath.Join(c.Home, ".config", "systemd", "user", linuxUnitName)
	content := "[Unit]\n" +
		"Description=herrscher gateway daemon\n" +
		"After=network-online.target\n" +
		"Wants=network-online.target\n" +
		"StartLimitIntervalSec=60\n" +
		"StartLimitBurst=5\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		unitPathLine(c) + "\n" +
		"ExecStart=" + joinQuoted(systemdQuote, c.BinPath, serveArgs(c)) + "\n" +
		"Restart=always\n" +
		"RestartSec=3\n\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
	cmds := []Command{
		{Argv: []string{"systemctl", "--user", "daemon-reload"}},
	}
	if c.SkipStart {
		cmds = append(cmds, Command{Argv: []string{"systemctl", "--user", "enable", linuxUnitName}})
	} else {
		cmds = append(cmds, Command{Argv: []string{"systemctl", "--user", "enable", "--now", linuxUnitName}})
	}
	if c.User != "" {
		cmds = append(cmds, Command{Argv: []string{"loginctl", "enable-linger", c.User}, IgnoreErr: true})
	}
	return Plan{
		Files:    []FileWrite{{Path: unit, Content: content, Mode: unitFileMode}, envFileWrite(c)},
		Commands: cmds,
		Notes:    []string{startNote(c, "systemctl --user start "+linuxUnitName)},
	}
}

func macPlan(c Config) Plan {
	plist := filepath.Join(c.Home, "Library", "LaunchAgents", macLabel+".plist")
	logPath := filepath.Join(c.Home, ".local", "state", "herrscher", "herrscher.log")
	argv := append([]string{c.BinPath}, serveArgs(c)...)
	var progArgs strings.Builder
	for _, a := range argv {
		progArgs.WriteString("    <string>" + xmlEscape(a) + "</string>\n")
	}
	content := xmlHeader +
		"<plist version=\"1.0\">\n<dict>\n" +
		"  <key>Label</key><string>" + macLabel + "</string>\n" +
		"  <key>ProgramArguments</key>\n  <array>\n" +
		progArgs.String() +
		"  </array>\n" +
		"  <key>EnvironmentVariables</key>\n  <dict>\n" +
		"    <key>PATH</key><string>" + xmlEscape(daemonPath(c)) + "</string>\n" +
		"  </dict>\n" +
		"  <key>RunAtLoad</key><true/>\n" +
		"  <key>KeepAlive</key><true/>\n" +
		"  <key>StandardOutPath</key><string>" + xmlEscape(logPath) + "</string>\n" +
		"  <key>StandardErrorPath</key><string>" + xmlEscape(logPath) + "</string>\n" +
		"</dict>\n</plist>\n"
	cmds := []Command{
		{Argv: []string{"launchctl", "unload", "-w", plist}, IgnoreErr: true},
	}
	if !c.SkipStart {
		cmds = append(cmds, Command{Argv: []string{"launchctl", "load", "-w", plist}})
	}
	return Plan{
		Files:    []FileWrite{{Path: plist, Content: content, Mode: unitFileMode}, envFileWrite(c)},
		Commands: cmds,
		Notes:    []string{startNote(c, "launchctl load -w "+plist)},
	}
}

func windowsPlan(c Config) Plan {
	launcher := filepath.Join(c.Home, "AppData", "Local", "herrscher", "herrscher-serve.cmd")
	content := "@echo off\r\n" +
		joinQuoted(cmdQuote, c.BinPath, serveArgs(c)) + "\r\n"
	return Plan{
		Files: []FileWrite{{Path: launcher, Content: content, Mode: unitFileMode}, envFileWrite(c)},
		Commands: []Command{
			{Argv: []string{"schtasks", "/create", "/tn", winTaskName, "/tr", launcher, "/sc", "onlogon", "/rl", "limited", "/f"}},
		},
		Notes: []string{"Edit " + c.EnvFile + " with your token; the task runs herrscher at your next logon (or run it now: schtasks /run /tn " + winTaskName + ")."},
	}
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
`

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func startNote(c Config, startCmd string) string {
	if c.SkipStart {
		return "installed and enabled at boot, but NOT started: fill the required secrets in " +
			c.EnvFile + ", then: " + startCmd
	}
	return "running. Secrets live in " + c.EnvFile + "; edit it and restart to change them."
}

func envFileHasRequired(path string, vars []EnvVar) bool {
	required := map[string]bool{}
	for _, v := range vars {
		if v.Required {
			required[v.Key] = true
		}
	}
	if len(required) == 0 {
		return true
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return false
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(v) != "" {
			set[strings.TrimSpace(k)] = true
		}
	}
	for k := range required {
		if !set[k] {
			return false
		}
	}
	return true
}

func joinQuoted(quote func(string) string, bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quote(bin))
	for _, a := range args {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

func systemdQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\n\"\\'") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func cmdQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// DefaultConfig fills a Config from the current environment (binary path, home,
// user, default env-file location and health address).
func DefaultConfig() (Config, error) {
	bin, err := os.Executable()
	if err != nil {
		return Config{}, fmt.Errorf("locate herrscher binary: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("locate home dir: %w", err)
	}
	uname := ""
	if u, err := user.Current(); err == nil {
		uname = u.Username
	}
	return Config{
		BinPath:    bin,
		Home:       home,
		User:       uname,
		EnvFile:    filepath.Join(home, ".config", "herrscher", "herrscher.env"),
		ConfigPath: filepath.Join(home, ".config", "herrscher", "config.json"),
		HealthAddr: "127.0.0.1:8787",
		EnvVars:    registryEnvVars(),
		Path:       os.Getenv("PATH"),
	}, nil
}

func registryEnvVars() []EnvVar {
	var out []EnvVar
	seen := map[string]bool{}
	for _, p := range contracts.Default.Gateways() {
		for _, s := range p.Manifest.Config {
			if s.Env == "" || seen[s.Env] {
				continue
			}
			seen[s.Env] = true
			out = append(out, EnvVar{Key: s.Env, Help: s.Help, Required: s.Required})
		}
	}
	for _, v := range coreEnvVars {
		if !seen[v.Key] {
			out = append(out, v)
		}
	}
	return out
}

// Install runs the install plan for the current OS: it writes the unit/launcher
// and secrets template, then enables and starts the service.
func Install(ctx context.Context, c Config) error {
	if !envFileHasRequired(c.EnvFile, c.EnvVars) {
		c.SkipStart = true
	}
	p, err := BuildPlan(c)
	if err != nil {
		return err
	}
	return runPlan(ctx, p)
}

// Uninstall stops and removes the service for the current OS.
func Uninstall(ctx context.Context, c Config) error {
	p, err := BuildUninstall(c)
	if err != nil {
		return err
	}
	return runPlan(ctx, p)
}

// Status prints the platform's service status to stdout/stderr.
func Status(ctx context.Context, c Config) error {
	cmd, err := StatusCommand(c)
	if err != nil {
		return err
	}
	return runCommand(ctx, cmd)
}

func runPlan(ctx context.Context, p Plan) error {
	for _, f := range p.Files {
		if err := writeFile(f); err != nil {
			return err
		}
	}
	for _, cmd := range p.Commands {
		if err := runCommand(ctx, cmd); err != nil {
			return err
		}
	}
	for _, n := range p.Notes {
		fmt.Fprintln(os.Stderr, "herrscher service: "+n)
	}
	return nil
}

func writeFile(f FileWrite) error {
	if f.Template {
		switch _, err := os.Stat(f.Path); {
		case err == nil:
			return nil
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("stat %s: %w", f.Path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), privateDirMode); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(f.Path), err)
	}
	if err := os.WriteFile(f.Path, []byte(f.Content), f.Mode); err != nil {
		return fmt.Errorf("write %s: %w", f.Path, err)
	}
	return nil
}

func runCapture(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := c.CombinedOutput()
	return []byte(strings.TrimSpace(string(out))), err
}

func runCommand(ctx context.Context, cmd Command) error {
	if len(cmd.Argv) == 0 {
		return nil
	}
	c := exec.CommandContext(ctx, cmd.Argv[0], cmd.Argv[1:]...)
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	if err := c.Run(); err != nil && !cmd.IgnoreErr {
		return fmt.Errorf("%s: %w", strings.Join(cmd.Argv, " "), err)
	}
	return nil
}
