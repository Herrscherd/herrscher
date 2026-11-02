// Package manage is the host's self-management surface: it edits the managed
// plugins.go composition and rebuilds the binary. It is invoked by the herrscher
// dispatcher for the plugin/update/install verbs. Runtime lives in the host; this
// is install/update/plugin tooling only.
package manage

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	errNoHostModule  = errors.New("no host module found (looked for plugins.go)")
	errNotCompiledIn = errors.New("not compiled in")
)

func parseExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

// PluginCmd lists, adds, removes, pins or unpins a compiled-in plugin in the
// managed plugins.go, rebuilding the binary on a change.
func PluginCmd(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("plugin", flag.ContinueOnError)
	hostDir := fs.String("host", "", "path to the host module")
	noBuild := fs.Bool("no-build", false, "edit plugins.go but skip go get/build")
	yes := fs.Bool("yes", false, "answer both confirmations without asking")
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: herrscher plugin <list|add|remove|pin|unpin> [module[@version]]")
		return 2
	}
	sub := args[0]
	var positionals []string
	rem := args[1:]
	for len(rem) > 0 {
		if err := fs.Parse(rem); err != nil {
			return parseExit(err)
		}
		rem = fs.Args()
		if len(rem) == 0 {
			break
		}
		positionals = append(positionals, rem[0])
		rem = rem[1:]
	}

	dir, err := resolveHost(*hostDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	manifest := filepath.Join(dir, "plugins.go")
	src, err := os.ReadFile(manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", manifest, err)
		return 1
	}

	switch sub {
	case "list":
		if err = listWithVersions(ctx, dir, string(src), os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0

	case "add", "remove":
		if len(positionals) != 1 {
			fmt.Fprintf(os.Stderr, "usage: herrscher plugin %s <module>\n", sub)
			return 2
		}
		module, version := moduleArg(sub, positionals[0])
		var out string
		var changed bool
		if sub == "add" {
			out, changed, err = addPlugin(string(src), module)
		} else {
			out, changed, err = removePlugin(string(src), module)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if !changed && version == "" {
			fmt.Fprintf(os.Stdout, "no change: %s already %s\n", module, pastTense(sub))
			return 0
		}
		if *noBuild {
			if err := os.WriteFile(manifest, []byte(out), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "write %s: %v\n", manifest, err)
				return 1
			}
			fmt.Fprintf(os.Stdout, "%s %s in %s\n", pastTense(sub), module, manifest)
			fmt.Fprintln(os.Stdout, "skipped build (--no-build); run `go build` in the host to apply")
			if version != "" {
				fmt.Fprintf(os.Stdout, "the version was not installed: %s@%s needs `go get`\n", module, version)
			}
			return 0
		}
		cmds := removeCommands()
		if sub == "add" {
			cmds = addCommands(module, version)
		}
		steps := append([]step{fileStep(manifest, out)}, cmdSteps(dir, cmds)...)
		applied, err := apply(ctx, dir, addFindings(ctx, dir, module, version), osDecider(*yes), steps)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if !applied {
			fmt.Fprintln(os.Stdout, "aborted; nothing was written")
			return 0
		}
		if !changed {
			fmt.Fprintf(os.Stdout, "%s was already in %s; moved to %s\n", module, manifest, version)
			return 0
		}
		fmt.Fprintf(os.Stdout, "%s %s in %s\n", pastTense(sub), module, manifest)
		return 0

	case "pin", "unpin":
		if len(positionals) < 1 || len(positionals) > 2 || (sub == "unpin" && len(positionals) != 1) {
			fmt.Fprintf(os.Stderr, "usage: herrscher plugin %s <module>%s\n", sub, pinUsageTail(sub))
			return 2
		}
		module, version := splitModuleVersion(positionals[0])
		if len(positionals) == 2 {
			version = positionals[1]
		}
		if err := compiledIn(dir, module); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if sub == "pin" && version != "" {
			steps := cmdSteps(dir, addCommands(module, version))
			applied, err := apply(ctx, dir, addFindings(ctx, dir, module, version), osDecider(*yes), steps)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if !applied {
				fmt.Fprintln(os.Stdout, "aborted; nothing was written and nothing was pinned")
				return 0
			}
		}
		if err := pinModule(dir, module, sub == "pin"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "%sned %s\n", sub, module)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown plugin subcommand %q\n", sub)
		return 2
	}
}

func moduleArg(sub, arg string) (module, version string) {
	if sub == "remove" {
		return arg, ""
	}
	return splitModuleVersion(arg)
}

func pinUsageTail(sub string) string {
	if sub == "pin" {
		return " [version]"
	}
	return ""
}

func pastTense(sub string) string {
	if sub == "add" {
		return "added"
	}
	return "removed"
}

func resolveHost(explicit string) (string, error) {
	candidates := []string{explicit, ".", "herrscher"}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "plugins.go")); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("%w; pass --host DIR", errNoHostModule)
}

