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
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

const (
	goosLinux       = "linux"
	goosDarwin      = "darwin"
	goosWindows     = "windows"
	flagUser        = "--user"
	subDaemonReload = "daemon-reload"
	cmdLaunchctl    = "launchctl"
	flagTaskName    = "/tn"
	cmdSystemctl    = "systemctl"
	cmdSchtasks     = "schtasks"
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

	Lang i18n.Language

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

func goos(cfg Config) string {
	if cfg.GOOS != "" {
		return cfg.GOOS
	}
	return runtime.GOOS
}

func serveArgs(cfg Config) []string {
	args := []string{"serve"}
	if cfg.EnvFile != "" {
		args = append(args, "--env-file", cfg.EnvFile)
	}
	return append(args, cfg.ExtraArgs...)
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

func coreEnvVars() []EnvVar {
	return []EnvVar{
		{Key: "HERRSCHER_OWNER_ID", Help: clitext.ServiceEnvOwnerID},
	}
}

func renderEnvTemplate(vars []EnvVar) string {
	var buf strings.Builder
	buf.WriteString("# herrscher daemon secrets: keep private (chmod 600), never commit.\n")
	buf.WriteString("# Fill these in, then restart the service.\n")
	for _, value := range vars {
		if value.Help != "" {
			fmt.Fprintf(&buf, "# %s\n", value.Help)
		}
		fmt.Fprintf(&buf, "%s=\n", value.Key)
	}
	return buf.String()
}

func envFileWrite(c Config) FileWrite {
	return FileWrite{Path: c.EnvFile, Content: renderEnvTemplate(c.EnvVars), Mode: privateFileMode, Template: true}
}

// BuildPlan returns the install plan for c's target OS. Every platform also
// scaffolds the declarative config.json (template, never clobbered).
func BuildPlan(cfg Config) (Plan, error) {
	var plan Plan
	var err error
	switch goos(cfg) {
	case goosLinux:
		plan, err = linuxPlan(cfg)
	case goosDarwin:
		plan, err = macPlan(cfg)
	case goosWindows:
		plan, err = windowsPlan(cfg)
	default:
		return Plan{}, unsupportedOS(cfg)
	}
	if err != nil {
		return Plan{}, err
	}
	if cfg.ConfigPath != "" {
		plan.Files = append(plan.Files, configFileWrite(cfg))
	}
	return plan, nil
}

func configFileWrite(c Config) FileWrite {
	return FileWrite{Path: c.ConfigPath, Content: config.Template(c.DefaultCmd, c.HealthAddr), Mode: unitFileMode, Template: true}
}

// BuildUninstall returns the uninstall plan for c's target OS.
func BuildUninstall(cfg Config) (Plan, error) {
	switch goos(cfg) {
	case goosLinux:
		unit := filepath.Join(cfg.Home, ".config", "systemd", "user", linuxUnitName)
		return Plan{Commands: []Command{
			{Argv: []string{cmdSystemctl, flagUser, "disable", "--now", linuxUnitName}, IgnoreErr: true},
			{Argv: []string{"rm", "-f", unit}},
			{Argv: []string{cmdSystemctl, flagUser, subDaemonReload}, IgnoreErr: true},
		}}, nil
	case goosDarwin:
		plist := filepath.Join(cfg.Home, "Library", "LaunchAgents", macLabel+".plist")
		return Plan{Commands: []Command{
			{Argv: []string{cmdLaunchctl, "unload", "-w", plist}, IgnoreErr: true},
			{Argv: []string{"rm", "-f", plist}},
		}}, nil
	case goosWindows:
		return Plan{Commands: []Command{
			{Argv: []string{cmdSchtasks, "/delete", flagTaskName, winTaskName, "/f"}},
		}}, nil
	default:
		return Plan{}, unsupportedOS(cfg)
	}
}

func unsupportedOS(cfg Config) error {
	return clitext.Failure(cfg.language(), errUnsupportedOS, clitext.ServiceUnsupportedOS, clitext.Params{clitext.ParamOS: strconv.Quote(goos(cfg))})
}

func (c Config) language() i18n.Language {
	return i18n.Resolve(c.Lang)
}

// StatusCommand returns the command that reports whether the service is active.
func StatusCommand(cfg Config) (Command, error) {
	switch goos(cfg) {
	case goosLinux:
		return Command{Argv: []string{cmdSystemctl, flagUser, "status", linuxUnitName}, IgnoreErr: true}, nil
	case goosDarwin:
		return Command{Argv: []string{cmdLaunchctl, "list", macLabel}, IgnoreErr: true}, nil
	case goosWindows:
		return Command{Argv: []string{cmdSchtasks, "/query", flagTaskName, winTaskName, "/v"}, IgnoreErr: true}, nil
	default:
		return Command{}, unsupportedOS(cfg)
	}
}

// RestartCommands returns the platform commands that restart the running
// service. Some platforms need more than one (stop then start).
func RestartCommands(cfg Config) ([]Command, error) {
	switch goos(cfg) {
	case goosLinux:
		return []Command{{Argv: []string{cmdSystemctl, flagUser, "restart", linuxUnitName}}}, nil
	case goosDarwin:
		target := fmt.Sprintf("gui/%d/%s", os.Getuid(), macLabel)
		return []Command{{Argv: []string{cmdLaunchctl, "kickstart", "-k", target}}}, nil
	case goosWindows:
		return []Command{
			{Argv: []string{cmdSchtasks, "/end", flagTaskName, winTaskName}, IgnoreErr: true},
			{Argv: []string{cmdSchtasks, "/run", flagTaskName, winTaskName}},
		}, nil
	default:
		return nil, unsupportedOS(cfg)
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
func RestartDetached(ctx context.Context, cfg Config) error {
	if goos(cfg) == goosLinux {
		return runCommand(ctx, Command{Argv: []string{
			"systemd-run", flagUser, "--on-active=3",
			"--timer-property=AccuracySec=100ms",
			cmdSystemctl, flagUser, "restart", linuxUnitName,
		}})
	}
	return Restart(ctx, cfg)
}

func validateSource(src string) error {
	lang := clitext.ClientLanguage()
	if src == "" {
		return clitext.Failure(lang, errNoSourceDir, clitext.ServiceNoSourceDir, nil)
	}
	data, err := os.ReadFile(filepath.Clean(filepath.Join(src, "go.mod")))
	if err != nil {
		return clitext.Failure(lang, errNotSourceCheckout, clitext.ServiceNotCheckoutNoModule, clitext.Params{clitext.ParamPath: src, clitext.ParamErr: err.Error()})
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "module github.com/Herrscherd/herrscher" {
			return nil
		}
	}
	return clitext.Failure(lang, errNotSourceCheckout, clitext.ServiceNotCheckoutWrongModule, clitext.Params{clitext.ParamPath: src})
}

// Pull fast-forwards the source checkout. --ff-only fails loudly rather than
// creating a merge commit when local and remote have diverged.
func Pull(ctx context.Context, src string) error {
	if err := validateSource(src); err != nil {
		return err
	}
	if out, err := runCapture(ctx, src, "git", "pull", "--ff-only"); err != nil {
		return clitext.Failure(clitext.ClientLanguage(), err, clitext.ServiceGitPullFailed, clitext.Params{clitext.ParamErr: err.Error(), clitext.ParamOutput: redact.Output(out)})
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
		return clitext.Failure(clitext.ClientLanguage(), errNoGoToolchain, clitext.ServiceNoGoToolchain, nil)
	}
	if out, err := runCapture(ctx, src, "go", "build", "-o", binPath, "."); err != nil {
		return clitext.Failure(clitext.ClientLanguage(), err, clitext.ServiceGoBuildFailed, clitext.Params{clitext.ParamErr: err.Error(), clitext.ParamOutput: redact.Output(out)})
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
		return clitext.Failure(clitext.ClientLanguage(), errNoGoToolchain, clitext.ServiceNoGoToolchain, nil)
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return clitext.Failure(clitext.ClientLanguage(), err, clitext.ServiceGoBuildForFailed, clitext.Params{
			clitext.ParamOS:     goos,
			clitext.ParamArch:   goarch,
			clitext.ParamErr:    err.Error(),
			clitext.ParamOutput: redact.Output(out),
		})
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
		return clitext.Failure(clitext.ClientLanguage(), errNoBinaryPath, clitext.ServiceNoBinaryPath, nil)
	}
	if out, err := runCapture(ctx, "", binPath, "--help"); err != nil {
		return clitext.Failure(clitext.ClientLanguage(), err, clitext.ServiceSmokeFailed, clitext.Params{
			clitext.ParamPath:   binPath,
			clitext.ParamErr:    err.Error(),
			clitext.ParamOutput: redact.Output(out),
		})
	}
	return nil
}

// InstalledBinPath returns the binary path baked into the installed service
// unit's ExecStart, so `service update` rebuilds the binary the service actually
// runs, not whatever binary happened to invoke the CLI (running it from a build
// dir would otherwise rebuild that throwaway binary and leave the daemon stale).
// Returns ("", false) when it can't be determined: unit absent, or an OS whose
// launcher this doesn't parse (only the linux systemd unit is read).
func InstalledBinPath(cfg Config) (string, bool) {
	if goos(cfg) != goosLinux {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(cfg.Home, ".config", "systemd", "user", linuxUnitName))
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

func firstToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if token[0] == '"' {
		if i := strings.IndexByte(token[1:], '"'); i >= 0 {
			return token[1 : 1+i]
		}
		return token[1:]
	}
	if i := strings.IndexByte(token, ' '); i >= 0 {
		return token[:i]
	}
	return token
}

// Update pulls (optional), rebuilds, smoke-tests, and restarts the service
// inline. Used by the `herrscher service update` CLI (a separate process from the
// daemon).
func Update(ctx context.Context, cfg Config, src string, pull bool) error {
	if pull {
		if err := Pull(ctx, src); err != nil {
			return err
		}
	}
	if err := Build(ctx, src, cfg.BinPath); err != nil {
		return err
	}
	if err := Smoke(ctx, cfg.BinPath); err != nil {
		return err
	}
	changed, err := syncUnitPath(cfg)
	if err != nil {
		return err
	}
	if changed {
		if err := runCommand(ctx, Command{Argv: []string{cmdSystemctl, flagUser, subDaemonReload}}); err != nil {
			return err
		}
		note, err := clitext.Text(cfg.language(), clitext.ServiceNoteUnitPath, nil)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, note)
	}
	return Restart(ctx, cfg)
}

func syncUnitPath(cfg Config) (bool, error) {
	if goos(cfg) != goosLinux {
		return false, nil
	}
	path := filepath.Clean(filepath.Join(cfg.Home, ".config", "systemd", "user", linuxUnitName))
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	want := unitPathLine(cfg)
	lines := strings.Split(string(data), "\n")
	for idx, line := range lines {
		if !isUnitPathLine(line) {
			continue
		}
		if line == want {
			return false, nil
		}
		lines[idx] = want
		return true, os.WriteFile(path, []byte(strings.Join(lines, "\n")), unitFileMode)
	}
	for idx, l := range lines {
		if strings.TrimSpace(l) != "[Service]" {
			continue
		}
		lines = append(lines[:idx+1], append([]string{want}, lines[idx+1:]...)...)
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

func daemonPath(cfg Config) string {
	var out []string
	seen := make(map[string]bool)
	add := func(dir string) {
		if dir == "" || seen[dir] || strings.ContainsAny(dir, "\n\r") || !volatileFree(dir) {
			return
		}
		seen[dir] = true
		out = append(out, dir)
	}
	if cfg.BinPath != "" {
		add(filepath.Dir(cfg.BinPath))
	}
	if cfg.Home != "" {
		add(filepath.Join(cfg.Home, ".local", "bin"))
	}
	for _, dir := range strings.Split(cfg.Path, ":") {
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

func linuxPlan(cfg Config) (Plan, error) {
	unit := filepath.Join(cfg.Home, ".config", "systemd", "user", linuxUnitName)
	content := "[Unit]\n" +
		"Description=herrscher gateway daemon\n" +
		"After=network-online.target\n" +
		"Wants=network-online.target\n" +
		"StartLimitIntervalSec=60\n" +
		"StartLimitBurst=5\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		unitPathLine(cfg) + "\n" +
		"ExecStart=" + joinQuoted(systemdQuote, cfg.BinPath, serveArgs(cfg)) + "\n" +
		"Restart=always\n" +
		"RestartSec=3\n\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
	cmds := []Command{
		{Argv: []string{cmdSystemctl, flagUser, subDaemonReload}},
	}
	if cfg.SkipStart {
		cmds = append(cmds, Command{Argv: []string{cmdSystemctl, flagUser, "enable", linuxUnitName}})
	} else {
		cmds = append(cmds, Command{Argv: []string{cmdSystemctl, flagUser, "enable", "--now", linuxUnitName}})
	}
	if cfg.User != "" {
		cmds = append(cmds, Command{Argv: []string{"loginctl", "enable-linger", cfg.User}, IgnoreErr: true})
	}
	note, err := startNote(cfg, "systemctl --user start "+linuxUnitName)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Files:    []FileWrite{{Path: unit, Content: content, Mode: unitFileMode}, envFileWrite(cfg)},
		Commands: cmds,
		Notes:    []string{note},
	}, nil
}

func macPlan(cfg Config) (Plan, error) {
	plist := filepath.Join(cfg.Home, "Library", "LaunchAgents", macLabel+".plist")
	logPath := filepath.Join(cfg.Home, ".local", "state", "herrscher", "herrscher.log")
	argv := append([]string{cfg.BinPath}, serveArgs(cfg)...)
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
		"    <key>PATH</key><string>" + xmlEscape(daemonPath(cfg)) + "</string>\n" +
		"  </dict>\n" +
		"  <key>RunAtLoad</key><true/>\n" +
		"  <key>KeepAlive</key><true/>\n" +
		"  <key>StandardOutPath</key><string>" + xmlEscape(logPath) + "</string>\n" +
		"  <key>StandardErrorPath</key><string>" + xmlEscape(logPath) + "</string>\n" +
		"</dict>\n</plist>\n"
	cmds := []Command{
		{Argv: []string{cmdLaunchctl, "unload", "-w", plist}, IgnoreErr: true},
	}
	if !cfg.SkipStart {
		cmds = append(cmds, Command{Argv: []string{cmdLaunchctl, "load", "-w", plist}})
	}
	note, err := startNote(cfg, "launchctl load -w "+plist)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Files:    []FileWrite{{Path: plist, Content: content, Mode: unitFileMode}, envFileWrite(cfg)},
		Commands: cmds,
		Notes:    []string{note},
	}, nil
}

func windowsPlan(cfg Config) (Plan, error) {
	launcher := filepath.Join(cfg.Home, "AppData", "Local", "herrscher", "herrscher-serve.cmd")
	content := "@echo off\r\n" +
		joinQuoted(cmdQuote, cfg.BinPath, serveArgs(cfg)) + "\r\n"
	note, err := clitext.Text(cfg.language(), clitext.ServiceNoteWindowsEdit, clitext.Params{clitext.ParamEnvFile: cfg.EnvFile, clitext.ParamTask: winTaskName})
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Files: []FileWrite{{Path: launcher, Content: content, Mode: unitFileMode}, envFileWrite(cfg)},
		Commands: []Command{
			{Argv: []string{cmdSchtasks, "/create", flagTaskName, winTaskName, "/tr", launcher, "/sc", "onlogon", "/rl", "limited", "/f"}},
		},
		Notes: []string{note},
	}, nil
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
`

func xmlEscape(value string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(value)
}

func startNote(cfg Config, startCmd string) (string, error) {
	if cfg.SkipStart {
		return clitext.Text(cfg.language(), clitext.ServiceNoteNotStarted, clitext.Params{clitext.ParamEnvFile: cfg.EnvFile, clitext.ParamStart: startCmd})
	}
	return clitext.Text(cfg.language(), clitext.ServiceNoteRunning, clitext.Params{clitext.ParamEnvFile: cfg.EnvFile})
}

func envFileHasRequired(path string, vars []EnvVar) bool {
	required := make(map[string]bool)
	for _, value := range vars {
		if value.Required {
			required[value.Key] = true
		}
	}
	if len(required) == 0 {
		return true
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return false
	}
	set := make(map[string]bool)
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

func systemdQuote(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\n\"\\'") {
		return value
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(value) + `"`
}

func cmdQuote(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\"") {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// DefaultConfig fills a Config from the current environment (binary path, home,
// user, default env-file location and health address).
func DefaultConfig() (Config, error) {
	lang := clitext.ClientLanguage()
	translator := clitext.For(lang)
	bin, err := os.Executable()
	if err != nil {
		return Config{}, clitext.Failure(lang, err, clitext.ServiceLocateBinary, clitext.Params{clitext.ParamErr: err.Error()})
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, clitext.Failure(lang, err, clitext.ServiceLocateHome, clitext.Params{clitext.ParamErr: err.Error()})
	}
	uname := ""
	if u, err := user.Current(); err == nil {
		uname = u.Username
	}
	envVars := registryEnvVars(translator)
	if err := translator.Err(); err != nil {
		return Config{}, err
	}
	return Config{
		Lang:       lang,
		BinPath:    bin,
		Home:       home,
		User:       uname,
		EnvFile:    filepath.Join(home, ".config", "herrscher", "herrscher.env"),
		ConfigPath: filepath.Join(home, ".config", "herrscher", "config.json"),
		HealthAddr: "127.0.0.1:8787",
		EnvVars:    envVars,
		Path:       os.Getenv("PATH"),
	}, nil
}

func registryEnvVars(translator *clitext.Translator) []EnvVar {
	var out []EnvVar
	seen := make(map[string]bool)
	gateways := contracts.Default.Gateways()
	for i := range gateways {
		for _, setting := range gateways[i].Manifest.Config {
			if setting.Env == "" || seen[setting.Env] {
				continue
			}
			seen[setting.Env] = true
			out = append(out, EnvVar{Key: setting.Env, Help: renderHelp(translator, setting.Help), Required: setting.Required})
		}
	}
	core := coreEnvVars()
	for idx := range core {
		if !seen[core[idx].Key] {
			core[idx].Help = renderHelp(translator, core[idx].Help)
			out = append(out, core[idx])
		}
	}
	return out
}

func renderHelp(translator *clitext.Translator, key string) string {
	if key == "" {
		return ""
	}
	return translator.Text(clitext.Key(key), nil)
}

// Install runs the install plan for the current OS: it writes the unit/launcher
// and secrets template, then enables and starts the service.
func Install(ctx context.Context, cfg Config) error {
	if !envFileHasRequired(cfg.EnvFile, cfg.EnvVars) {
		cfg.SkipStart = true
	}
	plan, err := BuildPlan(cfg)
	if err != nil {
		return err
	}
	return runPlan(ctx, plan)
}

// Uninstall stops and removes the service for the current OS.
func Uninstall(ctx context.Context, c Config) error {
	plan, err := BuildUninstall(c)
	if err != nil {
		return err
	}
	return runPlan(ctx, plan)
}

// Status prints the platform's service status to stdout/stderr.
func Status(ctx context.Context, c Config) error {
	cmd, err := StatusCommand(c)
	if err != nil {
		return err
	}
	return runCommand(ctx, cmd)
}

func runPlan(ctx context.Context, plan Plan) error {
	for _, f := range plan.Files {
		if err := writeFile(f); err != nil {
			return err
		}
	}
	for _, cmd := range plan.Commands {
		if err := runCommand(ctx, cmd); err != nil {
			return err
		}
	}
	for _, n := range plan.Notes {
		fmt.Fprintln(os.Stderr, n)
	}
	return nil
}

func writeFile(write FileWrite) error {
	lang := clitext.ClientLanguage()
	if write.Template {
		switch _, err := os.Stat(write.Path); {
		case err == nil:
			return nil
		case !errors.Is(err, fs.ErrNotExist):
			return clitext.Failure(lang, err, clitext.ServiceStatFailed, clitext.Params{clitext.ParamPath: write.Path, clitext.ParamErr: err.Error()})
		}
	}
	dir := filepath.Dir(write.Path)
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return clitext.Failure(lang, err, clitext.ServiceMkdirFailed, clitext.Params{clitext.ParamPath: dir, clitext.ParamErr: err.Error()})
	}
	if err := os.WriteFile(write.Path, []byte(write.Content), write.Mode); err != nil {
		return clitext.Failure(lang, err, clitext.ServiceWriteFailed, clitext.Params{clitext.ParamPath: write.Path, clitext.ParamErr: err.Error()})
	}
	return nil
}

func runCapture(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return []byte(strings.TrimSpace(string(out))), err
}

func runCommand(ctx context.Context, cmd Command) error {
	if len(cmd.Argv) == 0 {
		return nil
	}
	execCmd := exec.CommandContext(ctx, cmd.Argv[0], cmd.Argv[1:]...)
	execCmd.Stdout, execCmd.Stderr = os.Stderr, os.Stderr
	if err := execCmd.Run(); err != nil && !cmd.IgnoreErr {
		return fmt.Errorf("%s: %w", strings.Join(cmd.Argv, " "), err)
	}
	return nil
}
