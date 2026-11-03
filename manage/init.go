package manage

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Herrscherd/herrscher/internal/errwriter"
)

var (
	errUnknownKind       = errors.New("unknown kind")
	errInvalidModulePath = errors.New("invalid module path")
	errEmptyStack        = errors.New("empty stack: every category was set to none")
)

func catalog() map[string]map[string]string {
	return map[string]map[string]string{
		catGateway: {
			kindDiscord: "github.com/Herrscherd/herrscher-discord-gateway",
		},
		catBackend: {
			"claude": "github.com/Herrscherd/herrscher-claude-backend",
			"codex":  "github.com/Herrscherd/herrscher-codex-backend",
			"cursor": "github.com/Herrscherd/herrscher-cursor-backend",
		},
		catMemory: {
			"obsidian": "github.com/Herrscherd/herrscher-obsidian-memory",
		},
		catOrchestrator: {
			"basic": "github.com/Herrscherd/herrscher-orchestrator",
		},
		catExtractor: {
			"llm": "github.com/Herrscherd/herrscher-llm-extractor",
		},
		catSkills: {
			"superset": "github.com/Herrscherd/herrscher-superset-skills",
		},
	}
}

func inTree() []string {
	return []string{"github.com/Herrscherd/herrscher/plugins/terminal"}
}

const hostModule = "github.com/Herrscherd/herrscher"

func categories() []string {
	return []string{catGateway, catBackend, catMemory, catOrchestrator, catExtractor, catSkills}
}

func defaultStack() map[string]string {
	return map[string]string{
		catGateway:      kindDiscord,
		catBackend:      "claude",
		catMemory:       "obsidian",
		catOrchestrator: "basic",
		catExtractor:    "llm",
		catSkills:       "superset",
	}
}

// InitCmd composes the host's plugin stack from scratch: it picks one module per
// category (defaulting to the batteries-included stack), rewrites plugins.go to
// exactly that set, seeds a .env from .env.example, asks where agent turns run
// (route policy, gateway credentials, default model), then go-gets and rebuilds.
// Pass --gateway/--backend/--memory/--orchestrator to swap a category, the kind
// "none" to drop one, or --with MODULE to pin an extra module verbatim.
func InitCmd(ctx context.Context, args []string) int {
	out, errOut := errwriter.New(os.Stdout), errwriter.New(os.Stderr)
	return errwriter.Finish(initHost(ctx, out, errOut, args), out, errOut)
}

func initHost(ctx context.Context, out, errOut *errwriter.Writer, args []string) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	hostDir := flags.String("host", "", "path to the host module")
	noBuild := flags.Bool("no-build", false, "write plugins.go but skip go get/build")
	list := flags.Bool("list", false, "list the module catalog() and exit")
	yes := flags.Bool("yes", false, "skip the interactive wizard; use flags/defaults")
	gateway := flags.String(catGateway, defaultStack()[catGateway], "gateway module kind (or none)")
	backend := flags.String(catBackend, defaultStack()[catBackend], "backend module kind (or none)")
	memory := flags.String(catMemory, defaultStack()[catMemory], "memory module kind (or none)")
	orchestrator := flags.String(catOrchestrator, defaultStack()[catOrchestrator], "orchestrator module kind (or none)")
	extractor := flags.String(catExtractor, defaultStack()[catExtractor], "extractor module kind (or none)")
	skills := flags.String(catSkills, defaultStack()[catSkills], "skills module kind (or none)")
	var extras multiFlag
	flags.Var(&extras, "with", "pin an extra module path verbatim (repeatable)")
	if err := flags.Parse(args); err != nil {
		return parseExit(err)
	}

	if *list {
		writeCatalog(out)
		return 0
	}

	dir, hostErr := resolveHost(*hostDir)
	compose := hostErr == nil
	interactive := !*yes && !stackFlagsSet(flags) && isTerminal(os.Stdin) && isTerminal(os.Stderr)

	choices := map[string]string{
		catGateway:      *gateway,
		catBackend:      *backend,
		catMemory:       *memory,
		catOrchestrator: *orchestrator,
		catExtractor:    *extractor,
		catSkills:       *skills,
	}
	var secrets map[string]string
	if interactive {
		var ok bool
		choices, secrets, ok = runInitWizard(errOut, compose, choices)
		if !ok {
			return 1
		}
	}

	if !compose {
		return writeSecretsOnly(errOut, secrets)
	}

	modules, err := resolveStack(choices, extras)
	if err != nil {
		errOut.Println(err)
		return exitUsage
	}

	manifest := filepath.Join(dir, "plugins.go")
	src, code := rewritePlugins(errOut, manifest, modules)
	if code != 0 {
		return code
	}
	summaryStack(modules)
	seedEnv(out, errOut, dir)
	writeProjectSecrets(out, errOut, dir, secrets)

	if *noBuild {
		out.Println("wrote plugins.go (--no-build); run `go mod tidy && go build` in the host to apply")
		return 0
	}
	return buildOrRestore(ctx, errOut, dir, manifest, src, modules)
}