func buildCommands() [][]string {
	return [][]string{
		{"go", "mod", "tidy"},
		{"go", "build", "./..."},
		{"go", "install", "."},
	}
}

func addCommands(module, version string) [][]string {
	arg := module
	if version != "" {
		arg = module + "@" + version
	}
	return append([][]string{{"go", "get", "--", arg}}, buildCommands()...)
}

func removeCommands() [][]string { return buildCommands() }

func updatePlan(mods []string, pins map[string]bool, installed map[string]string) (cmds [][]string, skipped []string) {
	for _, m := range mods {
		if pins[m] {
			v := installed[m]
			if v == "" {
				v = "?"
			}
			skipped = append(skipped, fmt.Sprintf("skipped %s (pinned %s)", m, v))
			continue
		}
		cmds = append(cmds, []string{"go", "get", "-u", m})
	}
	if cmds == nil {
		return nil, skipped
	}
	return append(cmds, buildCommands()...), skipped
}

func cmdSteps(dir string, cmds [][]string) []step {
	steps := make([]step, 0, len(cmds))
	for _, c := range cmds {
		steps = append(steps, cmdStep(dir, c[0], c[1:]...))
	}
	return steps
}

func cmdStep(dir, name string, args ...string) step {
	return func(ctx context.Context) (string, error) {
		var captured strings.Builder
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		cmd.Stdout = io.MultiWriter(os.Stdout, &captured)
		cmd.Stderr = io.MultiWriter(os.Stderr, &captured)
		fmt.Fprintf(os.Stderr, "+ %s %s\n", name, strings.Join(args, " "))
		err := cmd.Run()
		return captured.String(), err
	}
}

func fileStep(path, body string) step {
	return func(context.Context) (string, error) {
		return "", os.WriteFile(path, []byte(body), 0o644)
	}
}

func osDecider(yes bool) decider {
	return newDecider(yes, isTerminal(os.Stdin), bufio.NewReader(os.Stdin))
}

func newDecider(yes, interactive bool, in *bufio.Reader) decider {
	if yes || !interactive {
		return newAutoDecider()
	}
	return newPromptDecider(in, newStyle())
}

func addFindings(ctx context.Context, dir, module, version string) []string {
	if version == "" {
		return nil
	}
	tc := goToolchain{dir: dir}
	var findings []string
	if installed, err := installedVersions(ctx, tc, []string{module, contractsModule}); err == nil {
		if cur := installed[module]; cur != "" && semverLess(version, cur) {
			findings = append(findings, fmt.Sprintf("%s moves back from %s to %s", module, cur, version))
		}
		if want, err := contractsRequirement(ctx, tc, module, version); err == nil && want != "" {
			if have := installed[contractsModule]; have != "" && minorOf(want) != minorOf(have) {
				findings = append(findings, fmt.Sprintf("%s@%s wants contracts %s; the composition resolves %s", module, version, want, have))
			}
		}
	}
	return findings
}

