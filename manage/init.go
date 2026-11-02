package manage

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	errUnknownKind       = errors.New("unknown kind")
	errInvalidModulePath = errors.New("invalid module path")
	errEmptyStack        = errors.New("empty stack: every category was set to none")
)

func catalog() map[string]map[string]string {
	return map[string]map[string]string{
		"gateway": {
			"discord": "github.com/Herrscherd/herrscher-discord-gateway",
		},
		"backend": {
			"claude": "github.com/Herrscherd/herrscher-claude-backend",
			"codex":  "github.com/Herrscherd/herrscher-codex-backend",
			"cursor": "github.com/Herrscherd/herrscher-cursor-backend",
		},
		"memory": {
			"obsidian": "github.com/Herrscherd/herrscher-obsidian-memory",
		},
		"orchestrator": {
			"basic": "github.com/Herrscherd/herrscher-orchestrator",
		},
		"extractor": {
			"llm": "github.com/Herrscherd/herrscher-llm-extractor",
		},
		"skills": {
			"superset": "github.com/Herrscherd/herrscher-superset-skills",
		},
	}
}

func inTree() []string {
	return []string{"github.com/Herrscherd/herrscher/plugins/terminal"}
}

const hostModule = "github.com/Herrscherd/herrscher"

func categories() []string {
	return []string{"gateway", "backend", "memory", "orchestrator", "extractor", "skills"}
}

func defaultStack() map[string]string {
	return map[string]string{
		"gateway":      "discord",
		"backend":      "claude",
		"memory":       "obsidian",
		"orchestrator": "basic",
		"extractor":    "llm",
		"skills":       "superset",
	}
}

// InitCmd composes the host's plugin stack from scratch: it picks one module per
// category (defaulting to the batteries-included stack), rewrites plugins.go to
// exactly that set, seeds a .env from .env.example, asks where agent turns run
// (route policy, gateway credentials, default model), then go-gets and rebuilds.
// Pass --gateway/--backend/--memory/--orchestrator to swap a category, the kind
// "none" to drop one, or --with MODULE to pin an extra module verbatim.
func InitCmd(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	hostDir := fs.String("host", "", "path to the host module")
	noBuild := fs.Bool("no-build", false, "write plugins.go but skip go get/build")
	list := fs.Bool("list", false, "list the module catalog() and exit")
	yes := fs.Bool("yes", false, "skip the interactive wizard; use flags/defaults")
	gateway := fs.String("gateway", defaultStack()["gateway"], "gateway module kind (or none)")
	backend := fs.String("backend", defaultStack()["backend"], "backend module kind (or none)")
	memory := fs.String("memory", defaultStack()["memory"], "memory module kind (or none)")
	orchestrator := fs.String("orchestrator", defaultStack()["orchestrator"], "orchestrator module kind (or none)")
	extractor := fs.String("extractor", defaultStack()["extractor"], "extractor module kind (or none)")
	skills := fs.String("skills", defaultStack()["skills"], "skills module kind (or none)")
	var extras multiFlag
	fs.Var(&extras, "with", "pin an extra module path verbatim (repeatable)")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}

	if *list {
		writeCatalog(os.Stdout)
		return 0
	}

	dir, hostErr := resolveHost(*hostDir)
	compose := hostErr == nil
	interactive := !*yes && !stackFlagsSet(fs) && isTerminal(os.Stdin) && isTerminal(os.Stderr)

	choices := map[string]string{
		"gateway":      *gateway,
		"backend":      *backend,
		"memory":       *memory,
		"orchestrator": *orchestrator,
		"extractor":    *extractor,
		"skills":       *skills,
	}
	var secrets map[string]string
	if interactive {
		wc, ws, err := runWizard(compose, liveCatalog, currentRoute())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		secrets = ws
		if compose {
			choices = wc
		}
	}

	if !compose {
		path := envTarget()
		n, err := writeSecretsTo(path, secrets)
		if err != nil {
			fmt.Fprintf(os.Stderr, "write secrets: %v\n", err)
			return 1
		}
		summaryConfig(path, n)
		return 0
	}

	modules, err := resolveStack(choices, extras)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	manifest := filepath.Join(dir, "plugins.go")
	src, err := os.ReadFile(manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", manifest, err)
		return 1
	}
	out, err := setPlugins(string(src), modules)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(manifest, []byte(out), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", manifest, err)
		return 1
	}
	summaryStack(modules)
	seedEnv(dir)
	if n, err := writeSecretsTo(filepath.Join(dir, ".env"), secrets); err != nil {
		fmt.Fprintf(os.Stderr, "write secrets: %v\n", err)
	} else if n > 0 {
		fmt.Fprintf(os.Stdout, "wrote %d setting(s) to .env\n", n)
	}

	if *noBuild {
		fmt.Fprintln(os.Stdout, "wrote plugins.go (--no-build); run `go mod tidy && go build` in the host to apply")
		return 0
	}

	if code := buildStack(ctx, dir, modules); code != 0 {
		if werr := os.WriteFile(manifest, src, 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "restore %s: %v\n", manifest, werr)
		} else {
			fmt.Fprintf(os.Stderr, "build failed; restored %s\n", manifest)
		}
		return code
	}
	return 0
}

