# Transcript, scrollback replay & `/resume` picker — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist a per-session transcript, replay it as dimmed scrollback when a TUI tab opens, and add a `/resume` picker with archive (close-but-keep) semantics.

**Architecture:** The daemon owns the state dir and both sides of a turn, so it records the transcript single-writer in `turnloop.go` (same point as `persistResume`). Archive is a new verb that keeps the row + transcript + resume token; `close` stays destructive. The TUI half (scrollback + picker) crosses the platform-blind boundary, which only `herrscher-contracts` can carry — so it needs a contracts bump.

**Tech Stack:** Go, herrscher-contracts (public module), Bubble Tea TUI.

## Global Constraints

- **Core stays platform-blind.** No vendor named in `core/`; `purity_test.go` must stay green. Resume token is an opaque `string` (already true).
- **Best-effort observability.** Transcript/replay failures must never error a turn — mirror `core/internal/state/journal.go` (return errors the caller ignores).
- **Backward compatible.** New `state.Session.Archived` and the transcript file are absent on old sessions (`omitempty`); behavior is unchanged until the first new turn.
- **Public deps only.** Every module imported in `plugins.go` must stay a public repo; `herrscher-contracts` is public and versioned (currently `v0.1.16`).
- **gofmt.** Run `gofmt -w` on every touched `.go` file before committing.

---

## PART A — daemon-internal (no contract change)

Fully shippable on its own: the transcript is recorded and archive works. Parts B builds on it.

### Task A1: Transcript storage in `state`

**Files:**
- Create: `core/internal/state/transcript.go`
- Test: `core/internal/state/transcript_test.go`

**Interfaces:**
- Produces:
  - `type TranscriptEntry struct { Ts string; Role string; Text string; Cost float64; Kind string }` with JSON tags `ts`, `role`, `text`, `cost,omitempty`, `kind,omitempty`.
  - `func TranscriptPath(dir, name string) string` → `dir/transcripts/<name>.jsonl`
  - `func AppendTranscript(path string, e TranscriptEntry) error`
  - `func ReadTranscript(path string, cap int) []TranscriptEntry` (last `cap`; `cap<=0` = all)
  - `func RemoveTranscript(path string) error`

- [ ] **Step 1: Write the failing test**

```go
package state

import (
	"path/filepath"
	"testing"
)

func TestTranscriptAppendReadCap(t *testing.T) {
	dir := t.TempDir()
	p := TranscriptPath(dir, "sess")
	for i, role := range []string{"user", "assistant", "user", "assistant"} {
		if err := AppendTranscript(p, TranscriptEntry{Ts: "t", Role: role, Text: string(rune('a' + i))}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	all := ReadTranscript(p, 0)
	if len(all) != 4 {
		t.Fatalf("want 4 entries, got %d", len(all))
	}
	if all[0].Role != "user" || all[3].Role != "assistant" {
		t.Fatalf("order wrong: %+v", all)
	}
	last2 := ReadTranscript(p, 2)
	if len(last2) != 2 || last2[0].Text != "c" || last2[1].Text != "d" {
		t.Fatalf("cap wrong: %+v", last2)
	}
	if got := TranscriptPath(dir, "sess"); got != filepath.Join(dir, "transcripts", "sess.jsonl") {
		t.Fatalf("path: %s", got)
	}
}

func TestTranscriptReadMissingAndRemove(t *testing.T) {
	dir := t.TempDir()
	p := TranscriptPath(dir, "gone")
	if got := ReadTranscript(p, 0); got != nil {
		t.Fatalf("missing file should read nil, got %v", got)
	}
	if err := RemoveTranscript(p); err != nil {
		t.Fatalf("remove missing should be nil, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/internal/state/ -run TestTranscript -v`
Expected: FAIL — `undefined: TranscriptPath` etc.

- [ ] **Step 3: Write minimal implementation**