func semverLess(a, b string) bool {
	an, bn := versionFields(a), versionFields(b)
	for i := 0; i < len(an) && i < len(bn); i++ {
		if an[i] != bn[i] {
			return an[i] < bn[i]
		}
	}
	return len(an) < len(bn)
}

func versionFields(v string) []int {
	var out []int
	for _, part := range strings.Split(strings.TrimPrefix(v, "v"), ".") {
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

func minorOf(v string) string {
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
	if len(parts) < 2 {
		return v
	}
	return parts[0] + "." + parts[1]
}

func pluginRows(mods []string, installed map[string]string, pins map[string]bool, available map[string][]string) []PluginVersion {
	rows := make([]PluginVersion, 0, len(mods))
	for _, m := range mods {
		row := PluginVersion{Module: m, Installed: "?", Latest: "?", Pinned: pins[m]}
		if v := installed[m]; v != "" {
			row.Installed = v
		}
		if vs := available[m]; len(vs) > 0 {
			row.Latest = vs[len(vs)-1]
		}
		rows = append(rows, row)
	}
	return rows
}

func pluginVersions(ctx context.Context, dir, src string) ([]PluginVersion, error) {
	mods, err := listPlugins(src)
	if err != nil {
		return nil, err
	}
	pins, err := loadPins(dir)
	if err != nil {
		return nil, err
	}
	tc := goToolchain{dir: dir}
	installed, err := installedVersions(ctx, tc, mods)
	if err != nil {
		installed = nil
	}
	available := map[string][]string{}
	for _, m := range mods {
		vs, err := availableVersions(ctx, tc, m)
		if err != nil {
			slog.Warn("latest version unknown", "module", m, "err", err)
			continue
		}
		if vs != nil {
			available[m] = vs
		}
	}
	return pluginRows(mods, installed, pins, available), nil
}

func listWithVersions(ctx context.Context, dir, src string, out io.Writer) error {
	rows, err := pluginVersions(ctx, dir, src)
	if err != nil {
		return err
	}
	for _, row := range rows {
		pin := ""
		if row.Pinned {
			pin = "  pinned"
		}
		if _, err := fmt.Fprintf(out, "%s  %s  %s%s\n", row.Module, row.Installed, row.Latest, pin); err != nil {
			return err
		}
	}
	return nil
}

func loadPins(dir string) (map[string]bool, error) {
	src, err := os.ReadFile(filepath.Join(dir, pinFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", pinFile, err)
	}
	return readPins(string(src))
}

func compiledIn(dir, module string) error {
	src, err := os.ReadFile(filepath.Join(dir, "plugins.go"))
	if err != nil {
		return fmt.Errorf("read plugins.go: %w", err)
	}
	mods, err := listPlugins(string(src))
	if err != nil {
		return err
	}
	for _, m := range mods {
		if m == module {
			return nil
		}
	}
	return fmt.Errorf("%s is %w; `herrscher plugin add %s` first", module, errNotCompiledIn, module)
}

func pinModule(dir, module string, pinned bool) error {
	if err := compiledIn(dir, module); err != nil {
		return err
	}
	pins, err := loadPins(dir)
	if err != nil {
		return err
	}
	if pins[module] == pinned {
		return nil
	}
	if pinned {
		pins[module] = true
	} else {
		delete(pins, module)
	}
	return os.WriteFile(filepath.Join(dir, pinFile), []byte(writePins(pins)), 0o644)
}

func run(ctx context.Context, dir, name string, args ...string) int {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	var line strings.Builder
	line.WriteString("+ ")
	line.WriteString(name)
	for _, a := range args {
		line.WriteByte(' ')
		line.WriteString(a)
	}
	fmt.Fprintln(os.Stderr, line.String())
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s failed: %v\n", name, err)
		return 1
	}
	return 0
}
