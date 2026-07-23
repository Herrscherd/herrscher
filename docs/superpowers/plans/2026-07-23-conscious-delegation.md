# Conscious cross-model Delegation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the running model conscious it can hand a full mission to another backend (Codex) by framing the already-built async `coordinator` in the prompt, and ship a default Codex agent so the capability is live.

**Architecture:** A new read-only `contracts.RosterProvider` port lists delegatable agents. The bridge assembles a `<delegation>` block into `Prompt.Context` (mirroring `withSkills`) from a roster the bridge process derives from the shared agents root. A default `codex` agent is provisioned idempotently at daemon boot. No coordination logic is added — `coordinator` (Delegate/Route/Report/…) and the `⟢` markers already exist and are reused verbatim.

**Tech Stack:** Go 1.25; modules `herrscher-contracts` (port) and `herrscher` (bridge + host wiring); `core/internal/agent` Store.

## Global Constraints

- Go 1.25; CI gates: `gofmt -l` clean, `go vet`, `go build`, `go test -race`, `go mod tidy` clean.
- No code comments except house-style doc comments on exported identifiers.
- No lint suppressions — fix the root cause.
- Commit author: `Akayashuu <sauvageleo1@gmail.com>`; NEVER add a `Co-Authored-By: Claude` trailer.
- Prefer classes/methods over free functions (OOP style).
- Commit message style: `feat(bridge): …` / `feat(contracts): …` / `docs(readme): …`.
- The `main` package (`herrscher/bridge.go`) is at the module root and CANNOT import `core/internal/agent` (internal boundary) — any code touching `agent.Store` lives under `core/`.

---

### Task 1: `contracts.RosterProvider` port

**Files:**
- Create: `/home/shan/dev/herrscher-contracts/roster.go`
- Test: `/home/shan/dev/herrscher-contracts/roster_test.go`

**Interfaces:**
- Produces: `type AgentInfo struct { Name, Backend, Summary string; Tags []string }` and `type RosterProvider interface { Agents() []AgentInfo }`.

- [ ] **Step 1: Write the failing test**

`/home/shan/dev/herrscher-contracts/roster_test.go`:

```go
package contracts

import "testing"

type fakeRoster struct{ agents []AgentInfo }

func (f fakeRoster) Agents() []AgentInfo { return f.agents }

func TestRosterProviderSatisfied(t *testing.T) {
	var _ RosterProvider = fakeRoster{}
	r := fakeRoster{agents: []AgentInfo{{Name: "codex", Backend: "codex", Tags: []string{"refactor"}}}}
	got := r.Agents()
	if len(got) != 1 || got[0].Name != "codex" || got[0].Backend != "codex" {
		t.Fatalf("unexpected roster projection: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestRosterProviderSatisfied`
Expected: FAIL — `undefined: RosterProvider` / `undefined: AgentInfo`.

- [ ] **Step 3: Write minimal implementation**

`/home/shan/dev/herrscher-contracts/roster.go`:

```go
package contracts

// AgentInfo is the delegation-relevant projection of a roster agent: what the
// model needs to name a delegate and reason about what it is good for.
type AgentInfo struct {
	Name    string   // name used in a ⟢ delegate: <name> marker
	Backend string   // backend vendor it runs on (claude, codex, …); "" = host default
	Summary string   // one-line description for the menu (may be empty)
	Tags    []string // capability tags, also what ⟢ route: matches on
}

// RosterProvider lists the agents a session may delegate to. It is an OPTIONAL
// capability the host supplies to the bridge: a nil provider (or an empty roster)
// yields no delegation affordance, so a deployment with no agents is unchanged.
type RosterProvider interface {
	Agents() []AgentInfo
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestRosterProviderSatisfied`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add roster.go roster_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(contracts): add RosterProvider port for delegation menus"
```

---

### Task 2: tag `herrscher-contracts` release and adopt it in `herrscher`

**Files:**
- Modify: `/home/shan/dev/herrscher/go.mod` (contracts require version)

**Interfaces:**
- Consumes: `contracts.RosterProvider`, `contracts.AgentInfo` (Task 1).

- [ ] **Step 1: Tag the contracts module**

```bash
cd /home/shan/dev/herrscher-contracts
git tag v0.2.4
```

- [ ] **Step 2: Point herrscher at the new contracts (workspace build already sees it)**

The `go.work` override makes the local checkout authoritative for dev builds, so no code change is needed to compile. Record the intended release floor in go.mod for the released-module (`GOWORK=off`) build:

Run:
```bash
cd /home/shan/dev/herrscher
GOWORK=off GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-contracts@v0.2.4
```
Expected: `go.mod` now requires `github.com/Herrscherd/herrscher-contracts v0.2.4`.

- [ ] **Step 3: Verify the workspace still builds**

Run: `cd /home/shan/dev/herrscher && go build ./...`
Expected: no output (success).

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher
git add go.mod go.sum
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "chore(deps): bump herrscher-contracts to v0.2.4 (RosterProvider)"
```

