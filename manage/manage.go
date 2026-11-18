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
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/internal/errwriter"
)

var (
	errNoHostModule  = clitext.SentinelError("no host module found (looked for plugins.go); pass --host DIR")
	errNotCompiledIn = clitext.SentinelError("module is not compiled in; run herrscher plugin add first")
)

func parseExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return exitUsage
}

// PluginCmd lists, adds, removes, pins or unpins a compiled-in plugin in the
// managed plugins.go, rebuilding the binary on a change.
func PluginCmd(ctx context.Context, messages *clitext.Messages, args []string) int {
	out, errOut := errwriter.New(os.Stdout), errwriter.New(os.Stderr)
	return errwriter.Finish(pluginHost(ctx, messages, out, errOut, args), out, errOut)
}

type pluginEnv struct {
	dir, manifest string
	src           string
	out, errOut   *errwriter.Writer
	yes, noBuild  bool
}

func pluginHost(ctx context.Context, messages *clitext.Messages, out, errOut *errwriter.Writer, args []string) int {
	flags := flag.NewFlagSet("plugin", flag.ContinueOnError)
	hostDir := flags.String("host", "", msg(messages, clitext.ManageFlagHost, nil))
	noBuild := flags.Bool("no-build", false, msg(messages, clitext.ManageFlagPluginNoBuild, nil))
	yes := flags.Bool("yes", false, msg(messages, clitext.ManageFlagYes, nil))
	if len(args) == 0 {
		errOut.Println(msg(messages, clitext.ManagePluginUsage, nil))
		return exitUsage
	}
	sub := args[0]
	var positionals []string
	rem := args[1:]
	for len(rem) > 0 {
		if err := flags.Parse(rem); err != nil {
			return parseExit(err)
		}
		rem = flags.Args()
		if len(rem) == 0 {
			break
		}
		positionals = append(positionals, rem[0])
		rem = rem[1:]
	}

	dir, err := resolveHost(messages, *hostDir)
	if err != nil {
		errOut.Println(err)
		return 1
	}
	manifest := filepath.Join(dir, manifestFile)
	src, err := os.ReadFile(manifest)
	if err != nil {
		errOut.Println(failure(messages, err, clitext.ManageReadFailed, clitext.Params{clitext.ParamPath: manifest, clitext.ParamErr: err.Error()}))
		return 1
	}
	env := pluginEnv{dir: dir, manifest: manifest, src: string(src), out: out, errOut: errOut, yes: *yes, noBuild: *noBuild}

	switch sub {
	case "list":
		return pluginList(ctx, messages, env)
	case verbAdd, verbRemove:
		return pluginAddRemove(ctx, messages, env, sub, positionals)
	case verbPin, verbUnpin:
		return pluginPin(ctx, messages, env, sub, positionals)
	default:
		errOut.Println(msg(messages, clitext.ManagePluginUnknownSubcommand, clitext.Params{clitext.ParamName: strconv.Quote(sub)}))
		return exitUsage
	}
}

func pluginList(ctx context.Context, messages *clitext.Messages, env pluginEnv) int {
	if err := listWithVersions(ctx, messages, env.dir, env.src, env.out); err != nil {
		env.errOut.Println(err)
		return 1
	}
	return 0
}

