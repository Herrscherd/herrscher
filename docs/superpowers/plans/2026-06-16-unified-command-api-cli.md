# Unified Command API + CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Discord-slash command abstraction with one neutral command API (a `Cmd` builder in `contracts`, a native dispatcher in `core/cli`) and drive the daemon's commands from the CLI.

**Architecture:** A command is declared once as a neutral `contracts.Cmd` (namespaced `Path`, `Param`s, a `Run` closure). `core/cli.Registry` holds them keyed by `Path` and dispatches argv. The manager's domain handlers are rewritten from the slash signature `func(ctx, contracts.Command) contracts.CommandResponse` to `func(ctx, contracts.Input) (string, error)`, registered as `Cmd`s. The legacy slash types are deleted; the slash dispatch loop is stripped from `serve.go`. Discord slash is intentionally dark until a later dctl phase.

**Tech Stack:** Go 1.23, standard library only. Tests with `go test`. Module: `github.com/Herrscherd/herrscher`; contracts module: `github.com/Herrscherd/herrscher-contracts` (sibling, wired via `replace`). Use `rtk proxy go test ./...` / `rtk proxy go build ./...` for raw output.

**Identity for every commit:** `git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit ...`

**Branch:** `feat/unified-command-api` (already checked out).

---

## Spec reference

`docs/superpowers/specs/2026-06-16-unified-command-api-cli-design.md`

## File structure

**contracts module** (`/home/shan/dev/herrscher-contracts/`):
- Create `command_api.go` — the neutral `Cmd`, `Param`, `Input`, `Builder`. The single command concept.
- Create `command_api_test.go` — builder + Input tests.
- Delete from `command.go` — `Command`, `CommandData`, `Option`, `OptionType`/`Opt*`, `CommandResponse`, `CommandKind`/`Kind*`, `Responder`, `InboundCommand`. Keep `Choice` if it lives here (used by `MenuRouter`); see Task 8.
- Modify `host.go` — delete `CommandRegistrar`.

**herrscher module** (`/home/shan/dev/herrscher/`):
- Create `core/cli/registry.go` — `Registry`, `Add`, `Dispatch`, `Help`.
- Create `core/cli/registry_test.go`.
- Rewrite `core/internal/manager/{handler,set,session,service,workspace,autocomplete}.go` onto `Cmd`/`Input`.
- Modify `core/internal/manager/handler_test.go` — drive handlers via `Input`, assert `(string, error)`.
- Add `core/internal/manager/commands.go` — `Commands(h *Handler) []contracts.Cmd`, the declarations.
- Strip slash dispatch from `core/host/serve.go`.
- Rewrite `main.go` to build a `cli.Registry`, register core + `manage` commands, dispatch.
- Stub/trim the gateway plugin (`/home/shan/dev/herrscher-discord-gateway/{source.go,adapters.go}`) so it builds without the deleted types.

## Phasing

- **Phase 1 (Tasks 1–3):** additive framework. Repo stays green throughout.
- **Phase 2 (Tasks 4–10):** the breaking migration. The tree will not build mid-phase; that is expected and accepted (see spec). Green is restored at Task 10.

---

### Task 1: Neutral `Cmd` type + builder in contracts

**Files:**
- Create: `/home/shan/dev/herrscher-contracts/command_api.go`
- Test: `/home/shan/dev/herrscher-contracts/command_api_test.go`

- [ ] **Step 1: Write the failing test**

`/home/shan/dev/herrscher-contracts/command_api_test.go`:

```go
package contracts

import (
	"context"
	"testing"
)

func TestBuilderProducesCmd(t *testing.T) {
	c := New("session", "create").
		Help("start a session").
		Param("name", "session name", true).
		Param("shared", "use main checkout", false).
		Do(func(ctx context.Context, in Input) (string, error) {
			return "ok " + in.Get("name"), nil
		})

	if got := c.Path; len(got) != 2 || got[0] != "session" || got[1] != "create" {
		t.Fatalf("path = %v, want [session create]", got)
	}
	if c.Help != "start a session" {
		t.Fatalf("help = %q", c.Help)
	}
	if len(c.Params) != 2 || c.Params[0].Name != "name" || !c.Params[0].Required {
		t.Fatalf("params = %+v", c.Params)
	}
	if c.Params[1].Required {
		t.Fatal("shared must be optional")
	}
	out, err := c.Run(context.Background(), Input{Args: map[string]string{"name": "x"}})
	if err != nil || out != "ok x" {
		t.Fatalf("run = %q, %v", out, err)
	}
}

func TestInputAccessors(t *testing.T) {
	in := Input{Args: map[string]string{"name": "x", "flag": "true"}, Rest: []string{"a"}}
	if v, ok := in.Lookup("name"); !ok || v != "x" {
		t.Fatalf("Lookup name = %q,%v", v, ok)
	}
	if _, ok := in.Lookup("missing"); ok {
		t.Fatal("missing must report ok=false")
	}
	if in.Get("name") != "x" {
		t.Fatal("Get")
	}
	if !in.Bool("flag") || in.Bool("name") {
		t.Fatal("Bool: flag true, name not a bool")
	}
	if len(in.Rest) != 1 || in.Rest[0] != "a" {
		t.Fatal("Rest")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && rtk proxy go test ./... -run 'TestBuilder|TestInput'`
