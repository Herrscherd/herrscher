package manage

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/internal/errwriter"
)

var (
	errUnknownKind       = clitext.SentinelError("unknown kind")
	errInvalidModulePath = clitext.SentinelError("invalid module path")
	errEmptyStack        = clitext.SentinelError("empty stack: every category was set to none")
)

func catalog() map[string]map[string]string {
	return map[string]map[string]string{
		catGateway: {
			kindDiscord:  "github.com/Herrscherd/herrscher-discord-gateway",
			kindTelegram: "github.com/Herrscherd/herrscher-telegram-gateway",
		},
		catBackend: {
			kindClaude:      "github.com/Herrscherd/herrscher-claude-backend",
			kindCodex:       "github.com/Herrscherd/herrscher-codex-backend",
			kindCursor:      "github.com/Herrscherd/herrscher-cursor-backend",
			kindGrok:        "github.com/Herrscherd/herrscher-grok-backend",
			kindAntigravity: "github.com/Herrscherd/herrscher-antigravity-backend",
		},
		catMemory: {
			kindObsidian: "github.com/Herrscherd/herrscher-obsidian-memory",
		},
		catOrchestrator: {
			kindBasic: "github.com/Herrscherd/herrscher-orchestrator",
		},
		catExtractor: {
			kindLLM: "github.com/Herrscherd/herrscher-llm-extractor",
		},
		catSkills: {
			kindSuperset: "github.com/Herrscherd/herrscher-superset-skills",
		},
		catCompactor: {
			kindJev: "github.com/Herrscherd/herrscher-jev-compactor",
		},
	}
}

func inTree() []string {
	return []string{"github.com/Herrscherd/herrscher/plugins/terminal"}
}

const hostModule = "github.com/Herrscherd/herrscher"

func categories() []string {
	return []string{catGateway, catBackend, catMemory, catOrchestrator, catExtractor, catSkills, catCompactor}
}

func defaultStack() map[string]string {
	return map[string]string{
		catGateway:      kindDiscord,
		catBackend:      kindClaude,
		catMemory:       kindObsidian,
		catOrchestrator: kindBasic,
		catExtractor:    kindLLM,
		catSkills:       kindSuperset,
		catCompactor:    kindJev,
	}
}

type stackOptions struct {
	hostDir string
	noBuild bool
	noTrial bool
	choices map[string]string
	extras  []string
}

func composeStack(ctx context.Context, messages *clitext.Messages, out, errOut *errwriter.Writer, opts stackOptions) int {
	modules, err := resolveStack(messages, opts.choices, opts.extras)
	if err != nil {
		errOut.Println(err)
		return exitUsage
	}
	trial := func(code int) int {
		if code != 0 || opts.noTrial {
			return code
		}
		return trialStack(ctx, messages, out, errOut, modules)
	}
	dir, hostErr := resolveHost(messages, opts.hostDir)
	if hostErr != nil {
		summaryConfig(messages, envTarget(), 0)
		return trial(0)
	}

	saved, err := saveComposition(messages, dir)
	if err != nil {
		errOut.Println(err)
		return 1
	}
	manifest := filepath.Join(dir, manifestFile)
	if code := rewritePlugins(messages, errOut, manifest, modules); code != 0 {
		return code
	}
	summaryStack(messages, modules)
	seedEnv(messages, out, errOut, dir)

	if opts.noBuild {
		out.Println(msg(messages, clitext.ManageSetupWroteNoBuild, nil))
		return trial(0)
	}
	return trial(buildOrRestore(ctx, messages, errOut, dir, saved, modules))
}

func rewritePlugins(messages *clitext.Messages, errOut *errwriter.Writer, manifest string, modules []string) int {
	src, err := os.ReadFile(manifest)
	if err != nil {
		errOut.Println(failure(messages, err, clitext.ManageReadFailed, clitext.Params{clitext.ParamPath: manifest, clitext.ParamErr: err.Error()}))
		return 1
	}
	rendered, err := setPlugins(messages, string(src), modules)
	if err != nil {
		errOut.Println(err)
		return 1
	}
	if err := os.WriteFile(manifest, []byte(rendered), modeFile); err != nil {
		errOut.Println(failure(messages, err, clitext.ManageWriteFailed, clitext.Params{clitext.ParamPath: manifest, clitext.ParamErr: err.Error()}))
		return 1
	}
	return 0
}

func buildOrRestore(ctx context.Context, messages *clitext.Messages, errOut *errwriter.Writer, dir string, saved map[string][]byte, modules []string) int {
	target := runningBinary(dir)
	for _, command := range stackCommands(dir, target, modules) {
		if code := run(ctx, messages, errOut, command.dir, command.name, command.args...); code != 0 {
			restoreAfterFailedBuild(messages, errOut, dir, saved)
			return code
		}
	}
	if err := swapBinary(target); err != nil {
		errOut.Println(failure(messages, err, clitext.ManageWriteFailed, clitext.Params{clitext.ParamPath: target, clitext.ParamErr: err.Error()}))
		restoreAfterFailedBuild(messages, errOut, dir, saved)
		return 1
	}
	return 0
}