---

### Task 3: host roster adapter + agents-root helper

**Files:**
- Create: `/home/shan/dev/herrscher/core/host/roster.go`
- Test: `/home/shan/dev/herrscher/core/host/roster_test.go`

**Interfaces:**
- Consumes: `agent.Store.List()` (`core/internal/agent`), `contracts.AgentInfo` / `contracts.RosterProvider` (Task 1).
- Produces: `func DefaultAgentsRoot() string`; `func NewRoster(root string) contracts.RosterProvider`. `NewRoster` returns a value whose `Agents()` projects each `agent.Agent` to a `contracts.AgentInfo` (Name, Backend, Tags; Summary left empty for now). A missing root yields an empty slice.

- [ ] **Step 1: Write the failing test**

`/home/shan/dev/herrscher/core/host/roster_test.go`:

```go
package host

import (
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestRosterProjectsAgents(t *testing.T) {
	root := t.TempDir()
	st := agent.NewStore(root)
	if _, err := st.Create(agent.CreateSpec{Name: "codex", Backend: "codex", Tags: []string{"refactor", "tests"}}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	got := NewRoster(root).Agents()
	if len(got) != 1 {
		t.Fatalf("want 1 agent, got %d (%+v)", len(got), got)
	}
	a := got[0]
	if a.Name != "codex" || a.Backend != "codex" || len(a.Tags) != 2 {
		t.Fatalf("bad projection: %+v", a)
	}
}

func TestRosterEmptyWhenNoRoot(t *testing.T) {
	got := NewRoster(filepath.Join(t.TempDir(), "absent")).Agents()
	if len(got) != 0 {
		t.Fatalf("missing root must yield empty roster, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestRoster`
Expected: FAIL — `undefined: NewRoster`.

- [ ] **Step 3: Write minimal implementation**

`/home/shan/dev/herrscher/core/host/roster.go`:

```go
package host

import (
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// storeRoster adapts an agent.Store to contracts.RosterProvider, projecting each
// durable agent home to the delegation-relevant AgentInfo the bridge frames for
// the model.
type storeRoster struct{ store *agent.Store }

// DefaultAgentsRoot returns the directory holding agent homes, derived the same
// way the daemon derives it (the "agents" dir beside the state file), so the
// separate bridge process resolves the identical roster the coordinator sees.
func DefaultAgentsRoot() string {
	return filepath.Join(filepath.Dir(DefaultStatePath()), "agents")
}

// NewRoster builds a RosterProvider over the agent homes under root.
func NewRoster(root string) contracts.RosterProvider {
	return storeRoster{store: agent.NewStore(root)}
}

// Agents lists the delegatable agents. A missing root maps to an empty list
// (Store.List returns (nil, nil)); a genuine read error yields nil, so delegation
// simply has nothing to offer rather than breaking the turn.
func (r storeRoster) Agents() []contracts.AgentInfo {
	list, err := r.store.List()
	if err != nil {
		return nil
	}
	out := make([]contracts.AgentInfo, 0, len(list))
	for _, a := range list {
		out = append(out, contracts.AgentInfo{Name: a.Name, Backend: a.Backend, Tags: a.Tags})
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestRoster`
Expected: PASS (both cases).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/host/roster.go core/host/roster_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): agent.Store -> RosterProvider adapter + DefaultAgentsRoot"
```

---

### Task 4: bridge `withDelegation` framing + `Options.Roster`

**Files:**
- Modify: `/home/shan/dev/herrscher/core/bridge/bridge.go` (add `Roster` to `Options`)
- Create: `/home/shan/dev/herrscher/core/bridge/delegation.go`
- Modify: `/home/shan/dev/herrscher/core/bridge/hub.go` (thread roster into `runOneTurn`)
- Test: `/home/shan/dev/herrscher/core/bridge/delegation_test.go`

**Interfaces:**
- Consumes: `contracts.RosterProvider`, `contracts.AgentInfo` (Task 1).
- Produces: `func withDelegation(baseCtx string, roster contracts.RosterProvider) string` — appends a `<delegation>` block listing agents (`name (backend: X) — summary [tags]`) and the trailer syntax; returns `baseCtx` unchanged when `roster` is nil or lists no agents. `runOneTurn` and `runHubTurnsCtl` gain a trailing `roster contracts.RosterProvider` parameter.

- [ ] **Step 1: Write the failing test**

`/home/shan/dev/herrscher/core/bridge/delegation_test.go`:

```go
package bridge

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type stubRoster struct{ agents []contracts.AgentInfo }