Expected: FAIL — `undefined: New`, `undefined: Input`.

- [ ] **Step 3: Write minimal implementation**

`/home/shan/dev/herrscher-contracts/command_api.go`:

```go
package contracts

import "context"

// Cmd is the one neutral command concept the platform exposes. A command is
// declared once — a namespaced Path, its Params, and the Run handler — and a
// format (the CLI today, a gateway binding later) resolves an invocation to it.
// The handler is opaque: whatever Run closes over (a Discord client, a backend),
// the registry that holds the Cmd stays agnostic of it.
type Cmd struct {
	Path   []string
	Params []Param
	Help   string
	Run    func(ctx context.Context, in Input) (string, error)
}

// Param is one declared input. Required params missing at dispatch are an error.
type Param struct {
	Name     string
	Help     string
	Required bool
}

// Input is the parsed, format-agnostic invocation handed to a handler. A CLI
// format fills it from argv; a future gateway fills it from an interaction.
type Input struct {
	Args map[string]string
	Rest []string
}

// Lookup returns a param value and whether it was supplied.
func (in Input) Lookup(name string) (string, bool) {
	v, ok := in.Args[name]
	return v, ok
}

// Get returns a param value, empty if absent.
func (in Input) Get(name string) string { return in.Args[name] }

// Bool reports whether a param was supplied as the literal "true".
func (in Input) Bool(name string) bool { return in.Args[name] == "true" }

// Builder fluently declares a Cmd.
type Builder struct{ c Cmd }

// New starts a command declaration under the given namespace path.
func New(path ...string) *Builder { return &Builder{c: Cmd{Path: path}} }

func (b *Builder) Help(text string) *Builder { b.c.Help = text; return b }

func (b *Builder) Param(name, help string, required bool) *Builder {
	b.c.Params = append(b.c.Params, Param{Name: name, Help: help, Required: required})
	return b
}

// Do sets the handler and returns the finished Cmd.
func (b *Builder) Do(fn func(ctx context.Context, in Input) (string, error)) Cmd {
	b.c.Run = fn
	return b.c
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-contracts && rtk proxy go test ./...`
Expected: PASS (whole module still green — purely additive).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add command_api.go command_api_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(contracts): neutral Cmd command API + builder"
```

---

### Task 2: `core/cli.Registry` — Add + duplicate rejection

**Files:**
- Create: `/home/shan/dev/herrscher/core/cli/registry.go`
- Test: `/home/shan/dev/herrscher/core/cli/registry_test.go`

- [ ] **Step 1: Write the failing test**

`/home/shan/dev/herrscher/core/cli/registry_test.go`:

```go
package cli_test

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

func leaf(path ...string) contracts.Cmd {
	return contracts.New(path...).Do(func(context.Context, contracts.Input) (string, error) {
		return "ran " + path[len(path)-1], nil
	})
}

func TestAddRejectsDuplicatePath(t *testing.T) {
	var r cli.Registry
	if err := r.Add(leaf("set", "home")); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(leaf("set", "home")); err == nil {
		t.Fatal("duplicate path must be rejected")
	}
}

