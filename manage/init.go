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
	"github.com/Herrscherd/herrscher/core/host"
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
			kindDiscord: "github.com/Herrscherd/herrscher-discord-gateway",
		},
		catBackend: {
			kindClaude: "github.com/Herrscherd/herrscher-claude-backend",
			kindCodex:  "github.com/Herrscherd/herrscher-codex-backend",
			kindCursor: "github.com/Herrscherd/herrscher-cursor-backend",
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

// InitCmd composes the host's plugin stack from scratch: it picks one module per
// category (defaulting to the batteries-included stack), rewrites plugins.go to
// exactly that set, seeds a .env from .env.example, then go-gets and rebuilds.
// On a terminal with no stack flag it opens the interactive setup instead.
// Pass --gateway/--backend/--memory/--orchestrator to swap a category, the kind
// "none" to drop one, or --with MODULE to pin an extra module verbatim.
func InitCmd(ctx context.Context, messages *clitext.Messages, hostMessages *host.Messages, args []string) int {
	out, errOut := errwriter.New(os.Stdout), errwriter.New(os.Stderr)
	return errwriter.Finish(initHost(ctx, messages, hostMessages, out, errOut, args), out, errOut)
}

func initHost(ctx context.Context, messages *clitext.Messages, hostMessages *host.Messages, out, errOut *errwriter.Writer, args []string) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	hostDir := flags.String("host", "", msg(messages, clitext.ManageFlagHost, nil))
	noBuild := flags.Bool("no-build", false, msg(messages, clitext.ManageFlagInitNoBuild, nil))
	list := flags.Bool("list", false, msg(messages, clitext.ManageFlagList, nil))
	yes := flags.Bool("yes", false, msg(messages, clitext.ManageFlagInitYes, nil))
	gateway := flags.String(catGateway, defaultStack()[catGateway], msg(messages, clitext.ManageFlagCategory, clitext.Params{clitext.ParamCategory: catGateway}))
	backend := flags.String(catBackend, defaultStack()[catBackend], msg(messages, clitext.ManageFlagCategory, clitext.Params{clitext.ParamCategory: catBackend}))
	memory := flags.String(catMemory, defaultStack()[catMemory], msg(messages, clitext.ManageFlagCategory, clitext.Params{clitext.ParamCategory: catMemory}))
	orchestrator := flags.String(catOrchestrator, defaultStack()[catOrchestrator], msg(messages, clitext.ManageFlagCategory, clitext.Params{clitext.ParamCategory: catOrchestrator}))
	extractor := flags.String(catExtractor, defaultStack()[catExtractor], msg(messages, clitext.ManageFlagCategory, clitext.Params{clitext.ParamCategory: catExtractor}))
	skills := flags.String(catSkills, defaultStack()[catSkills], msg(messages, clitext.ManageFlagCategory, clitext.Params{clitext.ParamCategory: catSkills}))
	compactor := flags.String(catCompactor, defaultStack()[catCompactor], msg(messages, clitext.ManageFlagCategory, clitext.Params{clitext.ParamCategory: catCompactor}))
	var extras multiFlag
	flags.Var(&extras, "with", msg(messages, clitext.ManageFlagWith, nil))
	if err := flags.Parse(args); err != nil {
		return parseExit(err)
	}

	if *list {
		writeCatalog(messages, out)
		return 0
	}

	dir, hostErr := resolveHost(messages, *hostDir)
	compose := hostErr == nil
	interactive := !*yes && !stackFlagsSet(flags) && isTerminal(os.Stdin) && isTerminal(os.Stderr)

	choices := map[string]string{
		catGateway:      *gateway,
		catBackend:      *backend,
		catMemory:       *memory,
		catOrchestrator: *orchestrator,
		catExtractor:    *extractor,
		catSkills:       *skills,
		catCompactor:    *compactor,
	}
	if interactive {
		return runSetup(ctx, messages, errOut, liveDeps(messages, hostMessages, *hostDir), screenWelcome)
	}
	var secrets map[string]string

	if !compose {
		return writeSecretsOnly(messages, errOut, secrets)
	}

	modules, err := resolveStack(messages, choices, extras)
	if err != nil {
		errOut.Println(err)
		return exitUsage
	}

	manifest := filepath.Join(dir, manifestFile)
	src, code := rewritePlugins(messages, errOut, manifest, modules)
	if code != 0 {
		return code
	}
	summaryStack(messages, modules)
	seedEnv(messages, out, errOut, dir)
	writeProjectSecrets(messages, out, errOut, dir, secrets)

	if *noBuild {
		out.Println(msg(messages, clitext.ManageInitWroteNoBuild, nil))
		return 0
	}
	return buildOrRestore(ctx, messages, errOut, dir, manifest, src, modules)
}

