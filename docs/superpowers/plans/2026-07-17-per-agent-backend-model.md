# Per-Agent Backend + Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let each durable agent pin its own backend vendor + model so a multi-agent run can mix backends (e.g. orchestrator on Fable, builders on gpt-5.6).

**Architecture:** Two independent pieces. (1) Make the persistent live bridge select its backend by the session's `Vendor` (today it hardcodes Claude), reusing the same registry selection the one-shot seed path already uses. (2) Let a durable agent store a default `cmd` (the invocation string that carries the model), inherited into every session created from it — exactly like the existing `backend`/vendor inheritance.

**Tech Stack:** Go, herrscher plugin registry (`contracts.Default.Backends()`), out-of-process bridge processes supervised by `core/internal/supervisor`.

## Global Constraints

- Go module; verify gate is `GOWORK=off gofmt -l`, `GOWORK=off go build ./...`, `GOWORK=off go vet ./...`, `GOWORK=off go test ./...`. Set `GOCACHE`/`GOMODCACHE`/`GOTMPDIR` under `/tmp` if the default cache is read-only.
- **No new dependencies.** No `go get`, no import of any external backend module beyond what is already imported. Every change is inside `core/` and root `bridge.go`.
- **No changes to any external backend plugin** (codex/cursor/claude modules). The model is expressed through the existing `cmd` string; no backend learns a new setting key.
- `--vendor` is distinct from the existing `--backend` flag on `bridge.go`: `--backend` is the *kind* (`stream`|`oneshot`); `--vendor` is the plugin vendor (`claude`|`codex`|`cursor`).
- Follow existing file patterns: mirror `readBackend`/`backendFile`/`Backend` field naming when adding the `cmd` equivalents.

---

### Task 1: Shared backend factory (`BuildBackend`) + live bridge honors vendor

Unify backend construction so the live bridge and the one-shot seed both select the plugin by vendor. Today `bridge.go`'s `newBackend` hardcodes `claude.NewBackend` (`bridge.go:56`) while `newSeedBackend` (`core/host/seed.go:117`) does the correct registry selection. Extract the seed logic into a reusable helper and call it from both.

**Files:**
- Modify: `core/host/seed.go` — extract `BuildBackend`, reimplement `newSeedBackend` on top of it.
- Modify: `bridge.go` — add `--vendor` flag; replace hardcoded claude fallback with `host.BuildBackend`.
- Modify: `core/internal/supervisor/supervisor.go:34-60` — thread `--vendor sess.Vendor`.
- Test: `core/host/seed_test.go` (extend/create), `core/internal/supervisor/supervisor_test.go` (extend).

**Interfaces:**
- Produces: `func BuildBackend(ctx context.Context, vendor, cmd, kind, dir string) (contracts.Backend, error)` in package `host`. Selects a remote resolver backend first, else `selectBackend(vendor, contracts.Default.Backends())`, resolves the plugin config, applies `cmd`/`kind`/`dir` settings, calls `plugin.Backend(ctx, cfg)`. `vendor==""` falls back to `HERRSCHER_BACKEND` then the first registered backend plugin.

- [ ] **Step 1: Write the failing test for `BuildBackend` vendor routing**

Add to `core/host/seed_test.go` (create the file if absent; use the existing test package `host`). Register two fake backend plugins and assert selection routes by vendor. If the repo already has a fake-plugin helper for `selectBackend`, reuse it; otherwise:

```go
func TestBuildBackendSelectsByVendor(t *testing.T) {
	// Save/restore the global registry so the test is hermetic.
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = &contracts.Registry{}

	var built string
	mk := func(kind string) contracts.Plugin {
		return contracts.Plugin{
			Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryBackend},
			Backend: func(ctx context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
				built = kind
				return stubBackend{}, nil
			},
		}
	}
	contracts.Default.Register(mk("claude"))
	contracts.Default.Register(mk("codex"))

	if _, err := host.BuildBackend(context.Background(), "codex", "codex --model gpt-5.6", "", ""); err != nil {
		t.Fatalf("BuildBackend: %v", err)
	}
	if built != "codex" {
		t.Fatalf("built %q, want codex", built)
	}
}
```