```go
package state

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

// TranscriptEntry is one recorded turn side. Kept separate from the learning
// call-journal: this is the human-visible conversation, replayed as scrollback.
type TranscriptEntry struct {
	Ts   string  `json:"ts"`
	Role string  `json:"role"` // "user" | "assistant"
	Text string  `json:"text"`
	Cost float64 `json:"cost,omitempty"`
	Kind string  `json:"kind,omitempty"` // reserved (tool calls)
}

// TranscriptPath returns the transcript path for session name under dir
// (dir/transcripts/<name>.jsonl), beside participants/<name>.log.
func TranscriptPath(dir, name string) string {
	return filepath.Join(dir, "transcripts", name+".jsonl")
}

// AppendTranscript appends one JSON-line entry. Best-effort: O_APPEND so the
// daemon's single writer never races a read; a missing parent is created.
func AppendTranscript(path string, e TranscriptEntry) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadTranscript returns entries in file order; when cap > 0, only the last cap.
// A missing file yields nil (best-effort observability, never an error).
func ReadTranscript(path string, cap int) []TranscriptEntry {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []TranscriptEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e TranscriptEntry
		if json.Unmarshal(line, &e) == nil {
			out = append(out, e)
		}
	}
	if cap > 0 && len(out) > cap {
		out = append([]TranscriptEntry(nil), out[len(out)-cap:]...)
	}
	return out
}

// RemoveTranscript deletes the transcript at path. A missing file is not an
// error (called on real session removal to avoid leaking transcripts/*.jsonl).
func RemoveTranscript(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/internal/state/ -run TestTranscript -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w core/internal/state/transcript.go core/internal/state/transcript_test.go
git add core/internal/state/transcript.go core/internal/state/transcript_test.go
git commit -m "feat(state): per-session transcript store (append/read-cap/remove)"
```

---

### Task A2: `Session.Archived` + `SetArchived`

**Files:**
- Modify: `core/internal/state/state.go` (Session struct ~L54; new method after `SetResumeToken` ~L233)
- Test: `core/internal/state/state_test.go` (add test)

**Interfaces:**
- Produces: `state.Session.Archived bool` (`json:"archived,omitempty"`); `func (s *State) SetArchived(name string, archived bool) error` (persist-on-change).

- [ ] **Step 1: Write the failing test**

```go
func TestSetArchivedPersistsOnChange(t *testing.T) {
	dir := t.TempDir()
	st, err := LoadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(Session{Name: "s", ChannelID: "c", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetArchived("s", true); err != nil {
		t.Fatal(err)
	}
	got, _ := st.FindSession("s")
	if !got.Archived {
		t.Fatalf("archived not set")
	}
	// unknown name is a no-op, not an error
	if err := st.SetArchived("nope", true); err != nil {
		t.Fatalf("unknown should be nil, got %v", err)
	}
}
```

Note: confirm `LoadState` is the constructor name used elsewhere in `state_test.go`; match the existing pattern in that file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/internal/state/ -run TestSetArchived -v`
Expected: FAIL — `st.SetArchived undefined` / `got.Archived undefined`

- [ ] **Step 3: Add the field and method**

In the `Session` struct, after the `ResumeToken` block:

```go
	// Archived marks a session closed-but-kept (session archive): its row,
	// transcript and ResumeToken are retained so /resume can revive it, but the
	// boot loop does not auto-supervise it. Absent/false = live as today.
	Archived bool `json:"archived,omitempty"`