func (s stubRoster) Agents() []contracts.AgentInfo { return s.agents }

func TestWithDelegationListsAgentsAndMarkers(t *testing.T) {
	r := stubRoster{agents: []contracts.AgentInfo{{Name: "codex", Backend: "codex", Tags: []string{"refactor", "tests"}}}}
	got := withDelegation("MEM", r)
	for _, want := range []string{"MEM", "<delegation>", "⟢ delegate:", "⟢ route:", "codex", "codex", "refactor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("delegation block missing %q in:\n%s", want, got)
		}
	}
}

func TestWithDelegationNilRosterUnchanged(t *testing.T) {
	if got := withDelegation("MEM", nil); got != "MEM" {
		t.Fatalf("nil roster must return base unchanged, got %q", got)
	}
}

func TestWithDelegationEmptyRosterUnchanged(t *testing.T) {
	if got := withDelegation("MEM", stubRoster{}); got != "MEM" {
		t.Fatalf("empty roster must return base unchanged, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/bridge/ -run TestWithDelegation`
Expected: FAIL — `undefined: withDelegation`.

- [ ] **Step 3: Write minimal implementation**

`/home/shan/dev/herrscher/core/bridge/delegation.go`:

```go
package bridge

import (
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// delegationIntro is the always-on affordance: it tells the model the async
// hand-off exists, how to trigger it, and its one hard precondition. The agent
// roster is appended per session.
const delegationIntro = "You can hand a full mission to another agent — it runs autonomously in its own " +
	"isolated worktree on its own backend and reports back to you when it is done " +
	"(async: you keep talking to the human meanwhile). To trigger it, end your reply " +
	"with ONE trailer line:\n" +
	"  ⟢ delegate: <agent> — <mission>   spawn a named worker and get its result back\n" +
	"  ⟢ route: <mission>                let the host pick the best-matching agent\n" +
	"When a worker's result later lands in your turn, synthesize it for the human. " +
	"This requires your session to be an isolated, committed worktree."

// withDelegation appends a <delegation> block — the intro plus the available
// agents — to baseCtx, mirroring withSkills. A nil roster or one that lists no
// agents returns baseCtx unchanged, so a deployment with no delegates is exactly
// as before.
func withDelegation(baseCtx string, roster contracts.RosterProvider) string {
	if roster == nil {
		return baseCtx
	}
	agents := roster.Agents()
	if len(agents) == 0 {
		return baseCtx
	}
	var b strings.Builder
	if baseCtx != "" {
		b.WriteString(baseCtx)
		b.WriteString("\n\n")
	}
	b.WriteString("<delegation>\n")
	b.WriteString(delegationIntro)
	b.WriteString("\nAvailable agents:\n")
	for _, a := range agents {
		b.WriteString(delegationLine(a))
	}
	b.WriteString("</delegation>")
	return b.String()
}

// delegationLine renders one roster entry: "  - name (backend: X) — summary [tags]".
func delegationLine(a contracts.AgentInfo) string {
	backend := a.Backend
	if backend == "" {
		backend = "host default"
	}
	line := fmt.Sprintf("  - %s (backend: %s)", a.Name, backend)
	if a.Summary != "" {
		line += " — " + a.Summary
	}
	if len(a.Tags) > 0 {
		line += " [" + strings.Join(a.Tags, " ") + "]"
	}
	return line + "\n"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test ./core/bridge/ -run TestWithDelegation`
Expected: PASS (all three).

- [ ] **Step 5: Thread the roster through Options and runOneTurn**

In `/home/shan/dev/herrscher/core/bridge/bridge.go`, add the field to `Options`:

```go
type Options struct {
	Channel string
	// HubSocket selects pure-runner (hub) mode: the bridge dials this socket,
	// reads input/pick frames from the daemon hub, and emits turn events back.
	HubSocket string
	// Roster lists the agents this session may delegate to (optional). When nil or
	// empty, no delegation affordance is injected.
	Roster contracts.RosterProvider
}
```

In the same file, `RunOneShot` calls `runOneTurn` — pass `nil` for its roster (the seed path has no delegation):

```go
		runOneTurn(ctx, channelSink{ctx: ctx, out: out}, resp, orch, ev, nil, newSkillEngine(resp), nil)
```

In `/home/shan/dev/herrscher/core/bridge/hub.go`:

- `runHub` passes `o.Roster` down: change the `runHubTurnsCtl(ctx, in, conn, resp, orch, ctrl, eng)` call to `runHubTurnsCtl(ctx, in, conn, resp, orch, ctrl, eng, o.Roster)`.
- `runHubTurns` (the test helper) passes nil: `runHubTurnsCtl(ctx, in, sink, resp, orch, nil, nil, nil)`.
- `runHubTurnsCtl` signature gains `roster contracts.RosterProvider` and forwards it: `runOneTurn(ctx, sink, resp, orch, ev, ctrl, eng, roster)`.
- `runOneTurn` signature gains `roster contracts.RosterProvider`; compose the delegation block onto the skills+memory context:

```go
	prompt := contracts.Prompt{Content: ev.Text, Context: withDelegation(withSkills(memCtx, eng), roster), Author: ev.Who, Attachments: ev.Attachments}
```

- [ ] **Step 6: Run the full bridge suite to verify wiring**

Run: `cd /home/shan/dev/herrscher && go test -race ./core/bridge/`
Expected: PASS (existing tests still compile with the new nil-passing call sites).

- [ ] **Step 7: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/bridge/delegation.go core/bridge/delegation_test.go core/bridge/bridge.go core/bridge/hub.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(bridge): inject delegation affordance from the session roster"
```

---

### Task 5: wire the roster into the bridge process

**Files:**
- Modify: `/home/shan/dev/herrscher/bridge.go` (main package `runBridge`)

**Interfaces:**
- Consumes: `host.NewRoster`, `host.DefaultAgentsRoot` (Task 3); `bridge.Options.Roster` (Task 4).

- [ ] **Step 1: Populate Options.Roster in runBridge**

In `/home/shan/dev/herrscher/bridge.go`, change the `bridge.Run` call at the end of `runBridge`:

```go
	return bridge.Run(ctx, newBackend, orch, bridge.Options{
		Channel:   *ch,
		HubSocket: *hubSocket,
		Roster:    host.NewRoster(host.DefaultAgentsRoot()),
	})
```

(`host` is already imported in this file.)

- [ ] **Step 2: Build to verify**

Run: `cd /home/shan/dev/herrscher && go build ./...`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
cd /home/shan/dev/herrscher
git add bridge.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(bridge): supply the session roster to the delegation affordance"
```

---

### Task 6: default Codex agent at daemon boot

**Files:**
- Modify: `/home/shan/dev/herrscher/core/host/cli.go` (call the ensure after the Store is built at line 43)
- Create: `/home/shan/dev/herrscher/core/host/codex_agent.go`
- Test: `/home/shan/dev/herrscher/core/host/codex_agent_test.go`

**Interfaces:**
- Consumes: `agent.Store.Get`, `agent.Store.Create`, `agent.CreateSpec` (`core/internal/agent`).
- Produces: `func ensureCodexAgent(store *agent.Store)` — idempotent: creates a `codex`/backend=codex agent with default tags when absent, no-op when present.

- [ ] **Step 1: Write the failing test**

`/home/shan/dev/herrscher/core/host/codex_agent_test.go`:

```go
package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestEnsureCodexAgentCreatesWhenAbsent(t *testing.T) {
	st := agent.NewStore(t.TempDir())
	ensureCodexAgent(st)
	a, ok := st.Get("codex")
	if !ok {
		t.Fatal("codex agent was not created")
	}
	if a.Backend != "codex" || len(a.Tags) == 0 {
		t.Fatalf("codex agent malformed: %+v", a)
	}
}

func TestEnsureCodexAgentIdempotent(t *testing.T) {
	st := agent.NewStore(t.TempDir())
	if _, err := st.Create(agent.CreateSpec{Name: "codex", Backend: "codex", Tags: []string{"custom"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ensureCodexAgent(st) // must not overwrite
	a, _ := st.Get("codex")
	if len(a.Tags) != 1 || a.Tags[0] != "custom" {
		t.Fatalf("ensure clobbered an existing agent: %+v", a)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestEnsureCodexAgent`
Expected: FAIL — `undefined: ensureCodexAgent`.

- [ ] **Step 3: Write minimal implementation**

`/home/shan/dev/herrscher/core/host/codex_agent.go`:

```go
package host

import "github.com/Herrscherd/herrscher/core/internal/agent"

// codexDefaultTags are the capability tags the auto-provisioned Codex delegate
// advertises — the kinds of mission it is a sensible default for and what
// ⟢ route: matches against.
var codexDefaultTags = []string{"refactor", "tests", "mechanical"}

// ensureCodexAgent makes the default Codex delegate exist so cross-model
// delegation works out of the box. It is idempotent and never overwrites an
// existing "codex" agent (a manual `agent create` or a prior boot wins), and it
// is best-effort: a creation failure is swallowed so a boot never blocks on it.
func ensureCodexAgent(store *agent.Store) {
	if _, ok := store.Get("codex"); ok {
		return
	}
	_, _ = store.Create(agent.CreateSpec{Name: "codex", Backend: "codex", Tags: codexDefaultTags})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestEnsureCodexAgent`
Expected: PASS (both).

- [ ] **Step 5: Call it at boot**

In `/home/shan/dev/herrscher/core/host/cli.go`, immediately after line 43 (`agents := agent.NewStore(filepath.Join(partDir, "agents"))`), add:

```go
	ensureCodexAgent(agents)
```

- [ ] **Step 6: Build and run the host suite**

Run: `cd /home/shan/dev/herrscher && go build ./... && go test -race ./core/host/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/host/codex_agent.go core/host/codex_agent_test.go core/host/cli.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): auto-provision a default codex delegate at boot"
```

---

### Task 7: full CI gate, docs, and release

**Files:**
- Modify: `/home/shan/dev/herrscher/README.md` (add a "Cross-model delegation" section + TOC entry)
- Modify: `/home/shan/dev/herrscher/docs/superpowers/specs/2026-07-23-conscious-delegation-design.md` (status → shipped with versions)

- [ ] **Step 1: Run the whole gate**

Run:
```bash
cd /home/shan/dev/herrscher && gofmt -l . && go vet ./... && go build ./... && go test -race ./... && go mod tidy && git diff --exit-code go.mod go.sum
```
Expected: `gofmt -l` prints nothing; vet/build/test succeed; `go mod tidy` leaves go.mod/go.sum unchanged (exit 0).

- [ ] **Step 2: Document the feature in the README**

Add a section (near the "Cross-backend skills" / "Conscious memory" sections) describing: the model is shown a `<delegation>` menu of agents each turn; it delegates with `⟢ delegate: <agent> — <mission>` or `⟢ route: <mission>`; the worker runs async on its own backend (e.g. Codex) in an isolated committed worktree and reports back into the lead's turn; a default `codex` agent is auto-provisioned and more can be added with `agent create --backend codex`. Add the matching TOC entry.

- [ ] **Step 3: Mark the spec shipped**

In the design spec, set the status line to: `**Status:** shipped — contracts v0.2.4, herrscher v0.1.36`.

- [ ] **Step 4: Commit docs**

```bash
cd /home/shan/dev/herrscher
git add README.md docs/superpowers/specs/2026-07-23-conscious-delegation-design.md
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "docs(readme): document conscious cross-model delegation"
```

- [ ] **Step 5: Tag the herrscher release and reinstall the local binary**

```bash
cd /home/shan/dev/herrscher
git tag v0.1.36
go install .
~/go/bin/herrscher --version
```
Expected: reports `herrscher 0.1.36` (or the module's version string).

---

## Self-Review

**Spec coverage:**
- Component 1 (RosterProvider port) → Task 1. ✓
- Component 2 (bridge `withDelegation` + `Options.Roster`) → Task 4 (+ Task 5 wires the roster source). ✓
- Component 3 (idempotent default codex agent) → Task 6. ✓
- Error handling (nil/empty roster → block omitted; idempotent provisioning; missing root → empty) → Tasks 3, 4, 6 tests. ✓
- Testing section (contracts satisfies; withDelegation lists+nil-unchanged+composes; adapter projects; provisioning idempotent) → Tasks 1, 3, 4, 6. ✓
- Non-regression on coordinator/markers → untouched; existing suites run in Task 7. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code. ✓

**Type consistency:** `contracts.AgentInfo{Name, Backend, Summary, Tags}` and `RosterProvider.Agents() []AgentInfo` used identically in Tasks 1/3/4. `NewRoster(root string) contracts.RosterProvider` and `DefaultAgentsRoot() string` consumed verbatim in Task 5. `runOneTurn`/`runHubTurnsCtl` gain the same trailing `roster contracts.RosterProvider` param across all call sites (Task 4 Step 5). `ensureCodexAgent(*agent.Store)` defined and called in Task 6. ✓

**Note on `storeRoster.Agents` (Task 3):** `agent.Store.List()` maps a missing root to `(nil, nil)`, so the empty-root test hits the normal projection path over an empty list; only a genuine read error returns nil (no affordance).