func restoreAfterFailedBuild(messages *clitext.Messages, errOut *errwriter.Writer, dir string, saved map[string][]byte) {
	if err := restoreComposition(messages, dir, saved); err != nil {
		errOut.Println(err)
		return
	}
	errOut.Println(msg(messages, clitext.ManageBuildRestored, clitext.Params{clitext.ParamPath: manifestFile}))
}

func stackCommands(dir, target string, modules []string) []setupCommand {
	var commands []setupCommand
	for _, module := range modules {
		if module == hostModule || strings.HasPrefix(module, hostModule+"/") {
			continue
		}
		commands = append(commands, goCommand(dir, verbGet, "--", module))
	}
	return append(commands, goCommand(dir, "mod", "tidy"), goCommand(dir, "build", "-o", freshBinary(target), "."))
}

func goCommand(dir string, args ...string) setupCommand {
	return setupCommand{dir: dir, name: "go", args: args}
}

func resolveStack(messages *clitext.Messages, choices map[string]string, extras []string) ([]string, error) {
	var modules []string
	seen := make(map[string]bool)
	add := func(module string) {
		if !seen[module] {
			seen[module] = true
			modules = append(modules, module)
		}
	}
	for _, m := range inTree() {
		add(m)
	}
	for _, cat := range categories() {
		kind := choices[cat]
		if kind == "" || kind == kindNone {
			continue
		}
		module, ok := catalog()[cat][kind]
		if !ok {
			return nil, failure(messages, errUnknownKind, clitext.ManageUnknownKindListed, clitext.Params{clitext.ParamCategory: cat, clitext.ParamKind: strconv.Quote(kind)})
		}
		add(module)
	}
	for _, module := range extras {
		if module == "" || module[0] == '-' {
			return nil, failure(messages, errInvalidModulePath, clitext.ManageInvalidModulePath, clitext.Params{clitext.ParamModule: strconv.Quote(module)})
		}
		add(module)
	}
	if len(modules) == 0 {
		return nil, failure(messages, errEmptyStack, clitext.ManageEmptyStack, nil)
	}
	return modules, nil
}

func stackFlagsSet(flags *flag.FlagSet) bool {
	set := false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case catGateway, catBackend, catMemory, catOrchestrator, catExtractor, catSkills, catCompactor, "with":
			set = true
		}
	})
	return set
}

func envTarget() string {
	if p := os.Getenv("HERRSCHER_ENV_FILE"); p != "" {
		return p
	}
	return ".env"
}

func writeSecretsTo(path string, secrets map[string]string) error {
	if len(secrets) == 0 {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, modeDirPrivate); err != nil {
			return err
		}
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	lines = mergeSecrets(lines, secrets)
	out := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(out), modeSecret); err != nil {
		return err
	}
	return os.Chmod(path, modeSecret)
}

func mergeSecrets(lines []string, secrets map[string]string) []string {
	done := make(map[string]bool)
	for idx, line := range lines {
		key := line
		if eq := strings.IndexByte(line, '='); eq >= 0 {
			key = strings.TrimSpace(strings.TrimPrefix(line[:eq], "export "))
		}
		if v, ok := secrets[key]; ok {
			lines[idx] = key + "=" + v
			done[key] = true
		}
	}
	keys := make([]string, 0, len(secrets))
	for name := range secrets {
		if !done[name] {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, k+"="+secrets[k])
	}
	return lines
}

func seedEnv(messages *clitext.Messages, out, errOut *errwriter.Writer, dir string) {
	dst := filepath.Join(dir, ".env")
	if _, err := os.Stat(dst); err == nil {
		return
	}
	src, err := os.ReadFile(filepath.Join(dir, ".env.example"))
	if err != nil {
		return
	}
	if err := os.WriteFile(dst, src, modeSecret); err != nil {
		errOut.Println(failure(messages, err, clitext.ManageSeedFailed, clitext.Params{clitext.ParamPath: dst, clitext.ParamErr: err.Error()}))
		return
	}
	out.Println(msg(messages, clitext.ManageSeeded, clitext.Params{clitext.ParamPath: dst}))
}

func writeCatalog(messages *clitext.Messages, out *errwriter.Writer) {
	translator := messages.For(clitext.ClientLanguage())
	defer logRenderErr(translator)
	for _, cat := range categories() {
		kinds := make([]string, 0, len(catalog()[cat]))
		for k := range catalog()[cat] {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			def := ""
			if defaultStack()[cat] == kind {
				def = " " + translator.Text(clitext.ManageDefaultTag, nil)
			}
			out.Printf("%-13s %-10s %s%s\n", cat, kind, catalog()[cat][kind], def)
		}
	}
}

type multiFlag []string

func (m *multiFlag) String() string { return fmt.Sprint([]string(*m)) }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}