```

After `SetResumeToken`:

```go
// SetArchived sets a session's archived flag and persists only on change. An
// unknown name is a no-op (best-effort, mirrors SetResumeToken).
func (s *State) SetArchived(name string, archived bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].Name == name {
			if s.Sessions[i].Archived == archived {
				return nil
			}
			s.Sessions[i].Archived = archived
			return s.saveLocked()
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/internal/state/ -run TestSetArchived -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w core/internal/state/state.go core/internal/state/state_test.go
git add core/internal/state/state.go core/internal/state/state_test.go
git commit -m "feat(state): Session.Archived + SetArchived (persist-on-change)"
```

---

### Task A3: Recorder wired into the turn loop

**Files:**
- Modify: `core/host/turnloop.go` (add `record` field ~L65 area; `RunSession` sig L468; write in `pump` L250 and `awaitTurn` L315)
- Modify: `core/host/hub.go` (`goLive` RunSession call L71)
- Modify: `core/host/turnloop_test.go` (3 `RunSession(...)` calls at L262/L449/L480 — add one trailing `nil`; new recorder test)

**Interfaces:**
- Consumes: `state.TranscriptEntry`, `state.AppendTranscript`, `state.TranscriptPath` (Task A1).
- Produces: `RunSession(..., persistResume func(string), record func(state.TranscriptEntry))` — `record` appended as the last parameter; `sessionDriver.record func(state.TranscriptEntry)`.

- [ ] **Step 1: Write the failing test**

Add to `core/host/turnloop_test.go`:

```go
func TestDriverRecordsTranscript(t *testing.T) {
	var got []state.TranscriptEntry
	d := newSessionDriver("s", nil, make(chan contracts.Event, 8), make(chan contracts.Event, 8))
	d.record = func(e state.TranscriptEntry) { got = append(got, e) }

	// user side: pump fans a human event for an input frame.
	d.recordEntry("user", "hello", 0)
	// assistant side: awaitTurn records on reply{done}.
	d.recordEntry("assistant", "hi there", 0.02)

	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[0].Role != "user" || got[0].Text != "hello" {
		t.Fatalf("user entry wrong: %+v", got[0])
	}
	if got[1].Role != "assistant" || got[1].Cost != 0.02 {
		t.Fatalf("assistant entry wrong: %+v", got[1])
	}
}
```

(`state` is already imported in turnloop_test.go via the host package tests; if not, add `"github.com/Herrscherd/herrscher/core/internal/state"`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/host/ -run TestDriverRecordsTranscript -v`
Expected: FAIL — `d.record undefined`, `d.recordEntry undefined`

- [ ] **Step 3: Implement the field, helper, and call sites**

In the `sessionDriver` struct (after `persistResume`):

```go
	// record appends one transcript entry (nil = disabled, e.g. tests and the
	// operator CLI path). Set by RunSession; the daemon is the single writer.
	record func(state.TranscriptEntry)
```

Add the helper (near `journal`):

```go
// recordEntry appends one transcript turn-side, best-effort. Timestamp is set
// here so both call sites stay one-liners.
func (d *sessionDriver) recordEntry(role, text string, cost float64) {
	if d.record == nil || text == "" {
		return
	}
	d.record(state.TranscriptEntry{
		Ts:   time.Now().UTC().Format(time.RFC3339),
		Role: role,
		Text: text,
		Cost: cost,
	})
}
```

In `pump`, inside the `if ev.T == "input"` block (before/after the `fanOut` of the human event):

```go
			if ev.T == "input" {
				d.recordEntry("user", ev.Text, 0)
				d.fanOut(ctx, contracts.Event{T: "human", Who: ev.Who, Text: ev.Text})
			}
```

In `awaitTurn`, inside the `if e.T == "reply" && e.Done {` block (alongside the existing `persistResume`):

```go
				d.recordEntry("assistant", e.Text, e.Cost)
```

Extend `RunSession`'s signature (append `record`) and set it on the driver:

```go
func RunSession(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, persistResume func(string), record func(state.TranscriptEntry)) {
	...
	d.persistResume = persistResume
	d.record = record
	...
}
```

In `hub.go` `goLive`, update the call:

```go
	go RunSession(sctx, sess.Name, sess.ChannelID, bound, acc, state.ParticipantsPath(h.partDir, sess.Name), h.metrics, h.coordinator,
		func(tok string) { _ = h.st.SetResumeToken(sess.Name, tok) },
		func(e state.TranscriptEntry) { _ = state.AppendTranscript(state.TranscriptPath(h.partDir, sess.Name), e) })
```

Update the 3 test `RunSession(...)` calls in `turnloop_test.go` (L262/L449/L480) to append one trailing `nil`:

```go
	go RunSession(ctx, "pickreg", "", []contracts.GatewaySet{{Gateway: a, Reader: a}}, acc, "", nil, nil, nil, nil)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/host/ -run 'TestDriver|Test' -count=1`
Expected: PASS (all host tests, including the new recorder test and the unchanged RunSession tests)

- [ ] **Step 5: Commit**

```bash
gofmt -w core/host/turnloop.go core/host/hub.go core/host/turnloop_test.go
git add core/host/turnloop.go core/host/hub.go core/host/turnloop_test.go
git commit -m "feat(host): record user+assistant transcript entries per turn"
```

---

### Task A4: Boot loop & reconcile skip archived

**Files:**
- Modify: `core/host/serve.go:242-245` (boot loop)
- Modify: `core/host/hub.go:110-112` (`reconcile`)
- Test: `core/host/hub_test.go` (reconcile skips archived) — match existing hub-test style; if `reconcile` has no direct unit test harness, cover via a serve-level or hub table test that asserts an archived session is not in `h.live`.

**Interfaces:**
- Consumes: `state.Session.Archived` (Task A2).

- [ ] **Step 1: Write the failing test**

Add to `core/host/hub_test.go` (adapt to the existing hub test constructor/helpers in that file):

```go
func TestReconcileSkipsArchived(t *testing.T) {
	h := newTestHub(t) // existing helper; if absent, build a hub as other tests here do
	_ = h.st.AddSession(state.Session{Name: "live", ChannelID: "c1", Type: "text"})
	_ = h.st.AddSession(state.Session{Name: "arch", ChannelID: "c2", Type: "text", Archived: true})
	h.reconcile()
	h.mu.Lock()
	_, liveUp := h.live["live"]
	_, archUp := h.live["arch"]
	h.mu.Unlock()
	if !liveUp {
		t.Fatalf("live session should be up")
	}
	if archUp {
		t.Fatalf("archived session must not be supervised")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/host/ -run TestReconcileSkipsArchived -v`
Expected: FAIL — archived session is brought live.

- [ ] **Step 3: Add the skips**

`serve.go` boot loop:

```go
	for _, sess := range st.SnapshotSessions() {
		if sess.Archived {
			continue
		}
		hb.goLive(sess)
		_ = sup.Start(sess)
	}
```

`hub.go` `reconcile`, in the `for _, s := range persisted` loop:

```go
	for _, s := range persisted {
		if s.Archived {
			continue
		}
		h.goLive(s)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/host/ -run TestReconcileSkipsArchived -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w core/host/serve.go core/host/hub.go core/host/hub_test.go
git add core/host/serve.go core/host/hub.go core/host/hub_test.go
git commit -m "feat(host): boot loop and reconcile skip archived sessions"
```

---

### Task A5: `session archive` verb; `close` purges transcript; status derivation

**Files:**
- Modify: `core/internal/manager/commands.go:28-32` (register `archive` after `close`)
- Modify: `core/internal/manager/session.go` (`sessionCloseRun` L274-299 add transcript purge; new `sessionArchiveRun`; `sessionJSONRow` L55-62 status derivation)
- Test: `core/internal/manager/session_test.go` (archive keeps row+sets flag; close removes transcript; row status)

**Interfaces:**
- Consumes: `state.SetArchived`, `state.RemoveTranscript`, `state.TranscriptPath`, `state.Session.Archived`.
- Produces: `session archive` command; `sessionJSONRow` returns `Status:"archived"` when `s.Archived` else `"running"`.

- [ ] **Step 1: Write the failing test**

Add to `core/internal/manager/session_test.go` (adapt to the file's existing Handler-construction helpers and channelAdmin fakes):

```go
func TestSessionJSONRowStatus(t *testing.T) {
	if r := sessionJSONRow(state.Session{Name: "a"}); r.Status != "running" {
		t.Fatalf("live row status = %q", r.Status)
	}
	if r := sessionJSONRow(state.Session{Name: "a", Archived: true}); r.Status != "archived" {
		t.Fatalf("archived row status = %q", r.Status)
	}
}
```

Plus a behavior test if the file already has a close/archive harness: `archive` leaves the row present with `Archived==true` and the transcript file intact; `close` removes the transcript file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/internal/manager/ -run TestSessionJSONRowStatus -v`
Expected: FAIL — status is always `"running"`.

- [ ] **Step 3: Implement**

`sessionJSONRow` (L61): replace the hardcoded status:

```go
	status := "running"
	if s.Archived {
		status = "archived"
	}
	return sessionJSON{
		Id: s.Name, Name: s.Name, Agent: s.Agent, Project: s.Project, Status: status,
		// ...preserve the rest of the existing literal fields...
	}
```

In `sessionCloseRun`, next to the participants purge (L297) add:

```go
	_ = state.RemoveParticipantJournal(state.ParticipantsPath(h.partDir, name))
	_ = state.RemoveTranscript(state.TranscriptPath(h.partDir, name))
```

Add `sessionArchiveRun` (keep row + transcript + token; stop child; archive channel; set Archived; keep worktree):

```go
func (h *Handler) sessionArchiveRun(ctx context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", fmt.Errorf("no session %q", name)
	}
	_ = h.sup.Stop(name)
	if err := h.adminFor(sess).Archive(ctx, sess.ChannelID); err != nil {
		return "", fmt.Errorf("archive: %w", err)
	}
	if err := h.st.SetArchived(name, true); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return fmt.Sprintf("📦 Session **%s** archived — resume it from /resume.", name), nil
}
```

Register in `commands.go` after the `close` block:

```go
		contracts.New("session", "archive").
			Help("archive a session: stop the bridge and keep it resumable (row + transcript + resume token kept)").
			Param("name", "session name", true).
			Do(h.sessionArchiveRun),
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/internal/manager/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w core/internal/manager/commands.go core/internal/manager/session.go core/internal/manager/session_test.go
git add core/internal/manager/commands.go core/internal/manager/session.go core/internal/manager/session_test.go
git commit -m "feat(manager): session archive verb; close purges transcript; archived status"
```

---

### Task A6: Part A green — full suite + purity

- [ ] **Step 1: Run the whole suite**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS across all packages.

- [ ] **Step 2: Confirm purity**

Run: `go test ./... -run Purity -count=1` and `go test ./core/... -run Purity`
Expected: PASS — no vendor named in core.

- [ ] **Step 3: Add the terminal `archive` verb to the palette allow-list if needed**

`plugins/terminal/terminal.go` `terminalVerbs` already allows the whole `session` group (`terminalVerbs = {"session": true, ...}`), so `session archive` is reachable with no change. Confirm the palette `Commands()` list — if it enumerates verbs explicitly, add `archive` there so it shows in the palette.

Run: `go test ./plugins/terminal/... -count=1`
Expected: PASS

- [ ] **Step 4: Commit any palette tweak**

```bash
gofmt -w plugins/terminal/terminal.go
git add -A && git commit -m "chore(terminal): surface session archive in the palette"
```

---

## PART B — TUI scrollback + `/resume` picker (needs contracts bump)

This half crosses the platform-blind boundary (hub → terminal plugin), which only
`herrscher-contracts` can carry. It therefore requires publishing a new
`herrscher-contracts` version and bumping `go.mod`. Do **not** start Part B until
the operator confirms they want to cut a contracts release.

### Task B1: Extend the contract (herrscher-contracts)

**Repo:** `/home/shan/dev/herrscher-contracts` (separate module; publish + tag).

**Files:**
- Modify: `session_control.go` — enrich `SessionInfo`, add methods to `SessionControl`, add `ScrollbackLine`.

- [ ] **Step 1: Enrich `SessionInfo`**

```go
type SessionInfo struct {
	Name      string
	ChannelID string
	Type      string
	Gateways  []string
	Vendor    string // agent backend vendor (for the picker column)
	Project   string // workspace sub-dir (picker column)
	Archived  bool   // true = closed-but-kept; TUI skips it in syncTabs, shows it in /resume
	Resumable bool   // true = has a resume token (⟲ column)
	LastTs    string // last transcript ts (RFC3339), for picker sort; "" if none
}
```

- [ ] **Step 2: Add the seam types + methods**

```go
// ScrollbackLine is one replayed transcript entry, carried across the seam so a
// gateway (the terminal TUI) can repaint history without reading the state dir.
type ScrollbackLine struct {
	Role string // "user" | "assistant"
	Text string
}
```

Add to `SessionControl`:

```go
	// Scrollback returns the last N transcript lines for a session (empty when
	// none), so a gateway can seed a reopened view before live events arrive.
	Scrollback(name string) []ScrollbackLine
	// Resume revives an archived session (unarchive + bring live). A live session
	// is a no-op success.
	Resume(name string) error
```

- [ ] **Step 3: Build, commit, tag, push**

```bash
cd /home/shan/dev/herrscher-contracts
gofmt -w session_control.go event.go
go build ./...
git add -A && git commit -m "feat(contracts): SessionInfo picker fields + Scrollback/Resume seam"
git tag v0.1.17 && git push && git push --tags
```

Expected: build PASS; tag `v0.1.17` pushed.

### Task B2: Bump herrscher to the new contract

**Files:** Modify `go.mod` / `go.sum`.

- [ ] **Step 1: Bump**

```bash
cd /home/shan/dev/herrscher
go get github.com/Herrscherd/herrscher-contracts@v0.1.17
go mod tidy
go build ./...
```

Expected: resolves to `v0.1.17`, builds (the hub now fails to satisfy `SessionControl` until Task B3 — that is expected; proceed).

- [ ] **Step 2: Commit after B3 compiles** (bump is committed together with B3 so the tree never has a broken `SessionControl`).

### Task B3: Hub implements the new seam + enriched `Sessions()`

**Files:**
- Modify: `core/host/hub.go` — `Sessions()` (L184) fills new fields; add `Scrollback` and `Resume`.
- Test: `core/host/hub_test.go`.

**Interfaces:**
- Consumes: `state` transcript readers, `state.Session.{Vendor,Project,Archived,ResumeToken}`.
- Produces: `hub.Scrollback(name) []contracts.ScrollbackLine`, `hub.Resume(name) error`; enriched `SessionInfo`.

- [ ] **Step 1: Write failing tests** — `Scrollback` maps entries to lines (cap 200); `Resume` clears `Archived` and brings the session live; `Sessions()` sets `Archived`/`Resumable`/`LastTs`.

```go
func TestHubScrollbackAndResume(t *testing.T) {
	h := newTestHub(t)
	_ = h.st.AddSession(state.Session{Name: "s", ChannelID: "c", Type: "text", Archived: true, ResumeToken: "tok"})
	p := state.TranscriptPath(h.partDir, "s")
	_ = state.AppendTranscript(p, state.TranscriptEntry{Ts: "t1", Role: "user", Text: "hi"})
	_ = state.AppendTranscript(p, state.TranscriptEntry{Ts: "t2", Role: "assistant", Text: "yo"})

	lines := h.Scrollback("s")
	if len(lines) != 2 || lines[0].Role != "user" || lines[1].Text != "yo" {
		t.Fatalf("scrollback wrong: %+v", lines)
	}
	if err := h.Resume("s"); err != nil {
		t.Fatal(err)
	}
	got, _ := h.st.FindSession("s")
	if got.Archived {
		t.Fatalf("resume should unarchive")
	}
	h.mu.Lock()
	_, up := h.live["s"]
	h.mu.Unlock()
	if !up {
		t.Fatalf("resume should bring the session live")
	}
}
```

- [ ] **Step 2: Run — FAIL** (`h.Scrollback` / `h.Resume` undefined).

- [ ] **Step 3: Implement**

```go
const scrollbackCap = 200

func (h *hub) Scrollback(name string) []contracts.ScrollbackLine {
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, name), scrollbackCap)
	out := make([]contracts.ScrollbackLine, 0, len(entries))
	for _, e := range entries {
		out = append(out, contracts.ScrollbackLine{Role: e.Role, Text: e.Text})
	}
	return out
}