func writeSecretsOnly(messages *clitext.Messages, errOut *errwriter.Writer, secrets map[string]string) int {
	path := envTarget()
	written, err := writeSecretsTo(path, secrets)
	if err != nil {
		errOut.Println(failure(messages, err, clitext.ManageEnvWriteFailed, clitext.Params{clitext.ParamErr: err.Error()}))
		return 1
	}
	summaryConfig(messages, path, written)
	return 0
}

func rewritePlugins(messages *clitext.Messages, errOut *errwriter.Writer, manifest string, modules []string) ([]byte, int) {
	src, err := os.ReadFile(manifest)
	if err != nil {
		errOut.Println(failure(messages, err, clitext.ManageReadFailed, clitext.Params{clitext.ParamPath: manifest, clitext.ParamErr: err.Error()}))
		return nil, 1
	}
	rendered, err := setPlugins(messages, string(src), modules)
	if err != nil {
		errOut.Println(err)
		return nil, 1
	}
	if err := os.WriteFile(manifest, []byte(rendered), modeFile); err != nil {
		errOut.Println(failure(messages, err, clitext.ManageWriteFailed, clitext.Params{clitext.ParamPath: manifest, clitext.ParamErr: err.Error()}))
		return nil, 1
	}
	return src, 0
}

func writeProjectSecrets(messages *clitext.Messages, out, errOut *errwriter.Writer, dir string, secrets map[string]string) {
	if written, err := writeSecretsTo(filepath.Join(dir, ".env"), secrets); err != nil {
		errOut.Println(failure(messages, err, clitext.ManageEnvWriteFailed, clitext.Params{clitext.ParamErr: err.Error()}))
	} else if written > 0 {
		out.Println(msg(messages, clitext.ManageWroteSettings, clitext.Params{clitext.ParamCount: written, clitext.ParamPath: ".env"}))
	}
}

func buildOrRestore(ctx context.Context, messages *clitext.Messages, errOut *errwriter.Writer, dir, manifest string, src []byte, modules []string) int {
	code := buildStack(ctx, messages, errOut, dir, modules)
	if code == 0 {
		return 0
	}
	if werr := os.WriteFile(manifest, src, modeFile); werr != nil {
		errOut.Println(failure(messages, werr, clitext.ManageRestoreFailed, clitext.Params{clitext.ParamPath: manifest, clitext.ParamErr: werr.Error()}))
	} else {
		errOut.Println(msg(messages, clitext.ManageBuildRestored, clitext.Params{clitext.ParamPath: manifest}))
	}
	return code
}

func buildStack(ctx context.Context, messages *clitext.Messages, errOut *errwriter.Writer, dir string, modules []string) int {
	for _, module := range modules {
		if module == hostModule || strings.HasPrefix(module, hostModule+"/") {
			continue
		}
		if code := run(ctx, messages, errOut, dir, "go", verbGet, "--", module); code != 0 {
			return code
		}
	}
	if code := run(ctx, messages, errOut, dir, "go", "mod", "tidy"); code != 0 {
		return code
	}
	return run(ctx, messages, errOut, dir, "go", "build", "./...")
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

func writeSecretsTo(path string, secrets map[string]string) (int, error) {
	if len(secrets) == 0 {
		return 0, nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, modeDirPrivate); err != nil {
			return 0, err
		}
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
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
	out := strings.Join(lines, "\n") + "\n"
	return len(secrets), os.WriteFile(path, []byte(out), modeSecret)
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