func runInitWizard(errOut *errwriter.Writer, compose bool, choices map[string]string) (map[string]string, map[string]string, bool) {
	wizardChoices, wizardSecrets, err := runWizard(compose, liveCatalog, currentRoute())
	if err != nil {
		errOut.Println(err)
		return choices, nil, false
	}
	if compose {
		choices = wizardChoices
	}
	return choices, wizardSecrets, true
}

func writeSecretsOnly(errOut *errwriter.Writer, secrets map[string]string) int {
	path := envTarget()
	written, err := writeSecretsTo(path, secrets)
	if err != nil {
		errOut.Printf("write secrets: %v\n", err)
		return 1
	}
	summaryConfig(path, written)
	return 0
}

func rewritePlugins(errOut *errwriter.Writer, manifest string, modules []string) ([]byte, int) {
	src, err := os.ReadFile(manifest)
	if err != nil {
		errOut.Printf("read %s: %v\n", manifest, err)
		return nil, 1
	}
	rendered, err := setPlugins(string(src), modules)
	if err != nil {
		errOut.Println(err)
		return nil, 1
	}
	if err := os.WriteFile(manifest, []byte(rendered), modeFile); err != nil {
		errOut.Printf("write %s: %v\n", manifest, err)
		return nil, 1
	}
	return src, 0
}

func writeProjectSecrets(out, errOut *errwriter.Writer, dir string, secrets map[string]string) {
	if written, err := writeSecretsTo(filepath.Join(dir, ".env"), secrets); err != nil {
		errOut.Printf("write secrets: %v\n", err)
	} else if written > 0 {
		out.Printf("wrote %d setting(s) to .env\n", written)
	}
}

func buildOrRestore(ctx context.Context, errOut *errwriter.Writer, dir, manifest string, src []byte, modules []string) int {
	code := buildStack(ctx, errOut, dir, modules)
	if code == 0 {
		return 0
	}
	if werr := os.WriteFile(manifest, src, modeFile); werr != nil {
		errOut.Printf("restore %s: %v\n", manifest, werr)
	} else {
		errOut.Printf("build failed; restored %s\n", manifest)
	}
	return code
}

func buildStack(ctx context.Context, errOut *errwriter.Writer, dir string, modules []string) int {
	for _, module := range modules {
		if module == hostModule || strings.HasPrefix(module, hostModule+"/") {
			continue
		}
		if code := run(ctx, errOut, dir, "go", verbGet, "--", module); code != 0 {
			return code
		}
	}
	if code := run(ctx, errOut, dir, "go", "mod", "tidy"); code != 0 {
		return code
	}
	return run(ctx, errOut, dir, "go", "build", "./...")
}

func resolveStack(choices map[string]string, extras []string) ([]string, error) {
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
			return nil, fmt.Errorf("%s: %w %q (try `herrscher init --list`)", cat, errUnknownKind, kind)
		}
		add(module)
	}
	for _, module := range extras {
		if module == "" || module[0] == '-' {
			return nil, fmt.Errorf("%w %q", errInvalidModulePath, module)
		}
		add(module)
	}
	if len(modules) == 0 {
		return nil, errEmptyStack
	}
	return modules, nil
}

func stackFlagsSet(flags *flag.FlagSet) bool {
	set := false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case catGateway, catBackend, catMemory, catOrchestrator, catExtractor, catSkills, "with":
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

func seedEnv(out, errOut *errwriter.Writer, dir string) {
	dst := filepath.Join(dir, ".env")
	if _, err := os.Stat(dst); err == nil {
		return
	}
	src, err := os.ReadFile(filepath.Join(dir, ".env.example"))
	if err != nil {
		return
	}
	if err := os.WriteFile(dst, src, modeSecret); err != nil {
		errOut.Printf("seed %s: %v\n", dst, err)
		return
	}
	out.Printf("seeded %s from .env.example: fill in your secrets\n", dst)
}

func writeCatalog(out *errwriter.Writer) {
	for _, cat := range categories() {
		kinds := make([]string, 0, len(catalog()[cat]))
		for k := range catalog()[cat] {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			def := ""
			if defaultStack()[cat] == kind {
				def = " (default)"
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