func (h *hub) Resume(name string) error {
	sess, ok := h.st.FindSession(name)
	if !ok {
		return fmt.Errorf("no session %q", name)
	}
	if sess.Archived {
		if err := h.st.SetArchived(name, false); err != nil {
			return err
		}
		sess.Archived = false
	}
	h.goLive(sess)
	_ = h.sup.Start(sess)
	return nil
}
```

Enrich `Sessions()`:

```go
		last := state.ReadTranscript(state.TranscriptPath(h.partDir, s.Name), 1)
		lastTs := ""
		if len(last) > 0 {
			lastTs = last[0].Ts
		}
		out = append(out, contracts.SessionInfo{
			Name:      s.Name,
			ChannelID: s.ChannelID,
			Type:      s.Type,
			Gateways:  s.BoundGateways(),
			Vendor:    s.Vendor,
			Project:   s.Project,
			Archived:  s.Archived,
			Resumable: s.ResumeToken != "",
			LastTs:    lastTs,
		})
```

Confirm `h.sup` is reachable from the hub (it holds `sup` — see `newHub`). If `sup.Start` is not already a hub field, thread it as the boot loop does.

- [ ] **Step 4: Run — PASS.** `go test ./core/host/ -run TestHubScrollbackAndResume -v`

- [ ] **Step 5: Commit (with the go.mod bump from B2)**

```bash
gofmt -w core/host/hub.go core/host/hub_test.go
git add go.mod go.sum core/host/hub.go core/host/hub_test.go
git commit -m "feat(host): hub Scrollback/Resume seam + enriched SessionInfo (contracts v0.1.17)"
```

### Task B4: Terminal gateway forwards the new seam

**Files:**
- Modify: `plugins/terminal/terminal.go` — add `Scrollback`/`Resume` delegating to `t.ctrl`; `Sessions()` already forwards.
- Modify: `plugins/terminal/tui/tui.go` — `Backend` interface gains `Scrollback(name) []contracts.ScrollbackLine` and `Resume(name) error`.
- Test: `plugins/terminal/terminal_test.go`, `plugins/terminal/tui/tui_test.go` fakes gain the new methods.

- [ ] **Step 1: Write failing test** — a `fakeBackend`/`fakeSessionControl` returning canned scrollback; assert `Terminal.Scrollback` forwards to `ctrl`.

- [ ] **Step 2: Run — FAIL** (method missing).

- [ ] **Step 3: Implement**

`terminal.go`:

```go
func (t *Terminal) Scrollback(name string) []contracts.ScrollbackLine {
	if c := t.Control(); c != nil {
		return c.Scrollback(name)
	}
	return nil
}