> NOTE for implementer: adapt `contracts.Registry`/`Register`/`Manifest` field names and the `stubBackend` to the ACTUAL contracts API in this repo (grep `contracts.Default` and the existing `selectBackend` callers/tests to copy the exact shapes). The behavioral assertion — "vendor `codex` builds the codex plugin" — is what must hold. Reuse an existing fake backend from the test suite if one exists.

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOWORK=off go test ./core/host/ -run TestBuildBackendSelectsByVendor -v`
Expected: FAIL — `host.BuildBackend` undefined.

- [ ] **Step 3: Extract `BuildBackend` and reimplement `newSeedBackend`**

In `core/host/seed.go`, add the helper and make `newSeedBackend` delegate to it. Preserve the current seed precedence (`sess.Vendor` → `HERRSCHER_BACKEND`) and the settings it applies (`cmd`, `kind`, `dir`):

```go
// BuildBackend selects and constructs a backend by vendor, the single path
// shared by the one-shot seed and the live bridge so they cannot drift. A
// remote resolver backend wins when configured; otherwise the registered
// plugin whose Manifest.Kind == vendor is built with cmd/kind/dir applied.
// vendor=="" falls back to HERRSCHER_BACKEND, then the first backend plugin.
func BuildBackend(ctx context.Context, vendor, cmd, kind, dir string) (contracts.Backend, error) {
	if vendor == "" {
		vendor = os.Getenv("HERRSCHER_BACKEND")
	}
	plugins := contracts.Default.Backends()
	resolver := NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
	if backend, err := resolver.Backend(ctx, plugins, vendor); err != nil {
		return nil, err
	} else if backend != nil {
		return backend, nil
	}
	plugin, err := selectBackend(vendor, plugins)
	if err != nil {
		return nil, err
	}
	cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
	if err != nil {
		return nil, err
	}
	if cmd != "" {
		cfg.Settings["cmd"] = cmd
	}
	if kind != "" {
		cfg.Settings["kind"] = kind
	}
	if dir != "" {
		cfg.Settings["dir"] = dir
	}
	return plugin.Backend(ctx, cfg)
}

func newSeedBackend(ctx context.Context, sess state.Session) (contracts.Backend, error) {
	return BuildBackend(ctx, sess.Vendor, sess.Cmd, sess.Backend, sess.Worktree)
}
```

Guard `cfg.Settings` against nil if `contracts.Resolve` can return a nil map (match how the old `newSeedBackend` handled it — it wrote directly, so assume non-nil; if a nil-map panic appears, initialize `cfg.Settings` before the writes).

- [ ] **Step 4: Run the test to verify it passes**

Run: `GOWORK=off go test ./core/host/ -run TestBuildBackendSelectsByVendor -v`
Expected: PASS.

- [ ] **Step 5: Add `--vendor` to the bridge and use `BuildBackend`**

In `bridge.go`, add the flag next to the others (after line 31):

```go
	vendor := fs.String("vendor", "", "backend vendor: claude | codex | cursor (empty = first registered / HERRSCHER_BACKEND)")
```

Replace the `newBackend` closure body (`bridge.go:50-63`) so it selects by vendor instead of hardcoding claude:

```go
	newBackend := func(channelID string) (contracts.Backend, error) {
		if be, err := br.Backend(ctx, contracts.Default.Backends()); err != nil {
			return nil, err
		} else if be != nil {
			return be, nil
		}
		return host.BuildBackend(ctx, *vendor, *cmdStr, *backend, "")
	}