func TestAddRejectsEmptyPath(t *testing.T) {
	var r cli.Registry
	if err := r.Add(contracts.New().Do(nil)); err == nil {
		t.Fatal("empty path must be rejected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && rtk proxy go test ./core/cli/`
Expected: FAIL — package `cli` does not exist.

- [ ] **Step 3: Write minimal implementation**

`/home/shan/dev/herrscher/core/cli/registry.go`:

```go
// Package cli is the native, channel-agnostic command dispatcher. It holds
// declared contracts.Cmd values keyed by their namespace Path and resolves an
// argv invocation to one. It imports only contracts: a command's Run may close
// over anything (a Discord client, a backend), but the registry never sees it.
package cli

import (
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Registry collects commands and dispatches argv to them.
type Registry struct {
	cmds []contracts.Cmd
}

func key(path []string) string { return strings.Join(path, " ") }

// Add registers a command. It rejects an empty path or a duplicate path.
func (r *Registry) Add(c contracts.Cmd) error {
	if len(c.Path) == 0 {
		return fmt.Errorf("cli: command with empty path")
	}
	for _, e := range r.cmds {
		if key(e.Path) == key(c.Path) {
			return fmt.Errorf("cli: duplicate command %q", key(c.Path))
		}
	}
	r.cmds = append(r.cmds, c)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && rtk proxy go test ./core/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/cli/registry.go core/cli/registry_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(core/cli): command registry with duplicate-path rejection"
```

---

### Task 3: `core/cli` — Dispatch (longest-path match, param parse, errors)

**Files:**
- Modify: `/home/shan/dev/herrscher/core/cli/registry.go`
- Test: `/home/shan/dev/herrscher/core/cli/registry_test.go`

- [ ] **Step 1: Write the failing test**

Append to `/home/shan/dev/herrscher/core/cli/registry_test.go`:

```go
func build(t *testing.T, cmds ...contracts.Cmd) *cli.Registry {
	t.Helper()
	var r cli.Registry
	for _, c := range cmds {
		if err := r.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	return &r
}

func TestDispatchResolvesLongestPath(t *testing.T) {
	got := ""
	r := build(t,
		contracts.New("session", "list").Do(func(_ context.Context, _ contracts.Input) (string, error) {
			got = "list"; return "", nil
		}),
		contracts.New("session", "create").Param("name", "", true).
			Do(func(_ context.Context, in contracts.Input) (string, error) {
				got = "create:" + in.Get("name"); return "", nil
			}),
	)
	if _, err := r.Dispatch(context.Background(), []string{"session", "create", "--name", "x"}); err != nil {
		t.Fatal(err)
	}
	if got != "create:x" {
		t.Fatalf("got %q", got)
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	r := build(t, leaf("session", "list"))
	if _, err := r.Dispatch(context.Background(), []string{"nope"}); err == nil {
		t.Fatal("unknown command must error")
	}
}

func TestDispatchMissingRequiredParam(t *testing.T) {
	r := build(t, contracts.New("set", "home").Param("channel", "", true).
		Do(func(context.Context, contracts.Input) (string, error) { return "", nil }))
	if _, err := r.Dispatch(context.Background(), []string{"set", "home"}); err == nil {
		t.Fatal("missing required param must error")
	}
}

func TestDispatchBoolFlagAndRest(t *testing.T) {
	var in contracts.Input
	r := build(t, contracts.New("session", "create").
		Param("name", "", true).Param("shared", "", false).
		Do(func(_ context.Context, got contracts.Input) (string, error) { in = got; return "", nil }))
	_, err := r.Dispatch(context.Background(), []string{"session", "create", "--name", "x", "--shared", "extra"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Get("name") != "x" || !in.Bool("shared") {
		t.Fatalf("args = %+v", in.Args)
	}
	if len(in.Rest) != 1 || in.Rest[0] != "extra" {
		t.Fatalf("rest = %v", in.Rest)
	}
}

func TestDispatchReturnsHandlerOutput(t *testing.T) {
	r := build(t, leaf("session", "list"))
	out, err := r.Dispatch(context.Background(), []string{"session", "list"})
	if err != nil || out != "ran list" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && rtk proxy go test ./core/cli/`
Expected: FAIL — `r.Dispatch undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `/home/shan/dev/herrscher/core/cli/registry.go` (and add `"context"` to imports):

```go
// Dispatch resolves args to the command whose Path is the longest prefix of
// args, parses the remainder into an Input (--flag value pairs into Args; a
// flag declared as an optional param with no following value is treated as the
// bool "true"; anything left over goes to Rest), checks required params, and
// runs it. It returns the handler's output string.
func (r *Registry) Dispatch(ctx context.Context, args []string) (string, error) {
	cmd, rest := r.match(args)
	if cmd == nil {
		return "", fmt.Errorf("unknown command %q", strings.Join(args, " "))
	}
	in, err := parse(*cmd, rest)
	if err != nil {
		return "", err
	}
	return cmd.Run(ctx, in)
}

// match finds the command whose Path is the longest prefix of args.
func (r *Registry) match(args []string) (*contracts.Cmd, []string) {
	var best *contracts.Cmd
	bestLen := 0
	for i := range r.cmds {
		c := &r.cmds[i]
		if len(c.Path) > len(args) || len(c.Path) <= bestLen {
			continue
		}
		if hasPrefix(args, c.Path) {
			best = c
			bestLen = len(c.Path)
		}
	}
	if best == nil {
		return nil, nil
	}
	return best, args[bestLen:]
}

func hasPrefix(args, path []string) bool {
	for i, p := range path {
		if args[i] != p {
			return false
		}
	}
	return true
}

func isParam(c contracts.Cmd, name string) (contracts.Param, bool) {
	for _, p := range c.Params {
		if p.Name == name {
			return p, true
		}
	}
	return contracts.Param{}, false
}

func parse(c contracts.Cmd, rest []string) (contracts.Input, error) {
	in := contracts.Input{Args: map[string]string{}}
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if !strings.HasPrefix(tok, "--") {
			in.Rest = append(in.Rest, tok)
			continue
		}
		name := strings.TrimPrefix(tok, "--")
		p, ok := isParam(c, name)
		if !ok {
			return in, fmt.Errorf("%s: unknown flag --%s", strings.Join(c.Path, " "), name)
		}
		// A value follows unless the next token is itself a flag; a valueless
		// optional flag is a bool set to "true".
		if i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "--") {
			in.Args[name] = rest[i+1]
			i++
		} else if !p.Required {
			in.Args[name] = "true"
		} else {
			return in, fmt.Errorf("%s: flag --%s needs a value", strings.Join(c.Path, " "), name)
		}
	}
	for _, p := range c.Params {
		if p.Required {
			if _, ok := in.Args[p.Name]; !ok {
				return in, fmt.Errorf("%s: missing required --%s", strings.Join(c.Path, " "), p.Name)
			}
		}
	}
	return in, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && rtk proxy go test ./core/cli/`
Expected: PASS.

> NOTE: `TestDispatchBoolFlagAndRest` passes `--shared extra`. Since `extra` is not `--`-prefixed it is consumed as `shared`'s value ("extra") — making `Bool("shared")` false. To keep the test meaningful, change the dispatch args in that test to `{"session","create","--name","x","--shared","--","extra"}` is overkill; instead assert the documented behavior: replace the bool test body to use `--shared` as the LAST token. Use this corrected test instead:

```go
func TestDispatchBoolFlagAndRest(t *testing.T) {
	var in contracts.Input
	r := build(t, contracts.New("session", "create").
		Param("name", "", true).Param("shared", "", false).
		Do(func(_ context.Context, got contracts.Input) (string, error) { in = got; return "", nil }))
	_, err := r.Dispatch(context.Background(), []string{"session", "create", "extra", "--name", "x", "--shared"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Get("name") != "x" || !in.Bool("shared") {
		t.Fatalf("args = %+v", in.Args)
	}
	if len(in.Rest) != 1 || in.Rest[0] != "extra" {
		t.Fatalf("rest = %v", in.Rest)
	}
}
```

- [ ] **Step 5: Add the `Help` method**

Append to `registry.go`:

```go
// Help renders one usage line per command, sorted by path, for the root help.
func (r *Registry) Help() string {
	lines := make([]string, 0, len(r.cmds))
	for _, c := range r.cmds {
		line := "  " + strings.Join(c.Path, " ")
		for _, p := range c.Params {
			if p.Required {
				line += " --" + p.Name + " <" + p.Name + ">"
			} else {
				line += " [--" + p.Name + "]"
			}
		}
		if c.Help != "" {
			line += "  — " + c.Help
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
```

Add `"sort"` to the imports. Run `cd /home/shan/dev/herrscher && rtk proxy go test ./core/cli/` — expected PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/cli/registry.go core/cli/registry_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(core/cli): argv dispatch (longest-path match, param parse) + help"
```

**End of Phase 1 — repo is green. Phase 2 deliberately breaks the build until Task 10.**

---

### Task 4: Rewrite the manager handler shape (handler.go + helpers)

**Files:**
- Modify: `/home/shan/dev/herrscher/core/internal/manager/handler.go`

The manager handlers currently return `contracts.CommandResponse` and take `contracts.Command`. The new handler signature is `func(ctx context.Context, in contracts.Input) (string, error)`. `Private` is dropped (CLI output is just stdout). `errf`/`deny` go away — handlers return an `error` for failure, a `string` for success.

- [ ] **Step 1: Replace handler.go**

Replace the whole file with:

```go
package manager

import (
	"github.com/Herrscherd/herrscher/core/internal/state"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Handler holds the dependencies the session/set/service/workspace commands act
// on. Commands(h) (commands.go) turns its methods into declared contracts.Cmd.
type Handler struct {
	d          discord
	sup        supervisor
	wt         worktrees
	fg         forges
	up         updater
	st         *state.State
	defaultCmd string
	partDir    string
	cmdPresets []contracts.Choice
}

func NewHandler(d discord, sup supervisor, wt worktrees, fg forges, up updater, st *state.State, defaultCmd string, partDir string, cmdPresets []contracts.Choice) *Handler {
	return &Handler{d: d, sup: sup, wt: wt, fg: fg, up: up, st: st, defaultCmd: defaultCmd, partDir: partDir, cmdPresets: cmdPresets}
}

// PartDir returns the participants journal directory (used by tests/wiring).
func (h *Handler) PartDir() string { return h.partDir }
```

> The authorization that `Handle` did (`h.st.Allowed(in.Invoker)`) is a Discord concern (it gates who may invoke from a channel). The CLI runs as the host operator, so it is unconditionally allowed; per-invoker gating returns with the gateway in the dctl phase. Drop it here.

- [ ] **Step 2: Commit (build still red — expected)**

```bash
cd /home/shan/dev/herrscher
git add core/internal/manager/handler.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "refactor(manager): strip slash Handle/Slow/errf; new handler deps only"
```

---

### Task 5: Rewrite `set.go` handlers onto `(Input) (string, error)`

**Files:**
- Modify: `/home/shan/dev/herrscher/core/internal/manager/set.go`

Mechanical transform applied to every handler in this and the next tasks:
1. Signature `(h *Handler) handleX(ctx, in contracts.Command) contracts.CommandResponse` → `(h *Handler) xRun(ctx context.Context, in contracts.Input) (string, error)` — one method per leaf command (no inner `switch`; the registry routes by path).
2. `in.Data.Opt("k")` → `in.Lookup("k")`; `in.Data.OptBool("k")` → `in.Bool("k")`.
3. `return errf("...")` → `return "", fmt.Errorf("...")`.
4. `return contracts.CommandResponse{Content: msg}` → `return msg, nil`.

- [ ] **Step 1: Replace set.go**

```go
package manager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *Handler) setHomeRun(ctx context.Context, in contracts.Input) (string, error) {
	id, ok := in.Lookup("channel")
	if !ok {
		return "", fmt.Errorf("missing channel")
	}
	kind, err := h.d.Kind(ctx, id)
	if err != nil {
		return "", fmt.Errorf("cannot read channel: %v", err)
	}
	var typ string
	switch kind {
	case "category":
		typ = "category"
	case "forum":
		typ = "forum"
	default:
		return "", fmt.Errorf("home must be a category or a forum")
	}
	if err := h.st.SetHome(state.HomeRef{ID: id, Type: typ}); err != nil {
		return "", fmt.Errorf("save failed: %v", err)
	}
	return fmt.Sprintf("🏠 Home set to %s `%s`.", typ, id), nil
}

func (h *Handler) setWorkspaceRun(_ context.Context, in contracts.Input) (string, error) {
	p, ok := in.Lookup("path")
	if !ok || p == "" {
		return "", fmt.Errorf("missing path")
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("bad path: %v", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	if err := h.st.SetWorkspace(abs); err != nil {
		return "", fmt.Errorf("save failed: %v", err)
	}
	return fmt.Sprintf("📂 Workspace set to `%s`.", abs), nil
}

func (h *Handler) setSourceRun(_ context.Context, in contracts.Input) (string, error) {
	p, ok := in.Lookup("path")
	if !ok || p == "" {
		return "", fmt.Errorf("missing path")
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("bad path: %v", err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	if !isHerrscherCheckout(abs) {
		return "", fmt.Errorf("not a herrscher source checkout: %s", abs)
	}
	if err := h.st.SetSource(abs); err != nil {
		return "", fmt.Errorf("save failed: %v", err)
	}
	return fmt.Sprintf("🛠️ Source set to `%s`.", abs), nil
}

// isHerrscherCheckout reports whether dir holds the herrscher module's go.mod.
func isHerrscherCheckout(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "module github.com/Herrscherd/herrscher" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Commit (still red)**

```bash
cd /home/shan/dev/herrscher
git add core/internal/manager/set.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "refactor(manager): set home/workspace/source onto Input handlers"
```

---

### Task 6: Rewrite `session.go` handlers

**Files:**
- Modify: `/home/shan/dev/herrscher/core/internal/manager/session.go`

Apply the same transform. The `handleSession`/`sessionAllow`-group routing and `allowAction` go away: each becomes its own leaf command (`session create`, `session close`, `session list`, `session who`, `session allow add|remove|list`). Keep `sessionBanner` and the timeouts unchanged.

- [ ] **Step 1: Rewrite the handlers**

Replace every handler signature and body per the recipe. Concretely:

- `handleSession` + `allowAction` — **delete** (routing now lives in the registry).
- `sessionCreate(ctx, in contracts.Command) contracts.CommandResponse` → `sessionCreateRun(ctx context.Context, in contracts.Input) (string, error)`. Body identical except: `in.Data.Opt("name")`→`in.Lookup("name")`, `in.Data.Opt("cmd")`→`in.Lookup("cmd")`, `in.Data.Opt("backend")`→`in.Lookup("backend")`, `in.Data.Opt("clone")`→`in.Lookup("clone")`, `in.Data.Opt("project")`→`in.Lookup("project")`, `in.Data.OptBool("shared")`→`in.Bool("shared")`; every `return errf(f, a...)`→`return "", fmt.Errorf(f, a...)`; the final `return contracts.CommandResponse{Content: reply, Private: true}`→`return reply, nil`.
- `sessionClose` → `sessionCloseRun(...)`: `in.Data.Opt("name")`→`in.Lookup("name")`, `in.Data.OptBool("force")`→`in.Bool("force")`, errors and the final response transformed as above (`return fmt.Sprintf("🗄️ Session **%s** closed.", name), nil`).
- `sessionList()` → `sessionListRun(_ context.Context, _ contracts.Input) (string, error)`: `return "No active sessions.", nil` / `return out, nil`.
- `sessionWho` → `sessionWhoRun(...)`: `in.Lookup("name")`, returns `(string, error)`.
- `sessionAllow` splits into three leaf handlers driven by `in`:

```go
func (h *Handler) sessionAllowAddRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	if _, exists := h.st.FindSession(name); !exists {
		return "", fmt.Errorf("no session %q", name)
	}
	id, ok := in.Lookup("user")
	if !ok {
		return "", fmt.Errorf("missing user")
	}
	id = normalizeUserID(id)
	added, err := h.st.AddSessionAllow(name, id)
	if err != nil {
		return "", fmt.Errorf("%v", err)
	}
	if !added {
		return fmt.Sprintf("<@%s> already allowed on **%s**.", id, name), nil
	}
	return fmt.Sprintf("✅ <@%s> allowed on **%s**.", id, name), nil
}

func (h *Handler) sessionAllowRemoveRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	if _, exists := h.st.FindSession(name); !exists {
		return "", fmt.Errorf("no session %q", name)
	}
	id, ok := in.Lookup("user")
	if !ok {
		return "", fmt.Errorf("missing user")
	}
	id = normalizeUserID(id)
	removed, err := h.st.RemoveSessionAllow(name, id)
	if err != nil {
		return "", fmt.Errorf("%v", err)
	}
	if !removed {
		return fmt.Sprintf("<@%s> was not in **%s**'s allowlist.", id, name), nil
	}
	return fmt.Sprintf("✅ <@%s> removed from **%s**.", id, name), nil
}

func (h *Handler) sessionAllowListRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	if _, exists := h.st.FindSession(name); !exists {
		return "", fmt.Errorf("no session %q", name)
	}
	ids := h.st.SessionAllowlist(name)
	if len(ids) == 0 {
		return fmt.Sprintf("**%s** has no per-session allowlist (the global allowlist still applies).", name), nil
	}
	out := fmt.Sprintf("Per-session allowlist for **%s** (plus the global allowlist):\n", name)
	for _, id := range ids {
		out += fmt.Sprintf("• <@%s>\n", id)
	}
	return out, nil
}
```

Remove the now-unused `contracts.OptSubcommand`/`OptSubcommandGroup` references (they were only in `allowAction`).

- [ ] **Step 2: Commit (still red)**

```bash
cd /home/shan/dev/herrscher
git add core/internal/manager/session.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "refactor(manager): session create/close/list/who/allow onto Input handlers"
```

---

### Task 7: Rewrite `service.go` + `workspace.go`; delete `autocomplete.go`

**Files:**
- Modify: `/home/shan/dev/herrscher/core/internal/manager/service.go`
- Modify: `/home/shan/dev/herrscher/core/internal/manager/workspace.go`
- Delete: `/home/shan/dev/herrscher/core/internal/manager/autocomplete.go`

- [ ] **Step 1: Transform service.go and workspace.go**

Apply the identical recipe (Task 5 steps 1–4) to every `handle*`/sub-handler in `service.go` (service restart/update/status etc.) and `workspace.go` (workspace list/remotes), renaming each leaf to `<verb>Run(ctx context.Context, in contracts.Input) (string, error)`, converting `in.Data.Opt*`→`in.Lookup`/`in.Bool`, `errf`→`fmt.Errorf`, and `CommandResponse{Content: x}`→`x, nil`. Read each file first; preserve all non-slash logic (state calls, exec, forge calls) verbatim.

- [ ] **Step 2: Delete autocomplete.go**

`autocomplete.go` implements slash `Suggest` (the `cmdPresets` → `[]contracts.Choice` completion). The CLI has no autocomplete; remove the file. Keep the `cmdPresets` field on `Handler` only if `sessionCreateRun` still reads it for validation — it does not (it only used it for suggestions), so leave the field unused for now (a later task may drop it) OR remove `cmdPresets` from `Handler` and `NewHandler` if nothing references it after this task. Grep first:

Run: `cd /home/shan/dev/herrscher && rtk proxy grep -rn "cmdPresets" core/`
If the only references are the field + constructor param, delete them from `handler.go` and update every `NewHandler(` call site found by `rtk proxy grep -rn "NewHandler(" core/ *.go`.

- [ ] **Step 3: Commit (still red)**

```bash
cd /home/shan/dev/herrscher
git add core/internal/manager/service.go core/internal/manager/workspace.go
git rm core/internal/manager/autocomplete.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "refactor(manager): service/workspace onto Input handlers; drop slash autocomplete"
```

---

### Task 8: Declare the manager commands; delete legacy slash types from contracts

**Files:**
- Create: `/home/shan/dev/herrscher/core/internal/manager/commands.go`
- Modify: `/home/shan/dev/herrscher-contracts/command.go`
- Modify: `/home/shan/dev/herrscher-contracts/host.go`

- [ ] **Step 1: Write commands.go**

```go
package manager

import contracts "github.com/Herrscherd/herrscher-contracts"

// Commands declares every manager command as a neutral contracts.Cmd bound to a
// handler method. This is the single declaration the CLI dispatches today and a
// gateway will bind slash names to later.
func Commands(h *Handler) []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New("set", "home").Help("set the session home (category/forum)").
			Param("channel", "channel id", true).Do(h.setHomeRun),
		contracts.New("set", "workspace").Help("set the workspace root").
			Param("path", "directory", true).Do(h.setWorkspaceRun),
		contracts.New("set", "source").Help("set the herrscher source checkout").
			Param("path", "directory", true).Do(h.setSourceRun),

		contracts.New("session", "create").Help("create a session").
			Param("name", "session name", true).
			Param("cmd", "bridge command", false).
			Param("backend", "stream|oneshot", false).
			Param("project", "workspace project", false).
			Param("clone", "repo to clone", false).
			Param("shared", "use main checkout", false).Do(h.sessionCreateRun),
		contracts.New("session", "close").Help("close a session").
			Param("name", "session name", true).
			Param("force", "discard uncommitted worktree", false).Do(h.sessionCloseRun),
		contracts.New("session", "list").Help("list active sessions").Do(h.sessionListRun),
		contracts.New("session", "who").Help("list observed participants").
			Param("name", "session name", true).Do(h.sessionWhoRun),
		contracts.New("session", "allow", "add").Help("allow a user on a session").
			Param("name", "session name", true).Param("user", "user id", true).Do(h.sessionAllowAddRun),
		contracts.New("session", "allow", "remove").Help("remove a user from a session").
			Param("name", "session name", true).Param("user", "user id", true).Do(h.sessionAllowRemoveRun),
		contracts.New("session", "allow", "list").Help("list a session's allowlist").
			Param("name", "session name", true).Do(h.sessionAllowListRun),
	}
}
```

> Add `service`/`workspace`/`allow` (global) entries here too, matching the leaf method names produced in Task 7 and the global-`allow` handler. Read those files to copy the exact param names from the old slash option reads.

- [ ] **Step 2: Delete the legacy slash types**

In `/home/shan/dev/herrscher-contracts/command.go` delete: `Command`, `CommandData`, `Option`, `OptionType`, the `Opt*`/`find*`/`Subcommand`/`Focused` helpers, `CommandResponse`, `CommandKind`/`Kind*`, `Responder`, `InboundCommand`. Keep `Choice` if it is defined here (used by `MenuRouter`/`RouteMenu` and by `Handler.cmdPresets`); if `Choice` lives in another file, leave it. After editing, the file may become empty — if so, `git rm` it.

In `/home/shan/dev/herrscher-contracts/host.go` delete the `CommandRegistrar` interface.

- [ ] **Step 3: Verify contracts compiles**

Run: `cd /home/shan/dev/herrscher-contracts && rtk proxy go build ./... && rtk proxy go test ./...`
Expected: PASS — contracts no longer references the slash types. If `gateway_test.go`/`manifest_test.go` referenced any deleted type, fix those references (they should not — they cover gateway/manifest, not commands).

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add -A
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(contracts)!: delete legacy slash command types (Cmd API replaces them)"
cd /home/shan/dev/herrscher
git add core/internal/manager/commands.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(manager): declare commands as neutral contracts.Cmd"
```

---

### Task 9: Strip slash dispatch from serve.go; trim the gateway plugin

**Files:**
- Modify: `/home/shan/dev/herrscher/core/host/serve.go`
- Modify: `/home/shan/dev/herrscher-discord-gateway/source.go`
- Modify: `/home/shan/dev/herrscher-discord-gateway/adapters.go`

- [ ] **Step 1: Strip the command loop from serve.go**

Read `core/host/serve.go`. Remove `dispatchCommand`, `handleChoicePick`, and the `switch in.Kind { ... }` consumer of `Source.Commands()` plus any `Responder`/`InboundCommand`/`CommandRegistrar.Register` use. Keep: state load, supervisor, health endpoint, the reconnect loop's non-command parts, and the status loop. If removing the command loop leaves the gateway `Source` unused in serve, keep the connection/reconnect (health/liveness) but drop the command consumption. The `manager.Handler` is no longer constructed in serve (it is now reached via the CLI registry); remove its construction here if it becomes unused.

> This is the one non-mechanical edit. Make the minimal deletion that compiles: anything that named a deleted contracts type must go. Verify with `rtk proxy grep -rn "InboundCommand\|Responder\|CommandRegistrar\|dispatchCommand\|handleChoicePick" core/host/`.

- [ ] **Step 2: Trim the gateway plugin**

In the gateway repo, `source.go`/`adapters.go` produce `InboundCommand`/implement `Responder`/`CommandRegistrar`. Delete the command-source side: the `Commands()` channel, the `Responder` implementation, and `CommandRegistrar.Register`. Keep the message read/reply/react/admin ports (`ChannelReader`/`ChannelAdmin`/`Gateway`/`MenuRouter`) the bridge needs. Update `GatewaySet` construction to drop the `Source`/`Registrar` fields (they will be re-added in the dctl phase).

> If `contracts.GatewaySet` still declares `Source ChannelSource`/`Registrar CommandRegistrar` fields and `ChannelSource` references nothing deleted, you may leave `ChannelSource` defined but set those fields to nil in the gateway. Simplest compiling path wins; the dctl phase rebuilds this surface.

- [ ] **Step 3: Verify both build**

Run:
```bash
cd /home/shan/dev/herrscher-discord-gateway && rtk proxy go build ./...
cd /home/shan/dev/herrscher && rtk proxy go build ./core/...
```
Expected: PASS for `core/...` and the gateway. `main.go` will still fail to build (Task 10).

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher-discord-gateway
git add -A
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "refactor(gateway): drop slash command source/responder (rebinds at dctl phase)"
cd /home/shan/dev/herrscher
git add core/host/serve.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "refactor(host): strip slash dispatch loop from serve"
```

---

### Task 10: Wire the CLI registry in main.go (restore green)

**Files:**
- Modify: `/home/shan/dev/herrscher/main.go`
- Modify: `/home/shan/dev/herrscher/core/internal/manager/handler_test.go`

- [ ] **Step 1: Fix the manager tests**

`handler_test.go` drives handlers via `h.Handle(ctx, it(...))` and asserts `CommandResponse`. Rewrite each test to call the leaf method directly with a `contracts.Input` and assert `(string, error)`. Example transform for one test:

```go
// before: r := h.Handle(ctx, it("owner", "set", "source", contracts.Option{Name:"path", Value: dir})); assert r.Content
// after:
out, err := h.setSourceRun(context.Background(), contracts.Input{Args: map[string]string{"path": dir}})
if err == nil { t.Fatalf("expected rejection, got %q", out) }
```

Delete the `it(...)`/`contracts.Option` test helper and any assertion on `.Private`. For success cases assert `err == nil` and `strings.Contains(out, ...)`.

- [ ] **Step 2: Rewrite main.go dispatch**

Replace the hand `switch` for the manager-backed verbs with the registry. Keep the channel verbs (`send`/`reply`/`read`/`watch`/`react`/`thread`/`channel`) and daemon verbs (`serve`/`bridge`/`service`) as they are for now **only if** they don't reference deleted types; the manager-backed commands (`set`/`session`) now come from the registry. Concretely, build the registry from `manager.Commands(h)` plus `manage` verbs:

```go
reg := &cli.Registry{}
for _, c := range manager.Commands(h) {
	if err := reg.Add(c); err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
		os.Exit(1)
	}
}
// dispatch: if reg has the command, run it; else fall through to legacy verbs
if out, err := reg.Dispatch(ctx, os.Args[1:]); err == nil {
	if out != "" {
		fmt.Println(out)
	}
	return
}
```

> Constructing `h *manager.Handler` in `main.go` needs the same deps `serve` built (state, supervisor, worktrees, forges, updater). Read how `runServe` builds them and factor a small constructor reused by both. If that is too large for this task, scope Task 10 to registering only the `set`/`session` commands that need just `state` + `discord` + `supervisor`, and leave `service` in the legacy switch; note the gap in the commit message.

- [ ] **Step 3: Verify the whole family builds and tests pass**

Run:
```bash
cd /home/shan/dev/herrscher && rtk proxy go build ./... && rtk proxy go test ./...
cd /home/shan/dev/herrscher-contracts && rtk proxy go test ./...
cd /home/shan/dev/herrscher-discord-gateway && rtk proxy go build ./... && rtk proxy go test ./...
```
Expected: all PASS.

- [ ] **Step 4: Smoke-test the CLI**

Run: `cd /home/shan/dev/herrscher && rtk proxy go run . session list`
Expected: prints `No active sessions.` (or the current sessions) — proves the registry path end to end.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
git add main.go core/internal/manager/handler_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(cli): dispatch manager commands through the registry; restore green"
```

---

### Task 11: Update README + recap

**Files:**
- Modify: `/home/shan/dev/herrscher/README.md`

- [ ] **Step 1: Update the README**

The "two run modes" / slash-command mentions are now stale for the command surface. Add a short note under CLI reference that session/set/etc. are CLI commands today and Discord slash is being re-platformed (dctl phase). Do not over-edit; keep it factual.

- [ ] **Step 2: Commit + open PR**

```bash
cd /home/shan/dev/herrscher
git add README.md
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "docs: CLI command surface; Discord slash deferred to dctl phase"
git push -u origin feat/unified-command-api
gh pr create --title "Unified command API + CLI" --body "Replaces the Discord slash abstraction with a neutral contracts.Cmd API dispatched natively by the CLI. Legacy slash types deleted; Discord slash dark until the dctl rebind phase. Spec: docs/superpowers/specs/2026-06-16-unified-command-api-cli-design.md"
```

---

## Self-review notes

- **Spec coverage:** Cmd/builder (T1), core/cli registry+dispatch (T2–3), delete legacy slash types incl. CommandRegistrar (T8), rewrite manager handlers (T5–7), strip serve dispatch (T9), host builds registry + CLI (T10), bridge kept working / MenuRouter+Choice retained (T8 keeps Choice, T9 keeps reader/admin/menu). Covered.
- **Known judgment calls flagged inline:** the `cmdPresets` field fate (T7), the simplest-compiling gateway trim (T9), and the `manager.Handler` construction sharing between serve and main (T10) are left to the implementer with explicit fallback scoping — these depend on file contents not fully shown here and must be read at execution time.
- **Type consistency:** handler leaves are named `<verb>Run` returning `(string, error)`; `Commands()` binds those exact names; `Registry.Dispatch` returns `(string, error)`; `Input` uses `Lookup/Get/Bool/Rest` consistently across tasks.