func (t *Terminal) Resume(name string) (string, error) {
	c := t.Control()
	if c == nil {
		return "", fmt.Errorf("no session control")
	}
	if err := c.Resume(name); err != nil {
		return "", err
	}
	return "resumed " + name, nil
}
```

`tui.go` `Backend` interface — add:

```go
	Scrollback(name string) []contracts.ScrollbackLine
	Resume(name string) (string, error)
```

Update the `fakeBackend` (tui_test.go L104 area) and `fakeSessionControl` (terminal_test.go L204 area) with the new methods.

- [ ] **Step 4: Run — PASS.** `go test ./plugins/terminal/... -count=1`

- [ ] **Step 5: Commit**

```bash
gofmt -w plugins/terminal/terminal.go plugins/terminal/tui/tui.go plugins/terminal/terminal_test.go plugins/terminal/tui/tui_test.go
git add -A && git commit -m "feat(terminal): forward Scrollback/Resume across the TUI Backend seam"
```

### Task B5: TUI seeds scrollback on tab creation

**Files:**
- Modify: `plugins/terminal/tui/tui.go` — `ensureTab` (L175) seeds `tab.lines` from `Scrollback`; `syncTabs` (L229) skips `Archived` SessionInfo so archived sessions never auto-open a tab.
- Test: `plugins/terminal/tui/tui_test.go`.

- [ ] **Step 1: Write failing test** — opening a tab for a session with scrollback seeds dimmed lines before any live event; a live event then appends.

- [ ] **Step 2: Run — FAIL.**

- [ ] **Step 3: Implement**

In `ensureTab`, after creating the `tab` and before returning, seed once:

```go
	tb := &tab{channel: channel, label: channel}
	for _, sl := range m.tm.Scrollback(m.sessionName(channel)) {
		tb.appendLine(dimStyle.Render(scrollbackText(sl)))
	}
	m.tabs[channel] = tb