```

The direct `claude` import in `bridge.go:9` is now unused — remove it (and the `claude` alias) so `go vet`/build stay clean. `host` is already imported (`bridge.go:12`).

> NOTE: `*model` (the existing `--model` flag) stays declared for backward compat but is no longer consulted here — the model rides inside `*cmdStr`. Leave the flag; do not delete it (avoids breaking any caller passing `--model`). If `go vet` flags it as unused, it is a declared flag var (used by `flag`), not a dead local — it will not be flagged.

- [ ] **Step 6: Thread `--vendor` from the supervisor**

In `core/internal/supervisor/supervisor.go`, inside `bridgeArgs`, after the existing `--backend` block (around line 47):

```go
	if sess.Vendor != "" {
		args = append(args, "--vendor", sess.Vendor)
	}
```

- [ ] **Step 7: Write the supervisor arg test**

Add to `core/internal/supervisor/supervisor_test.go` (mirror the existing `bridgeArgs` assertions):

```go
func TestBridgeArgsThreadsVendor(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	args := s.bridgeArgs(state.Session{Name: "w", ChannelID: "c", Vendor: "codex"})
	if !argsContainPair(args, "--vendor", "codex") {
		t.Fatalf("args missing --vendor codex: %v", args)
	}
	// empty vendor omits the flag
	args2 := s.bridgeArgs(state.Session{Name: "w", ChannelID: "c"})
	for _, a := range args2 {
		if a == "--vendor" {
			t.Fatalf("--vendor present for empty vendor: %v", args2)
		}
	}
}
```

> NOTE: reuse the repo's existing arg-assertion helper if one exists (grep `bridgeArgs` in `supervisor_test.go`); otherwise add a small `argsContainPair(args []string, k, v string) bool` local.

- [ ] **Step 8: Run the full gate for touched packages**

Run:
```
GOWORK=off go test ./core/host/ ./core/internal/supervisor/ -v
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off gofmt -l bridge.go core/host/seed.go core/internal/supervisor/supervisor.go
```
Expected: tests PASS; build/vet clean; `gofmt -l` prints nothing.

- [ ] **Step 9: Commit**

```bash
git add bridge.go core/host/seed.go core/host/seed_test.go core/internal/supervisor/supervisor.go core/internal/supervisor/supervisor_test.go
git commit -m "feat(host): live bridge selects backend by session vendor via shared BuildBackend"
```

---

### Task 2: Durable agent stores a default `cmd`, inherited on session create

Give an agent a `cmd` file in its home, populated by `agent create --cmd`, read into `Agent.Cmd`, and inherited into a new session with precedence: explicit `cmd:` param > agent `Cmd` > `defaultCmd`.

**Files:**
- Modify: `core/internal/agent/agent.go` — add `cmdFile` const + `Cmd` field.
- Modify: `core/internal/agent/store.go` — `readCmd`, `CreateSpec.Cmd`, write in `Create`, populate in `Get`/`List`.
- Modify: `core/internal/manager/agent.go:14-31` — read `cmd` param into `CreateSpec`.
- Modify: `core/internal/manager/commands.go:40-46` — declare the `cmd` param on `agent create`.
- Modify: `core/internal/manager/session.go:110-113,186-202` — inherit agent `Cmd`.
- Test: `core/internal/agent/store_test.go`, `core/internal/manager/agent_test.go`.

**Interfaces:**
- Consumes: `Agent.Backend` inheritance pattern at `session.go:195`.
- Produces: `Agent.Cmd string`; `CreateSpec.Cmd string`; `func readCmd(home string) string`.

- [ ] **Step 1: Write the failing store test**

Add to `core/internal/agent/store_test.go`:

```go
func TestCreateStoresAndReadsCmd(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "b", Backend: "codex", Cmd: "codex --model gpt-5.6"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, ok := s.Get("b")
	if !ok {
		t.Fatal("agent not found")
	}
	if got.Cmd != "codex --model gpt-5.6" {
		t.Fatalf("Cmd = %q, want codex --model gpt-5.6", got.Cmd)
	}
	// absent cmd file yields empty
	if _, err := s.Create(CreateSpec{Name: "n"}); err != nil {
		t.Fatalf("create n: %v", err)
	}
	n, _ := s.Get("n")
	if n.Cmd != "" {
		t.Fatalf("Cmd = %q, want empty", n.Cmd)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `GOWORK=off go test ./core/internal/agent/ -run TestCreateStoresAndReadsCmd -v`
Expected: FAIL — `CreateSpec` has no `Cmd` field / `Agent` has no `Cmd`.

- [ ] **Step 3: Add the `cmd` field, const, reader**

`core/internal/agent/agent.go` — add const (after `backendFile` at line 24) and field (after `Backend` at line 38):

```go
	backendFile  = "backend"
	cmdFile      = "cmd"
```
```go
	Backend string   // backend vendor from <home>/backend, empty when absent
	Cmd     string   // default invocation from <home>/cmd, empty when absent
```

`core/internal/agent/store.go` — add reader (next to `readBackend` at line 112), field on `CreateSpec` (line 24-29), write in `Create` (extend the `spec.Backend != ""` block near line 174), and populate in `Get` (line 199) and `List` (line 218):

```go
func readCmd(home string) string {
	buf, err := os.ReadFile(filepath.Join(home, cmdFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(buf))
}
```
```go
type CreateSpec struct {
	Name    string
	Soul    string
	MCP     string
	Backend string
	Cmd     string
}
```
In `Create`, after the `spec.Backend` append block (line 174-179):
```go
	if spec.Cmd != "" {
		files = append(files, struct {
			name string
			data []byte
		}{cmdFile, []byte(spec.Cmd)})
	}
```
Return value at line 186 — add `Cmd: spec.Cmd`:
```go
	return Agent{Name: name, Home: home, Backend: spec.Backend, Cmd: spec.Cmd}, nil
```
`Get` (line 199) and `List` (line 218) — add `Cmd: readCmd(home)` to each `Agent{...}` literal.

- [ ] **Step 4: Run it to verify it passes**

Run: `GOWORK=off go test ./core/internal/agent/ -run TestCreateStoresAndReadsCmd -v`
Expected: PASS.

- [ ] **Step 5: Wire the `cmd` param through `agent create`**

`core/internal/manager/agent.go` — in `agentCreateRun`, read the param (after line 25) and pass it (line 26):

```go
	backend, _ := in.Lookup("backend")
	cmd, _ := in.Lookup("cmd")
	a, err := h.agents.Create(agent.CreateSpec{Name: name, Soul: soul, MCP: mcp, Backend: backend, Cmd: cmd})
```

`core/internal/manager/commands.go` — declare the param on `agent create` (after line 45):

```go
			Param("backend", "agent backend vendor: claude | codex | cursor", false).
			Param("cmd", "default invocation carrying the model, e.g. 'codex --model gpt-5.6'", false).
```

- [ ] **Step 6: Write the failing session-inheritance test**

Extend `core/internal/manager/agent_test.go`. The existing `TestSessionCreate...` (around line 86-92) already asserts vendor inheritance; add a sibling assertion path. Create an agent with a `cmd`, create a session from it with NO `cmd:` param, assert the session inherited it; then assert an explicit `cmd:` overrides:

```go
func TestSessionCreateInheritsAgentCmd(t *testing.T) {
	h, _, agents, wt, _, st := newTestHandler(t, "default-cmd")
	wt.path = t.TempDir()
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if _, err := agents.Create(agent.CreateSpec{Name: "bob", Soul: "P", Backend: "codex", Cmd: "codex --model gpt-5.6"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// inherits agent cmd when none given
	if _, err := h.sessionCreateRun(context.Background(), args("name", "s1", "agent", "bob")); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	s1, _ := st.FindSession("s1")
	if s1.Cmd != "codex --model gpt-5.6" {
		t.Fatalf("s1.Cmd = %q, want inherited agent cmd", s1.Cmd)
	}

	// explicit cmd wins
	if _, err := h.sessionCreateRun(context.Background(), args("name", "s2", "agent", "bob", "cmd", "claude")); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	s2, _ := st.FindSession("s2")
	if s2.Cmd != "claude" {
		t.Fatalf("s2.Cmd = %q, want explicit override", s2.Cmd)
	}
}
```

> NOTE: match `newTestHandler`'s ACTUAL signature/return tuple and the `args(...)` helper already used in this test file (copy from the existing `TestSessionCreate` test). The `agents` fake must be the real `*agent.Store` or a fake that persists `Cmd`; if the test uses a fake agent store, ensure it returns `Cmd`.

- [ ] **Step 7: Run it to verify it fails**

Run: `GOWORK=off go test ./core/internal/manager/ -run TestSessionCreateInheritsAgentCmd -v`
Expected: FAIL — session `Cmd` is `default-cmd`, not the inherited value (inheritance not wired yet).

- [ ] **Step 8: Wire cmd inheritance in session create**

`core/internal/manager/session.go` — track whether `cmd:` was explicit (replace lines 110-113):

```go
	cmd := h.defaultCmd
	cmdExplicit := false
	if c, ok := in.Lookup("cmd"); ok && c != "" {
		cmd = c
		cmdExplicit = true
	}
```

Then in the `agentName != ""` block, alongside the vendor inheritance (after line 197, inside the same block where `a` is resolved):

```go
		if vendor == "" {
			vendor = a.Backend
		}
		if !cmdExplicit && a.Cmd != "" {
			cmd = a.Cmd
		}
```

- [ ] **Step 9: Run it to verify it passes**

Run: `GOWORK=off go test ./core/internal/manager/ -run TestSessionCreateInheritsAgentCmd -v`
Expected: PASS.

- [ ] **Step 10: Run the full gate for touched packages**

Run:
```
GOWORK=off go test ./core/internal/agent/ ./core/internal/manager/ -v
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off gofmt -l core/internal/agent/agent.go core/internal/agent/store.go core/internal/manager/agent.go core/internal/manager/commands.go core/internal/manager/session.go
```
Expected: tests PASS; build/vet clean; `gofmt -l` prints nothing.

- [ ] **Step 11: Commit**

```bash
git add core/internal/agent/ core/internal/manager/
git commit -m "feat(agent): store per-agent default cmd, inherited on session create"
```

---

### Task 3: Full-suite verification

- [ ] **Step 1: Run the whole gate**

Run:
```
GOWORK=off gofmt -l . ; GOWORK=off go build ./... ; GOWORK=off go vet ./... ; GOWORK=off go test ./...
```
Expected: `gofmt -l` prints nothing; build/vet clean; all tests PASS. (Some socket-bound tests, e.g. NATS, may be skipped/blocked under a sandbox — note any that do not run so the main agent re-runs them outside the sandbox.)

- [ ] **Step 2: Manual end-to-end sanity (documented, not automated)**

Confirm the intended flow is now expressible (no code, just a check against the built binary help):
```
go run . agent create --help   # shows --cmd
go run . bridge --help         # shows --vendor
```
Expected: both flags present.

## Self-Review notes (author)

- **Spec coverage:** Piece 1 (live bridge honors vendor) → Task 1. Piece 2 (agent stores cmd, inherited) → Task 2. Data-flow scenario is exercised by Task 2 Step 6 (session inherits) + Task 1 Steps 6-7 (supervisor threads vendor). Testing section of spec → each task's TDD steps + Task 3.
- **cmd/vendor coherence** is an operator concern per the spec — no cross-validation task, intentional.
- **No external backend module touched** — confirmed: all edits under `core/` and `bridge.go`.
- **Type consistency:** `BuildBackend(ctx, vendor, cmd, kind, dir)` defined in Task 1 Step 3, consumed in Task 1 Step 5 with the same arg order. `Agent.Cmd`/`CreateSpec.Cmd`/`readCmd` defined in Task 2 Step 3, consumed in Steps 5/8.