func pluginAddRemove(ctx context.Context, messages *clitext.Messages, env pluginEnv, sub string, positionals []string) int {
	if len(positionals) != 1 {
		env.errOut.Println(msg(messages, clitext.ManagePluginEditUsage, clitext.Params{clitext.ParamVerb: sub}))
		return exitUsage
	}
	module, version := moduleArg(sub, positionals[0])
	rendered, changed, err := renderPluginEdit(messages, sub, env.src, module)
	if err != nil {
		env.errOut.Println(err)
		return 1
	}
	if !changed && version == "" {
		env.out.Println(msg(messages, clitext.ManagePluginNoChange, clitext.Params{clitext.ParamModule: module, clitext.ParamState: stateWord(messages, sub)}))
		return 0
	}
	if env.noBuild {
		if writeErr := os.WriteFile(env.manifest, []byte(rendered), modeFile); writeErr != nil {
			env.errOut.Println(failure(messages, writeErr, clitext.ManageWriteFailed, clitext.Params{clitext.ParamPath: env.manifest, clitext.ParamErr: writeErr.Error()}))
			return 1
		}
		env.out.Println(changeLine(messages, sub, module, env.manifest))
		env.out.Println(msg(messages, clitext.ManagePluginSkippedBuild, nil))
		if version != "" {
			env.out.Println(msg(messages, clitext.ManagePluginVersionNotInstalled, clitext.Params{clitext.ParamModule: module, clitext.ParamVersion: version}))
		}
		return 0
	}
	cmds := removeCommands()
	if sub == verbAdd {
		cmds = addCommands(module, version)
	}
	steps := append([]step{fileStep(env.manifest, rendered)}, cmdSteps(env.errOut, env.dir, cmds)...)
	applied, err := apply(ctx, messages, env.dir, addFindings(ctx, messages, env.dir, module, version), osDecider(messages, env.yes), steps)
	if err != nil {
		env.errOut.Println(err)
		return 1
	}
	if !applied {
		env.out.Println(msg(messages, clitext.ManageAborted, nil))
		return 0
	}
	if !changed {
		env.out.Println(msg(messages, clitext.ManagePluginMoved, clitext.Params{clitext.ParamModule: module, clitext.ParamPath: env.manifest, clitext.ParamVersion: version}))
		return 0
	}
	env.out.Println(changeLine(messages, sub, module, env.manifest))
	return 0
}

func renderPluginEdit(messages *clitext.Messages, sub, src, module string) (string, bool, error) {
	if sub == verbAdd {
		return addPlugin(messages, src, module)
	}
	return removePlugin(messages, src, module)
}

func pluginPin(ctx context.Context, messages *clitext.Messages, env pluginEnv, sub string, positionals []string) int {
	if len(positionals) < 1 || len(positionals) > 2 || (sub == verbUnpin && len(positionals) != 1) {
		env.errOut.Println(pinUsage(messages, sub))
		return exitUsage
	}
	module, version := splitModuleVersion(positionals[0])
	if len(positionals) == positionalsWithVersion {
		version = positionals[1]
	}
	if err := compiledIn(messages, env.dir, module); err != nil {
		env.errOut.Println(err)
		return 1
	}
	if sub == verbPin && version != "" {
		steps := cmdSteps(env.errOut, env.dir, addCommands(module, version))
		applied, err := apply(ctx, messages, env.dir, addFindings(ctx, messages, env.dir, module, version), osDecider(messages, env.yes), steps)
		if err != nil {
			env.errOut.Println(err)
			return 1
		}
		if !applied {
			env.out.Println(msg(messages, clitext.ManageAbortedPinned, nil))
			return 0
		}
	}
	if err := pinModule(messages, env.dir, module, sub == verbPin); err != nil {
		env.errOut.Println(err)
		return 1
	}
	env.out.Println(pinChangeLine(messages, sub, module))
	return 0
}

func pinUsage(messages *clitext.Messages, sub string) string {
	if sub == verbPin {
		return msg(messages, clitext.ManagePluginPinUsage, nil)
	}
	return msg(messages, clitext.ManagePluginEditUsage, clitext.Params{clitext.ParamVerb: sub})
}

func pinChangeLine(messages *clitext.Messages, sub, module string) string {
	if sub == verbPin {
		return msg(messages, clitext.ManagePluginPinned, clitext.Params{clitext.ParamModule: module})
	}
	return msg(messages, clitext.ManagePluginUnpinned, clitext.Params{clitext.ParamModule: module})
}

func moduleArg(sub, arg string) (string, string) {
	if sub == verbRemove {
		return arg, ""
	}
	return splitModuleVersion(arg)
}

func resolveHost(messages *clitext.Messages, explicit string) (string, error) {
	candidates := []string{explicit, ".", "herrscher"}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(candidate, manifestFile)); err == nil {
			return candidate, nil
		}
	}
	return "", failure(messages, errNoHostModule, clitext.ManageNoHostModule, nil)
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
	return append([][]string{{"go", verbGet, "--", arg}}, buildCommands()...)
}