```

Where `scrollbackText` renders a line consistent with live `human`/`reply` rendering (reuse the existing renderers' prefix/style helpers in `renderInto`), and `dimStyle` is a lipgloss faint style (match the file's existing style vars). In `syncTabs`, skip archived infos when creating tabs:

```go
	for _, s := range infos {
		if s.Archived {
			continue
		}
		// ...existing tab-creation...
	}
```

- [ ] **Step 4: Run — PASS.** `go test ./plugins/terminal/tui/... -count=1`

- [ ] **Step 5: Commit**

```bash
gofmt -w plugins/terminal/tui/tui.go plugins/terminal/tui/tui_test.go
git add -A && git commit -m "feat(tui): seed scrollback from transcript on tab open; skip archived"
```

### Task B6: `/resume` picker overlay

**Files:**
- Modify: `plugins/terminal/tui/tui.go` — add a picker overlay (model state, key handling, render), triggered by `/resume`; Enter action per session state.
- Test: `plugins/terminal/tui/tui_test.go`.

**Interfaces:**
- Consumes: `Backend.Sessions()` (enriched), `Backend.Resume(name)`, `Backend.Scrollback(name)`.

- [ ] **Step 1: Write failing test** — the picker lists sessions sorted by `LastTs` desc; Enter on an archived row calls `Resume` and opens its tab; Enter on a live-with-tab row focuses it.

- [ ] **Step 2: Run — FAIL.**

- [ ] **Step 3: Implement** — mirror the existing command-palette overlay (`paletteOpen`/`palIdx`/`paletteHeight` at tui.go L122-137): a `resumeOpen bool` + `resumeIdx int` + `resumeRows []contracts.SessionInfo`; `/resume` in `handleEnter` opens it (populate + sort by `LastTs` desc); arrow keys move `resumeIdx`; Enter:
  - archived → `m.tm.Resume(name)`, then `ensureTab(channelID)` + `switch active`,
  - live & tab exists → set `m.active`,
  - live & no tab → `ensureTab` (seeds scrollback via B5).
  Render columns `name · project · lastTs · vendor · [live|archived] · ⟲` when `Resumable`.

- [ ] **Step 4: Run — PASS.** `go test ./plugins/terminal/tui/... -count=1`

- [ ] **Step 5: Commit**

```bash
gofmt -w plugins/terminal/tui/tui.go plugins/terminal/tui/tui_test.go
git add -A && git commit -m "feat(tui): /resume picker — revive archived, open/focus live"
```

### Task B7: Part B green — full suite + manual smoke

- [ ] **Step 1:** `go build ./... && go test ./... -count=1` → PASS.
- [ ] **Step 2:** Purity green.
- [ ] **Step 3: Manual smoke** (README's run recipe): create a terminal session, exchange a couple of turns, `session archive` it, restart the daemon, open `/resume`, revive it → scrollback repaints and the backend resumes. Record the result.

---

## Self-Review (author checklist)

- **Spec coverage:** transcript store (A1), Archived (A2), recorder (A3), boot skip (A4), archive verb + close purge + status (A5), scrollback replay (B4/B5), picker (B6), purity (A6/B7). All spec sections map to a task. The one deviation from the spec is intentional and operator-approved: `close` stays destructive; archive is the new verb `session archive` (spec §"Archive semantics"). The spec's "no contract change" line holds for Part A only — Part B needs the `v0.1.17` bump (B1), called out explicitly.
- **Placeholder scan:** none — every code step carries real code; B5/B6 name the exact existing symbols to mirror (`paletteOpen`, `renderInto`, `ensureTab`).
- **Type consistency:** `TranscriptEntry`/`ScrollbackLine`/`SessionInfo` field names are used identically across tasks; `record func(state.TranscriptEntry)` is the same signature in the struct, `RunSession`, and the `goLive` wiring.
