# Backend-Native Resume — Implementation Plan (Plan 1 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist each session's backend resume id and feed it back on restart, so a supervised session (`main` or named) comes back up with its claude conversation context intact — no picker required.

**Architecture:** Three modules, bottom-up. `herrscher-contracts` gains a neutral `ResumeAware` interface and an `Event.Resume` field. `herrscher-claude-backend` already parses claude's stream-json `session_id` and already appends `--resume` in `streamArgv`; we wire a resume id *in* (via `Config.ResumeID`) and expose the live id *out* (via `ResumeAware`). `herrscher` carries the token: the bridge piggybacks it on the terminal `reply{done}` event, the daemon turn loop folds it into `state.Session.ResumeToken` (persist-on-change), and on boot the supervisor passes `--resume <token>` to the bridge child.

**Tech Stack:** Go, standard `go test`. Cross-module local dev via a `go.work` file at `/home/shan/dev` (never committed).

## Global Constraints

- **Core stays platform-blind.** herrscher core names no vendor. The resume token is an opaque `string`. `core/purity_test.go` and `core/host` purity tests must stay green.
- **Best-effort, non-blocking.** A missing/empty token is normal; it must degrade to today's behavior (fresh backend), never break a turn.
- **Backward compatible.** New fields are `omitempty`. Sessions persisted before this change (no `resumeToken`) behave exactly as today until their first turn folds an id in.
- **Public deps only.** herrscher host CI fetches modules with no auth; the final herrscher commit must reference **published** contracts/backend versions — no `replace` directives and no `go.work` committed to any repo.
- **claude session_id is invariant across a conversation** — it changes only when a brand-new session starts (no `--resume`). The folded token is therefore stable; passing `--resume <token>` at boot is never stale.
- Module paths: `github.com/Herrscherd/herrscher`, `github.com/Herrscherd/herrscher-contracts`, `github.com/Herrscherd/herrscher-claude-backend`. Local checkouts: `/home/shan/dev/herrscher{,-contracts,-claude-backend}`.

---

### Task 0: Local cross-module dev harness

**Files:**
- Create: `/home/shan/dev/go.work` (NOT committed to any repo)

- [ ] **Step 1: Create the workspace linking all local repos**

Run:
```bash
cd /home/shan/dev && go work init ./herrscher ./herrscher-contracts ./herrscher-claude-backend ./herrscher-codex-backend ./herrscher-cursor-backend
```
A `go.work` at `/home/shan/dev` is above every repo root, so `go build`/`go test` run from inside any repo resolve `github.com/Herrscherd/herrscher-*` deps to the local checkouts. It is committed to **no** repo (it lives in the parent dir).

- [ ] **Step 2: Verify each module still builds through the workspace**

Run:
```bash
cd /home/shan/dev/herrscher-contracts && go build ./... && \
cd /home/shan/dev/herrscher-claude-backend && go build ./... && \
cd /home/shan/dev/herrscher && go build ./...
```
Expected: all succeed (no code changed yet).

*(No commit — this task changes no tracked file.)*

---