func removeCommands() [][]string { return buildCommands() }

type pinnedSkip struct {
	module, version string
}

func updatePlan(mods []string, pins map[string]bool, installed map[string]string) ([][]string, []pinnedSkip) {
	var cmds [][]string
	var skipped []pinnedSkip
	for _, module := range mods {
		if pins[module] {
			version := installed[module]
			if version == "" {
				version = "?"
			}
			skipped = append(skipped, pinnedSkip{module: module, version: version})
			continue
		}
		cmds = append(cmds, []string{"go", verbGet, "-u", module})
	}
	if cmds == nil {
		return nil, skipped
	}
	return append(cmds, buildCommands()...), skipped
}

func cmdSteps(errOut *errwriter.Writer, dir string, cmds [][]string) []step {
	steps := make([]step, 0, len(cmds))
	for _, c := range cmds {
		steps = append(steps, cmdStep(errOut, dir, c[0], c[1:]...))
	}
	return steps
}

func cmdStep(errOut *errwriter.Writer, dir, name string, args ...string) step {
	return func(ctx context.Context) (string, error) {
		var captured strings.Builder
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		cmd.Stdout = io.MultiWriter(os.Stdout, &captured)
		cmd.Stderr = io.MultiWriter(os.Stderr, &captured)
		errOut.Printf("+ %s %s\n", name, strings.Join(args, " "))
		err := cmd.Run()
		return captured.String(), err
	}
}

func fileStep(path, body string) step {
	return func(context.Context) (string, error) {
		return "", os.WriteFile(path, []byte(body), modeFile)
	}
}

func osDecider(messages *clitext.Messages, yes bool) decider {
	return newDecider(messages, yes, isTerminal(os.Stdin), bufio.NewReader(os.Stdin))
}

func newDecider(messages *clitext.Messages, yes, interactive bool, input *bufio.Reader) decider {
	if yes || !interactive {
		return newAutoDecider()
	}
	return newPromptDecider(messages, input, newStyle())
}

func addFindings(ctx context.Context, messages *clitext.Messages, dir, module, version string) []string {
	if version == "" {
		return nil
	}
	toolchain := goToolchain{messages: messages, dir: dir}
	var findings []string
	if installed, err := installedVersions(ctx, messages, toolchain, []string{module, contractsModule}); err == nil {
		if cur := installed[module]; cur != "" && semverLess(version, cur) {
			findings = append(findings, msg(messages, clitext.ManageFindingMovesBack, clitext.Params{clitext.ParamModule: module, clitext.ParamCurrent: cur, clitext.ParamVersion: version}))
		}
		if want, err := contractsRequirement(ctx, messages, toolchain, module, version); err == nil && want != "" {
			if have := installed[contractsModule]; have != "" && minorOf(want) != minorOf(have) {
				findings = append(findings, msg(messages, clitext.ManageFindingContracts, clitext.Params{clitext.ParamModule: module, clitext.ParamVersion: version, clitext.ParamWant: want, clitext.ParamHave: have}))
			}
		}
	}
	return findings
}

func semverLess(a, b string) bool {
	aFields, bFields := versionFields(a), versionFields(b)
	for idx := 0; idx < len(aFields) && idx < len(bFields); idx++ {
		if aFields[idx] != bFields[idx] {
			return aFields[idx] < bFields[idx]
		}
	}
	return len(aFields) < len(bFields)
}

func versionFields(version string) []int {
	var out []int
	for _, part := range strings.Split(strings.TrimPrefix(version, "v"), ".") {
		number := 0
		for _, char := range part {
			if char < '0' || char > '9' {
				break
			}
			number = number*decimalBase + int(char-'0')
		}
		out = append(out, number)
	}
	return out
}

func minorOf(version string) string {
	parts := strings.SplitN(strings.TrimPrefix(version, "v"), ".", semverParts)
	if len(parts) < minVersionParts {
		return version
	}
	return parts[0] + "." + parts[1]
}