func buildStack(ctx context.Context, dir string, modules []string) int {
	for _, m := range modules {
		if m == hostModule || strings.HasPrefix(m, hostModule+"/") {
			continue
		}
		if code := run(ctx, dir, "go", "get", "--", m); code != 0 {
			return code
		}
	}
	if code := run(ctx, dir, "go", "mod", "tidy"); code != 0 {
		return code
	}
	return run(ctx, dir, "go", "build", "./...")
}

func resolveStack(choices map[string]string, extras []string) ([]string, error) {
	var modules []string
	seen := map[string]bool{}
	add := func(m string) {
		if !seen[m] {
			seen[m] = true
			modules = append(modules, m)
		}
	}
	for _, m := range inTree() {
		add(m)
	}
	for _, cat := range categories() {
		kind := choices[cat]
		if kind == "" || kind == "none" {
			continue
		}
		module, ok := catalog()[cat][kind]
		if !ok {
			return nil, fmt.Errorf("%s: %w %q (try `herrscher init --list`)", cat, errUnknownKind, kind)
		}
		add(module)
	}
	for _, m := range extras {
		if m == "" || m[0] == '-' {
			return nil, fmt.Errorf("%w %q", errInvalidModulePath, m)
		}
		add(m)
	}
	if len(modules) == 0 {
		return nil, errEmptyStack
	}
	return modules, nil
}

func stackFlagsSet(fs *flag.FlagSet) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "gateway", "backend", "memory", "orchestrator", "extractor", "skills", "with":
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
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return 0, err
		}
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	done := map[string]bool{}
	for i, ln := range lines {
		key := ln
		if eq := strings.IndexByte(ln, '='); eq >= 0 {
			key = strings.TrimSpace(strings.TrimPrefix(ln[:eq], "export "))
		}
		if v, ok := secrets[key]; ok {
			lines[i] = key + "=" + v
			done[key] = true
		}
	}
	keys := make([]string, 0, len(secrets))
	for k := range secrets {
		if !done[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, k+"="+secrets[k])
	}
	out := strings.Join(lines, "\n") + "\n"
	return len(secrets), os.WriteFile(path, []byte(out), 0o600)
}

func seedEnv(dir string) {
	dst := filepath.Join(dir, ".env")
	if _, err := os.Stat(dst); err == nil {
		return
	}
	src, err := os.ReadFile(filepath.Join(dir, ".env.example"))
	if err != nil {
		return
	}
	if err := os.WriteFile(dst, src, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "seed %s: %v\n", dst, err)
		return
	}
	fmt.Fprintf(os.Stdout, "seeded %s from .env.example: fill in your secrets\n", dst)
}

func writeCatalog(out io.Writer) {
	for _, cat := range categories() {
		kinds := make([]string, 0, len(catalog()[cat]))
		for k := range catalog()[cat] {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			def := ""
			if defaultStack()[cat] == k {
				def = " (default)"
			}
			fmt.Fprintf(out, "%-13s %-10s %s%s\n", cat, k, catalog()[cat][k], def)
		}
	}
}

type multiFlag []string

func (m *multiFlag) String() string { return fmt.Sprint([]string(*m)) }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}