### Task 1: contracts — `ResumeAware` + `Event.Resume`

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/backend.go`
- Modify: `/home/shan/dev/herrscher-contracts/event.go`
- Test: `/home/shan/dev/herrscher-contracts/resume_test.go` (create)

**Interfaces:**
- Produces: `contracts.ResumeAware` interface with `ResumeToken() string`; `contracts.Event.Resume string` field (JSON `resume,omitempty`).

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-contracts/resume_test.go`:
```go
package contracts

import (
	"encoding/json"
	"testing"
)

// resumableStub proves a type can satisfy ResumeAware.
type resumableStub struct{ tok string }

func (r resumableStub) ResumeToken() string { return r.tok }

func TestResumeAwareSatisfied(t *testing.T) {
	var _ ResumeAware = resumableStub{tok: "abc"}
}

func TestEventResumeJSON(t *testing.T) {
	b, err := json.Marshal(Event{T: "reply", Done: true, Resume: "sid-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"t":"reply","done":true,"resume":"sid-1"}` {
		t.Fatalf("marshal: got %s", got)
	}
	// Empty Resume must be omitted.
	b, _ = json.Marshal(Event{T: "reply"})
	if got := string(b); got != `{"t":"reply"}` {
		t.Fatalf("omitempty: got %s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run 'Resume|EventResume' -v`
Expected: FAIL — `ResumeAware` undefined, `Event` has no field `Resume`.

- [ ] **Step 3: Add the interface**

In `backend.go`, after the `Backend` interface, add:
```go
// ResumeAware is implemented by backends that can be resumed later. The host
// reads the opaque token after a turn, persists it, and feeds it back at
// construction via cfg.Settings["resume"]. "" means "no resumable id yet".
type ResumeAware interface {
	ResumeToken() string
}
```

- [ ] **Step 4: Add the Event field**

In `event.go`, add to the `Event` struct after `Cost`:
```go
	// Resume carries the backend's opaque resume token (e.g. claude's stable
	// session_id), piggybacked on the terminal reply{done} so the daemon can
	// persist it for cross-restart --resume. Empty when the backend is not
	// ResumeAware or has no id yet.
	Resume string `json:"resume,omitempty"`
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -v`
Expected: PASS (all contracts tests).

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher-contracts && git add backend.go event.go resume_test.go && \
git commit -m "feat(contracts): ResumeAware interface + Event.Resume field"
```

---

### Task 2: claude backend — thread resume id in + expose it out

**Files:**
- Modify: `/home/shan/dev/herrscher-claude-backend/backend.go` (Config + NewBackend stream case)
- Modify: `/home/shan/dev/herrscher-claude-backend/stream.go` (streamResponder)
- Modify: `/home/shan/dev/herrscher-claude-backend/register.go` (config mapping)
- Test: `/home/shan/dev/herrscher-claude-backend/resume_test.go` (create)

**Interfaces:**
- Consumes: `contracts.ResumeAware` (Task 1).
- Produces: `Config.ResumeID string`; `streamResponder.ResumeToken()`; `cfg.Get("resume")` → `Config.ResumeID` wiring.

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-claude-backend/resume_test.go`:
```go
package claude

import (
	"context"
	"io"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestStreamResponderResumeToken(t *testing.T) {
	// Before the first turn, the token is whatever was passed in at construction.
	r := &streamResponder{resumeID: "boot-id"}
	if got := r.ResumeToken(); got != "boot-id" {
		t.Fatalf("nil session: want boot-id, got %q", got)
	}
	// Once a session exists, the live claude session_id wins.
	r.sess = newStreamSession(nopWriteCloser{io.Discard}, strings.NewReader(""))
	r.sess.sessID = "live-id"
	if got := r.ResumeToken(); got != "live-id" {
		t.Fatalf("live session: want live-id, got %q", got)
	}
}

func TestStreamResponderIsResumeAware(t *testing.T) {
	var _ contracts.ResumeAware = (*streamResponder)(nil)
}

func TestNewBackendThreadsResumeID(t *testing.T) {
	b, err := NewBackend(context.Background(), Config{Kind: "stream", Cmd: "claude", ResumeID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := b.(*streamResponder)
	if !ok {
		t.Fatalf("want *streamResponder, got %T", b)
	}
	if r.resumeID != "x" {
		t.Fatalf("resumeID not threaded: got %q", r.resumeID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-claude-backend && go test ./... -run Resume -v`
Expected: FAIL — `streamResponder` has no field `resumeID`, no method `ResumeToken`; `Config` has no field `ResumeID`.

- [ ] **Step 3: Add `ResumeID` to Config and thread it in NewBackend**

In `backend.go`, add to `Config`:
```go
	ResumeID string // claude session id to resume on first start ("" = fresh)
```
In `NewBackend`, change the `default: // "stream"` case to:
```go
	default: // "stream"
		r := &streamResponder{ctx: ctx, base: streamBase(strings.Fields(c.Cmd)), model: c.Model, resumeID: c.ResumeID}
		r.dir = c.Dir
		return r, nil
```

- [ ] **Step 4: Add `resumeID` field, first-start use, and `ResumeToken()` in stream.go**

In `stream.go`, add `resumeID` to `streamResponder`:
```go
type streamResponder struct {
	ctx      context.Context
	base     []string
	model    string
	dir      string
	resumeID string // id to resume on the FIRST start ("" = fresh session)
	mu       sync.Mutex
	sess     *streamSession
}
```
In `streamResponder.Respond`, change the first-start line from `startStreamSession(r.ctx, r.base, r.model, "", r.dir)` to:
```go
		s, err := startStreamSession(r.ctx, r.base, r.model, r.resumeID, r.dir)
```
Add the method (place after `Respond`):
```go
// ResumeToken returns the backend's current claude session id — the stable id
// for this conversation, for the host to persist and pass back via --resume.
// Before the first turn it returns the id supplied at construction. Implements
// contracts.ResumeAware.
func (r *streamResponder) ResumeToken() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sess == nil {
		return r.resumeID
	}
	r.sess.mu.Lock()
	defer r.sess.mu.Unlock()
	return r.sess.sessID
}
```

- [ ] **Step 5: Map the `resume` setting in register.go**

In `register.go`, change the `Backend` factory to include `ResumeID`:
```go
		Backend: func(ctx context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			return NewBackend(ctx, Config{
				Kind:     cfg.Get("kind"),
				Stream:   cfg.Get("stream") != "false",
				Cmd:      cfg.Get("cmd"),
				Model:    cfg.Get("model"),
				Dir:      cfg.Get("dir"),
				ResumeID: cfg.Get("resume"),
			})
		},
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-claude-backend && go test ./... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /home/shan/dev/herrscher-claude-backend && git add backend.go stream.go register.go resume_test.go && \
git commit -m "feat(claude): thread resume id in (Config.ResumeID) + expose it out (ResumeAware)"
```

---

### Task 3: herrscher state — `Session.ResumeToken` + `SetResumeToken`

**Files:**
- Modify: `core/internal/state/state.go`
- Test: `core/internal/state/state_resume_test.go` (create)

**Interfaces:**
- Produces: `state.Session.ResumeToken string`; `(*State).SetResumeToken(name, token string) error` (persist-on-change, no-op for unknown session or unchanged token).

- [ ] **Step 1: Write the failing test**

Create `core/internal/state/state_resume_test.go`:
```go
package state

import (
	"path/filepath"
	"testing"
)

func TestSetResumeTokenPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	if err := s.AddSession(Session{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetResumeToken("main", "sid-1"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.FindSession("main")
	if got.ResumeToken != "sid-1" {
		t.Fatalf("want sid-1, got %q", got.ResumeToken)
	}
}

func TestSetResumeTokenUnknownSessionIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := s.SetResumeToken("ghost", "sid"); err != nil {
		t.Fatalf("unknown session must be a silent no-op, got %v", err)
	}
}

func TestSetResumeTokenUnchangedIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	_ = s.AddSession(Session{Name: "main", ResumeToken: "sid-1"})
	if err := s.SetResumeToken("main", "sid-1"); err != nil {
		t.Fatalf("unchanged token must be a no-op, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/state/ -run Resume -v`
Expected: FAIL — `Session` has no field `ResumeToken`; `SetResumeToken` undefined.

- [ ] **Step 3: Add the field**

In `state.go`, add to the `Session` struct (after the `Agent` field):
```go
	// ResumeToken is the backend's opaque resume id (e.g. claude's stable
	// session_id), folded in from each turn's reply so a restart can resume the
	// conversation with --resume. Empty = start fresh.
	ResumeToken string `json:"resumeToken,omitempty"`
```

- [ ] **Step 4: Add the mutator**

In `state.go`, after `RemoveSession`, add:
```go
// SetResumeToken records the backend resume token for the named session,
// persisting only when it changes. Turns report the same id, so this avoids
// rewriting state.json every turn. A missing session or an unchanged token is a
// no-op.
func (s *State) SetResumeToken(name, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].Name == name {
			if s.Sessions[i].ResumeToken == token {
				return nil
			}
			s.Sessions[i].ResumeToken = token
			return s.saveLocked()
		}
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/state/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/internal/state/state.go core/internal/state/state_resume_test.go && \
git commit -m "feat(state): Session.ResumeToken + SetResumeToken (persist-on-change)"
```

---

### Task 4: herrscher wiring — `BuildBackend` resume param, bridge `-resume` flag, supervisor `--resume` arg

**Files:**
- Modify: `core/host/seed.go` (`BuildBackend` signature, `newSeedBackend`)
- Modify: `core/host/seed_test.go:76` (call-site signature)
- Modify: `bridge.go` (add `-resume` flag; pass to `BuildBackend`)
- Modify: `core/internal/supervisor/supervisor.go` (`bridgeArgs`)
- Test: `core/internal/supervisor/supervisor_resume_test.go` (create)

**Interfaces:**
- Consumes: `state.Session.ResumeToken` (Task 3).
- Produces: `BuildBackend(ctx, vendor, cmd, kind, dir, resume string)`; supervisor emits `--resume <token>` when set.

- [ ] **Step 1: Write the failing test**

Create `core/internal/supervisor/supervisor_resume_test.go`:
```go
package supervisor

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestBridgeArgsIncludesResume(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	args := s.bridgeArgs(state.Session{Name: "main", ChannelID: "c1", Cmd: "claude", ResumeToken: "sid-1"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--resume sid-1") {
		t.Fatalf("bridgeArgs must pass --resume sid-1; got %q", joined)
	}
}

func TestBridgeArgsOmitsResumeWhenEmpty(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	args := s.bridgeArgs(state.Session{Name: "main", ChannelID: "c1", Cmd: "claude"})
	if strings.Contains(strings.Join(args, " "), "--resume") {
		t.Fatalf("no --resume expected for empty token; got %v", args)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/supervisor/ -run Resume -v`
Expected: FAIL — `bridgeArgs` does not emit `--resume`.

- [ ] **Step 3: Emit `--resume` from bridgeArgs**

In `core/internal/supervisor/supervisor.go`, in `bridgeArgs`, before `return args`, add:
```go
	if sess.ResumeToken != "" {
		args = append(args, "--resume", sess.ResumeToken)
	}
```

- [ ] **Step 4: Run the supervisor test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/supervisor/ -v`
Expected: PASS.

- [ ] **Step 5: Extend `BuildBackend` with a resume param**

In `core/host/seed.go`, change the signature and body of `BuildBackend`:
```go
func BuildBackend(ctx context.Context, vendor, cmd, kind, dir, resume string) (contracts.Backend, error) {
```
and after the existing `if dir != "" { cfg.Settings["dir"] = dir }` block, add:
```go
	if resume != "" {
		cfg.Settings["resume"] = resume
	}
```
Update `newSeedBackend` to pass the session's token:
```go
func newSeedBackend(ctx context.Context, sess state.Session) (contracts.Backend, error) {
	return BuildBackend(ctx, sess.Vendor, sess.Cmd, sess.Backend, sess.Worktree, sess.ResumeToken)
}
```

- [ ] **Step 6: Update the existing `BuildBackend` call sites**

In `core/host/seed_test.go:76`, change:
```go
	if _, err := BuildBackend(context.Background(), "codex", "codex --model gpt-5.6", "", "", ""); err != nil {
```
In `bridge.go`, add the flag next to the other `fs.String` declarations:
```go
	resume := fs.String("resume", "", "backend resume token (claude session id) to resume the conversation on start")
```
and change the `BuildBackend` call inside `newBackend`:
```go
		return host.BuildBackend(ctx, *vendor, *cmdStr, *backend, "", *resume)
```

- [ ] **Step 7: Run the build + affected tests**

Run: `cd /home/shan/dev/herrscher && go build ./... && go test ./core/host/ ./core/internal/supervisor/ -v`
Expected: build OK; tests PASS.

- [ ] **Step 8: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/host/seed.go core/host/seed_test.go bridge.go core/internal/supervisor/supervisor.go core/internal/supervisor/supervisor_resume_test.go && \
git commit -m "feat(host): thread resume token through BuildBackend, bridge -resume flag, supervisor --resume arg"
```

---

### Task 5: herrscher bridge — carry the token on `reply{done}`

**Files:**
- Modify: `core/bridge/hub.go` (`runOneTurn` + a `resumeToken` helper)
- Test: `core/bridge/hub_resume_test.go` (create)

**Interfaces:**
- Consumes: `contracts.ResumeAware`, `contracts.Event.Resume` (Task 1).
- Produces: the terminal `reply{done}` event now sets `Resume` from the backend when it is `ResumeAware`.

- [ ] **Step 1: Write the failing test**

Create `core/bridge/hub_resume_test.go`:
```go
package bridge

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// resumableBackend is a fakeBackend that also reports a resume token.
type resumableBackend struct {
	fakeBackend
	token string
}

func (b resumableBackend) ResumeToken() string { return b.token }

func TestRunHubReplyCarriesResumeToken(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, resumableBackend{fakeBackend: fakeBackend{reply: "ok"}, token: "sid-1"}, nil)

	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || !last.Done || last.Resume != "sid-1" {
		t.Fatalf("reply must carry resume sid-1; got %+v", last)
	}
}

func TestRunHubReplyResumeEmptyWhenNotResumeAware(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, fakeBackend{reply: "ok"}, nil)

	last := sink.events[len(sink.events)-1]
	if last.Resume != "" {
		t.Fatalf("non-ResumeAware backend must leave Resume empty; got %q", last.Resume)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/bridge/ -run Resume -v`
Expected: FAIL — `reply` event's `Resume` is empty (not yet set).

- [ ] **Step 3: Set `Resume` on the reply and add the helper**

In `core/bridge/hub.go`, in `runOneTurn`, change the reply emission line to:
```go
	sink.Emit(contracts.Event{T: "reply", Text: out, Done: true, Cost: cost, Resume: resumeToken(resp)})
```
Add the helper (place after `runOneTurn`):
```go
// resumeToken reads a backend's opaque resume token when it is ResumeAware, so
// the daemon can persist it for cross-restart --resume. "" when unsupported.
func resumeToken(resp contracts.Backend) string {
	if ra, ok := resp.(contracts.ResumeAware); ok {
		return ra.ResumeToken()
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/shan/dev/herrscher && go test ./core/bridge/ -v`
Expected: PASS (new + existing bridge tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/bridge/hub.go core/bridge/hub_resume_test.go && \
git commit -m "feat(bridge): carry backend resume token on reply{done}"
```

---

### Task 6: herrscher turnloop — fold the token into state

**Files:**
- Modify: `core/host/turnloop.go` (`sessionDriver.persistResume`, `awaitTurn`, `RunSession` signature)
- Modify: `core/host/hub.go:71` (wire the persist callback)
- Modify: `core/host/turnloop_test.go:262,449,480` (add `nil` arg to `RunSession` calls)
- Test: `core/host/turnloop_resume_test.go` (create)

**Interfaces:**
- Consumes: `contracts.Event.Resume` (Task 1), `(*State).SetResumeToken` (Task 3).
- Produces: `RunSession(..., coord, persistResume func(string))`; on `reply{done}` with a non-empty `Resume`, `persistResume` is invoked.

- [ ] **Step 1: Write the failing test**

Create `core/host/turnloop_resume_test.go`:
```go
package host

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestAwaitTurnPersistsResumeToken(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from)
	var got string
	d.persistResume = func(tok string) { got = tok }

	from <- contracts.Event{T: "reply", Text: "ok", Done: true, Resume: "sid-1"}
	if !d.awaitTurn(context.Background()) {
		t.Fatal("awaitTurn should return true on reply{done}")
	}
	if got != "sid-1" {
		t.Fatalf("persistResume: want sid-1, got %q", got)
	}
}

func TestAwaitTurnSkipsEmptyResumeToken(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from)
	called := false
	d.persistResume = func(string) { called = true }

	from <- contracts.Event{T: "reply", Text: "ok", Done: true} // no Resume
	_ = d.awaitTurn(context.Background())
	if called {
		t.Fatal("persistResume must not fire for an empty token")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run AwaitTurn -v`
Expected: FAIL — `sessionDriver` has no field `persistResume`.

- [ ] **Step 3: Add the field and the fold**

In `core/host/turnloop.go`, add to the `sessionDriver` struct (after `coordinator`):
```go
	// persistResume folds a completed turn's backend resume token into durable
	// state (nil = disabled, e.g. tests and the operator CLI path).
	persistResume func(token string)
```
In `awaitTurn`, inside the `if e.T == "reply" && e.Done {` block, at the very top, add:
```go
			if d.persistResume != nil && e.Resume != "" {
				d.persistResume(e.Resume)
			}
```

- [ ] **Step 4: Extend `RunSession` and wire the driver**

In `core/host/turnloop.go`, change the `RunSession` signature to add a trailing param:
```go
func RunSession(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, persistResume func(string)) {
```
and after `d.coordinator = coord`, add:
```go
	d.persistResume = persistResume
```

- [ ] **Step 5: Wire the production call site in hub.go**

In `core/host/hub.go:71`, change the `RunSession` call to pass a state-writing callback:
```go
	go RunSession(sctx, sess.Name, sess.ChannelID, bound, acc, state.ParticipantsPath(h.partDir, sess.Name), h.metrics, h.coordinator,
		func(tok string) { _ = h.st.SetResumeToken(sess.Name, tok) })
```

- [ ] **Step 6: Fix the test call sites**

In `core/host/turnloop_test.go`, add a trailing `nil` to each `RunSession(...)` call (lines ~262, ~449, ~480):
```go
	go RunSession(ctx, "pickreg", "", []contracts.GatewaySet{{Gateway: a, Reader: a}}, acc, "", nil, nil, nil)
```
```go
	go RunSession(ctx, "s1", "", []contracts.GatewaySet{{Gateway: a, Reader: a}}, acc, "", nil, nil, nil)
```
(apply to both `s1` call sites).

- [ ] **Step 7: Run the host tests to verify they pass**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/host/turnloop.go core/host/hub.go core/host/turnloop_test.go core/host/turnloop_resume_test.go && \
git commit -m "feat(host): fold backend resume token into state on turn completion"
```

---

### Task 7: Whole-repo verification, manual end-to-end, and publish

**Files:**
- Modify: `/home/shan/dev/herrscher-claude-backend/go.mod` (bump contracts require)
- Modify: `go.mod` (bump contracts + claude-backend requires)
- Delete: `/home/shan/dev/go.work`

**Interfaces:** none (integration + release).

- [ ] **Step 1: Full test sweep across all three modules (via go.work)**

Run:
```bash
cd /home/shan/dev/herrscher-contracts && go test ./... && \
cd /home/shan/dev/herrscher-claude-backend && go test ./... && \
cd /home/shan/dev/herrscher && go test ./...
```
Expected: all PASS, including `core/purity_test.go` and `core/host` purity tests (core still names no vendor).

- [ ] **Step 2: Manual end-to-end resume check**

Build and run the TUI, hold a short conversation, quit, relaunch, and confirm the model has context:
```bash
cd /home/shan/dev/herrscher && go build -o /tmp/herrscher . && \
grep -o '"resumeToken": *"[^"]*"' ~/.config/dctl/state.json || echo "no token yet — run the TUI first"
```
Procedure (record the result in the PR):
1. `/tmp/herrscher` → in the `main` tab say: "Remember the codeword is HERRSCHER-42."
2. Wait for the reply, then quit (Ctrl+C).
3. Confirm `~/.config/dctl/state.json`'s `main` session now has a non-empty `resumeToken`.
4. `/tmp/herrscher` again → ask: "What was the codeword?"
5. Expected: the model answers "HERRSCHER-42" (context resumed). If it does not know, resume failed — capture stderr (the bridge logs `--resume`) before diagnosing.

- [ ] **Step 3: Publish contracts (bottom of the stack)**

Requires push access to `github.com/Herrscherd/herrscher-contracts`.
```bash
cd /home/shan/dev/herrscher-contracts && git push origin HEAD && \
git tag v0.1.16 && git push origin v0.1.16
```

- [ ] **Step 4: Bump + publish the claude backend**

```bash
cd /home/shan/dev/herrscher-claude-backend && \
go mod edit -require=github.com/Herrscherd/herrscher-contracts@v0.1.16 && \
GOFLAGS=-mod=mod go build ./... && \
git add go.mod go.sum && git commit -m "chore: require contracts v0.1.16 (ResumeAware)" && \
git push origin HEAD && git tag v0.1.3 && git push origin v0.1.3
```
(Temporarily disable the workspace for the module-graph resolution: `GOWORK=off` may be needed on the `go mod edit`/build if the proxy is required — `GOWORK=off GOFLAGS=-mod=mod go build ./...`.)

- [ ] **Step 5: Bump herrscher to the published versions and drop the workspace**

```bash
rm /home/shan/dev/go.work && \
cd /home/shan/dev/herrscher && \
go mod edit -require=github.com/Herrscherd/herrscher-contracts@v0.1.16 -require=github.com/Herrscherd/herrscher-claude-backend@v0.1.3 && \
GOWORK=off go mod tidy && GOWORK=off go build ./... && GOWORK=off go test ./...
```
Expected: resolves the published modules, builds, all tests PASS with no `go.work` and no `replace`.

- [ ] **Step 6: Commit the herrscher dependency bump**

```bash
cd /home/shan/dev/herrscher && git add go.mod go.sum && \
git commit -m "chore: bump contracts v0.1.16 + claude-backend v0.1.3 for backend resume"
```

- [ ] **Step 7: Confirm the branch is clean and pushable**

Run: `cd /home/shan/dev/herrscher && git status && git log --oneline -8`
Expected: working tree clean; the `feat/session-resume` branch holds Tasks 3–7's herrscher commits; no `go.work`/`replace` present.

---

## Self-Review

**Spec coverage (backend-native resume slice):**
- ResumeAware contract seam → Task 1. ✓
- claude `Config.ResumeID` in + `ResumeToken()` out → Task 2. ✓
- `state.Session.ResumeToken` + persist-on-change → Task 3. ✓
- `Settings["resume"]` via `BuildBackend`; supervisor `--resume`; bridge `-resume` flag → Task 4. ✓
- Event transport (`Event.Resume` set in `runOneTurn`) → Tasks 1 + 5. ✓
- Daemon fold in `awaitTurn` → Task 6. ✓
- Stale/expired token fallback: **partially deferred.** claude's own restart-on-death path already retries; a hard `--resume <bad>` failure at first start currently surfaces as a turn error. Full "clear the token and restart fresh" is tracked as a follow-up in Plan 3's error-handling pass — noted here so it is not assumed done.
- Transcript / scrollback / picker / close-archive semantics → **out of scope for Plan 1** (Plans 2 and 3).

**Placeholder scan:** No TBD/TODO; every code step shows exact code. ✓

**Type consistency:** `ResumeToken()` (method) and `ResumeToken` (state field) are distinct by receiver/context; `resumeID` (claude field) vs `resume` (setting key / flag) vs `ResumeToken` (state) are used consistently per repo. `BuildBackend` arity is 6 everywhere after Task 4. `RunSession` arity gains exactly one trailing param, updated at all four call sites (1 prod + 3 test). ✓

**Known limitations carried into later plans:** stale-token self-heal (above); no transcript yet (so a resumed session has model context but the TUI tab still starts visually empty until Plan 2's scrollback lands).