func pluginRows(mods []string, installed map[string]string, pins map[string]bool, available map[string][]string) []PluginVersion {
	rows := make([]PluginVersion, 0, len(mods))
	for _, module := range mods {
		row := PluginVersion{Module: module, Installed: "?", Latest: "?", Pinned: pins[module]}
		if v := installed[module]; v != "" {
			row.Installed = v
		}
		if vs := available[module]; len(vs) > 0 {
			row.Latest = vs[len(vs)-1]
		}
		rows = append(rows, row)
	}
	return rows
}

func pluginVersions(ctx context.Context, messages *clitext.Messages, dir, src string) ([]PluginVersion, error) {
	mods, err := listPlugins(messages, src)
	if err != nil {
		return nil, err
	}
	pins, err := loadPins(messages, dir)
	if err != nil {
		return nil, err
	}
	toolchain := goToolchain{messages: messages, dir: dir}
	installed, err := installedVersions(ctx, messages, toolchain, mods)
	if err != nil {
		installed = nil
	}
	available := make(map[string][]string)
	for _, module := range mods {
		versions, err := availableVersions(ctx, messages, toolchain, module)
		if err != nil {
			slog.Warn("latest version unknown", "module", module, "err", err)
			continue
		}
		if versions != nil {
			available[module] = versions
		}
	}
	return pluginRows(mods, installed, pins, available), nil
}

func listWithVersions(ctx context.Context, messages *clitext.Messages, dir, src string, out io.Writer) error {
	rows, err := pluginVersions(ctx, messages, dir, src)
	if err != nil {
		return err
	}
	return writePluginRows(messages, out, rows)
}

func writePluginRows(messages *clitext.Messages, out io.Writer, rows []PluginVersion) error {
	translator := messages.For(clitext.ClientLanguage())
	defer logRenderErr(translator)
	for _, row := range rows {
		pin := ""
		if row.Pinned {
			pin = "  " + translator.Text(clitext.ManagePinnedTag, nil)
		}
		if _, err := fmt.Fprintf(out, "%s  %s  %s%s\n", row.Module, row.Installed, row.Latest, pin); err != nil {
			return err
		}
	}
	return nil
}

func loadPins(messages *clitext.Messages, dir string) (map[string]bool, error) {
	src, err := os.ReadFile(filepath.Join(dir, pinFile))
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]bool), nil
	}
	if err != nil {
		return nil, failure(messages, err, clitext.ManageReadFailed, clitext.Params{clitext.ParamPath: pinFile, clitext.ParamErr: err.Error()})
	}
	return readPins(messages, string(src))
}

func compiledIn(messages *clitext.Messages, dir, module string) error {
	src, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return failure(messages, err, clitext.ManageReadFailed, clitext.Params{clitext.ParamPath: manifestFile, clitext.ParamErr: err.Error()})
	}
	mods, err := listPlugins(messages, string(src))
	if err != nil {
		return err
	}
	for _, m := range mods {
		if m == module {
			return nil
		}
	}
	return failure(messages, errNotCompiledIn, clitext.ManageNotCompiledIn, clitext.Params{clitext.ParamModule: module})
}

func pinModule(messages *clitext.Messages, dir, module string, pinned bool) error {
	if err := compiledIn(messages, dir, module); err != nil {
		return err
	}
	pins, err := loadPins(messages, dir)
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
	return os.WriteFile(filepath.Join(dir, pinFile), []byte(writePins(pins)), modeFile)
}

func run(ctx context.Context, messages *clitext.Messages, errOut *errwriter.Writer, dir, name string, args ...string) int {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	var line strings.Builder
	line.WriteString("+ ")
	line.WriteString(name)
	for _, arg := range args {
		_ = line.WriteByte(' ')
		line.WriteString(arg)
	}
	errOut.Println(line.String())
	if err := cmd.Run(); err != nil {
		errOut.Println(failure(messages, err, clitext.ManageCommandFailed, clitext.Params{clitext.ParamName: name, clitext.ParamErr: err.Error()}))
		return 1
	}
	return 0
}
