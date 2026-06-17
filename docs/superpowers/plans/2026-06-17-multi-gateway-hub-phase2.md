# Multi-gateway hub — Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the bridge a structured turn-event vocabulary — a JSON-lines `Event` wire format, the per-message turn handler extracted into a testable `runner`, and `human`/`status`/`chunk`/`reply` events emitted through an injectable sink — so a later consumer (the TUI hub) can render a real conversation instead of raw logs. Zero behavior change: Discord posting is untouched and the sink is nil in this phase.

**Architecture:** Add `control.Event` (the wire type both sides will use) plus a JSON-lines codec that tolerates the legacy bare-value pick line. Extract the body of `bridge.Run`'s per-message loop into a `runner` struct with a `handle(ctx, Message)` method (pure refactor, full suite stays green). Then thread an `EventSink` through the runner and emit a `human` event when a turn starts, `chunk`/`status` events from backend `onEvent`, and a `reply` event when the answer is posted. The sink is `nil` for now, so Discord behavior is identical.

**Tech Stack:** Go (stdlib only — `testing`, `encoding/json`, `bufio`, `io`), the existing `github.com/Herrscherd/herrscher-contracts` backend/gateway/channel ports, and the in-repo `core/internal/control` + `core/bridge` packages.

Spec: `docs/superpowers/specs/2026-06-16-serve-tui-multi-gateway-hub-design.md` (the "Transcript = structured turn events" and "Session bus protocol" sections).

## Scope boundary (read first)

The spec lists, under Phase 2, both "bridge emits turn events" **and** the full bidirectional socket transport + bridge-as-pure-runner (gateway I/O relocated to the hub). This plan deliberately implements only the parts that are **independently valuable and testable today**:

- ✅ The `Event` wire format (codec).
- ✅ The bridge turn handler extracted into a `runner` (the "runner" refactor, non-regression).
- ✅ Structured turn-event **emission** through an injectable `EventSink` (nil today).

Deferred to **Phase 3** (because they are coupled to the consumer that does not exist yet, and building them now would be speculative / untestable):

- ⛔ The persistent bidirectional socket transport (`control.Dial`, a long-lived `Server` connection carrying events both ways).
- ⛔ Serializing socket-injected `input`/`pick` into the turn queue.
- ⛔ Supervisor passing `--control-socket`, the daemon-side reader, and gateway fan-out.

The existing one-shot pick path (`control.Send` / `Server.Values()` / the `ControlSocket` listener in `Run`) is **left exactly as-is** — this plan does not touch it.

---

## File Structure

- `core/internal/control/event.go` (new) — the `Event` struct + `WriteEvent` / `ScanEvents` JSON-lines codec (with legacy bare-value tolerance).
- `core/internal/control/event_test.go` (new) — codec round-trip, legacy bare value, multi-line order, blank-line skipping.
- `core/bridge/bridge.go` (modify) — extract the per-message loop body into a `runner` struct + `handle` method (Task 2), then add `EventSink` + emission (Task 3). `Run` gains one `sink EventSink` parameter.
- `core/bridge/runner_test.go` (new) — turn-handler test with fakes (Task 3): a fake backend that emits a text event and returns a reply, asserting the emitted `human`/`chunk`/`reply` events and that the gateway recorded the post.
- `bridge.go` (repo root, modify) — `runBridge` passes `nil` for the new `Run` sink parameter (Task 2).

---

## Task 1: `control.Event` wire format + JSON-lines codec

**Files:**
- Create: `core/internal/control/event.go`
- Test: `core/internal/control/event_test.go`

- [ ] **Step 1: Write the failing test**

Create `core/internal/control/event_test.go`:

```go
package control

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteThenScanRoundTrip(t *testing.T) {
	want := []Event{
		{T: "human", Who: "alice", Text: "refactor the env loader"},
		{T: "status", Text: "reading envfile.go"},
		{T: "chunk", Text: "proposing 3 changes"},
		{T: "reply", Text: "done", Done: true},
		{T: "pick", Value: "2"},
	}
	var buf bytes.Buffer
	for _, e := range want {
		if err := WriteEvent(&buf, e); err != nil {
			t.Fatalf("WriteEvent(%v): %v", e, err)
		}
	}
	var got []Event
	if err := ScanEvents(&buf, func(e Event) error { got = append(got, e); return nil }); err != nil {
		t.Fatalf("ScanEvents: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestWriteEventOneLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	_ = WriteEvent(&buf, Event{T: "chunk", Text: "a\nb"}) // text with an embedded newline
	if n := strings.Count(buf.String(), "\n"); n != 1 {
		t.Fatalf("encoded form has %d newlines, want exactly 1 (text newline must be escaped)", n)
	}
}

func TestScanLegacyBareValueBecomesPick(t *testing.T) {
	r := strings.NewReader("2\n")
	var got []Event
	if err := ScanEvents(r, func(e Event) error { got = append(got, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].T != "pick" || got[0].Value != "2" {
		t.Fatalf("legacy bare value decoded as %+v, want a single pick with value 2", got)
	}
}

func TestScanSkipsBlankLines(t *testing.T) {
	r := strings.NewReader("\n  \n{\"t\":\"pick\",\"value\":\"1\"}\n\n")
	count := 0
	if err := ScanEvents(r, func(Event) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("decoded %d events, want 1 (blank lines skipped)", count)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/control/ -run 'TestWrite|TestScan'`
Expected: FAIL — `undefined: Event` / `undefined: WriteEvent` / `undefined: ScanEvents` (compile error).

- [ ] **Step 3: Write the codec**

Create `core/internal/control/event.go`:

```go
package control

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// Event is one message on the session bus. The bridge emits turn events
// (human/status/chunk/reply) for a consumer to render; a consumer injects
// input/pick down to the bridge. One Event encodes to exactly one JSON line.
//
//	{"t":"human","who":"alice","text":"refactor the env loader"}
//	{"t":"chunk","text":"reading envfile.go"}
//	{"t":"status","text":"proposing 3 changes"}
//	{"t":"reply","text":"done","done":true}
//	{"t":"input","who":"terminal","text":"apply them"}
//	{"t":"pick","value":"2"}
type Event struct {
	T     string `json:"t"`
	Who   string `json:"who,omitempty"`
	Text  string `json:"text,omitempty"`
	Value string `json:"value,omitempty"`
	Done  bool   `json:"done,omitempty"`
}

// WriteEvent encodes e as a single JSON line (newline-terminated). encoding/json
// escapes any newline inside a field, so one Event is always one line.
func WriteEvent(w io.Writer, e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// ScanEvents reads JSON-line events from r, calling fn for each. A blank line is
// skipped; a non-JSON line is treated as the legacy bare pick value (one digit
// per connection) and surfaced as Event{T:"pick", Value:line} for back-compat
// with the pre-bus daemon. It returns the first error from fn or a read error
// other than io.EOF.
func ScanEvents(r io.Reader, fn func(Event) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Event
		if line[0] == '{' && json.Unmarshal([]byte(line), &e) == nil {
			if err := fn(e); err != nil {
				return err
			}
			continue
		}
		if err := fn(Event{T: "pick", Value: line}); err != nil {
			return err
		}
	}
	return sc.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/control/ -run 'TestWrite|TestScan' -v`
Expected: PASS (4 tests). Also run the whole package to confirm the existing socket tests still pass: `go test ./core/internal/control/` → PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/internal/control/event.go core/internal/control/event_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -q -m "feat(control): Event wire type + JSON-lines codec (legacy bare-value tolerant)"
```

---

## Task 2: Extract the bridge turn handler into a `runner` (non-regression refactor)

**Files:**
- Modify: `core/bridge/bridge.go` — the `Run` function (currently `core/bridge/bridge.go:62-236`)
- Modify: `bridge.go` (repo root) — `runBridge`'s call to `bridge.Run`

This is a pure refactor: move the per-message loop body into a method so it can be unit-tested and (in Task 3) emit events. No behavior changes. The full test suite is the gate.

- [ ] **Step 1: Add the `runner` struct and `handle` method**

In `core/bridge/bridge.go`, add the following near the top of the file (after the `Options` struct definition, before `Run`). It holds everything the per-message work needs:

```go
// runner carries the per-session state the turn handler needs. Run builds one
// and calls handle for each inbound message; extracting it keeps Run a thin
// loop and makes a single turn unit-testable.
type runner struct {
	p    contracts.ChannelReader
	gw   contracts.Gateway
	resp contracts.Backend
	orch contracts.Orchestrator
	conv contracts.Conversation
	ch   string
	o    Options
	seen map[string]bool
}

// handle processes one inbound message: acknowledge, run the backend turn, post
// the reply, and record the turn. It is a no-op for bot messages (including our
// own) so the bridge never answers itself.
func (r *runner) handle(ctx context.Context, m contracts.Message) {
	if m.AuthorBot {
		return // never answer a bot (incl. ourselves) → no loops
	}
	if !r.seen[m.AuthorID] {
		r.seen[m.AuthorID] = true
		recordParticipant(r.o.Participants, m.AuthorID)
	}
	logf(r.o.Verbose, "<%s> %s", m.AuthorName, oneline(m.Content))

	// Pull any image attachments down to local files so the backend can
	// reference them. Best-effort: a download failure never drops a turn.
	var atts []string
	if len(m.Attachments) > 0 {
		var derr error
		atts, derr = downloadImages(ctx, nil, m, attachmentDir(r.o.Session))
		if derr != nil {
			logf(r.o.Verbose, "attachment download error: %v", derr)
		}
	}
	// Acknowledge immediately so the human sees the message was picked up while
	// the (slow) command runs. Best-effort: ignore if we lack Add Reactions.
	_ = r.gw.React(ctx, r.conv, contracts.MessageID(m.ID), ackEmoji)

	var pv *progressView
	var onEvent func(contracts.BackendEvent)
	if r.o.Progress != "off" {
		post := func(id, content string) (string, error) {
			return r.p.UpsertStatusMessage(ctx, r.ch, id, content)
		}
		pv = newProgressView(post, r.o.Progress, r.o.ProgressKeep, time.Now())
		onEvent = pv.add
	}

	var memCtx string
	if r.orch != nil {
		memCtx = r.orch.Context(ctx)
	}
	prompt := contracts.Prompt{
		Content:     m.Content,
		Context:     memCtx,
		Author:      m.AuthorName,
		MessageID:   m.ID,
		ChannelID:   m.ChannelID,
		Attachments: atts,
	}
	out, err := r.resp.Respond(ctx, prompt, onEvent)
	// The backend has read the files during the (now-finished) turn, so they can
	// go. Keeping them would slowly fill the temp dir.
	removeFiles(atts)
	if err != nil && out == "" {
		out = "⚠️ " + err.Error()
	}
	out = strings.TrimSpace(out)
	if out == "" {
		if pv != nil {
			pv.finish(true)
		}
		_ = r.p.Unreact(ctx, r.ch, m.ID, ackEmoji)
		_ = r.gw.React(ctx, r.conv, contracts.MessageID(m.ID), failEmoji)
		return
	}
	postResult(ctx, r.p, r.gw, r.conv, m.ID, out, r.resp, r.o)
	if r.orch != nil {
		if rerr := r.orch.Observe(ctx, prompt, out); rerr != nil {
			logf(r.o.Verbose, "memory record error: %v", rerr) // best-effort: never break the loop
		}
	}
	if pv != nil {
		pv.finish(err != nil)
	}
	// Swap the "seen" mark for a "done" mark once the answer is posted.
	_ = r.p.Unreact(ctx, r.ch, m.ID, ackEmoji)
	_ = r.gw.React(ctx, r.conv, contracts.MessageID(m.ID), doneEmoji)
}
```

- [ ] **Step 2: Rewrite `Run`'s loop to build and use the runner**

In `core/bridge/bridge.go`, change the `Run` signature to accept a sink parameter (used in Task 3; ignored here) and replace the inline per-message loop body with a call to `r.handle`.

Change the signature from:
```go
func Run(ctx context.Context, p contracts.ChannelReader, gw contracts.Gateway, newBackend BackendFactory, orch contracts.Orchestrator, o Options) error {
```
to:
```go
func Run(ctx context.Context, p contracts.ChannelReader, gw contracts.Gateway, newBackend BackendFactory, orch contracts.Orchestrator, sink EventSink, o Options) error {
```

Add the `EventSink` definition just above `Run` (the interface is used in Task 3; defining it here keeps the signature stable across both tasks):
```go
// EventSink receives structured turn events for an out-of-band consumer (the
// TUI hub, in a later phase). Discord posting is independent of the sink; a nil
// sink simply disables emission, so the bridge behaves exactly as before.
type EventSink interface {
	Emit(control.Event)
}
```

Then, after the existing `resp, err := newBackend(ch)` / `defer resp.Close()` lines and the control-socket block (leave those untouched), build the runner and replace the `for { ... }` loop body. The existing loop is:
```go
	// Authors already journaled this run; skip the dedup-read for repeats.
	seen := map[string]bool{}

	for {
		msgs, err := p.Read(ctx, ch, 100, last)
		if err != nil {
			logf(true, "read error: %v", err)
			time.Sleep(time.Duration(o.Interval) * time.Second)
			continue
		}
		for _, m := range msgs {
			last = m.ID
			persist(o.State, last)
			if m.AuthorBot {
				continue // never answer a bot (incl. ourselves) → no loops
			}
			// ... (everything that is now in handle) ...
		}
		time.Sleep(time.Duration(o.Interval) * time.Second)
	}
```
Replace the whole block (from `seen := map[string]bool{}` through the closing of the outer `for`) with:
```go
	r := &runner{
		p:    p,
		gw:   gw,
		resp: resp,
		orch: orch,
		conv: conv,
		ch:   ch,
		o:    o,
		seen: map[string]bool{}, // authors journaled this run; skip the dedup-read for repeats
	}

	for {
		msgs, err := p.Read(ctx, ch, 100, last)
		if err != nil {
			logf(true, "read error: %v", err)
			time.Sleep(time.Duration(o.Interval) * time.Second)
			continue
		}
		for _, m := range msgs {
			last = m.ID
			persist(o.State, last)
			r.handle(ctx, m)
		}
		time.Sleep(time.Duration(o.Interval) * time.Second)
	}
```

Note: `sink` is intentionally unused in this task. To avoid an "unused parameter" issue (parameters are never flagged by the Go compiler, so this compiles fine) it is simply carried until Task 3. Do **not** add a blank `_ = sink` — unused function parameters are legal in Go.

- [ ] **Step 3: Update the caller in repo-root `bridge.go`**

In `bridge.go` (repo root), the `runBridge` function ends with a call to `bridge.Run(...)`. Add the new `nil` sink argument in the correct position (after `orch`, before the `bridge.Options{...}` literal):

Change:
```go
	return bridge.Run(ctx, set.Reader, contracts.Degrade(set.Gateway), newBackend, orch, bridge.Options{
```
to:
```go
	return bridge.Run(ctx, set.Reader, contracts.Degrade(set.Gateway), newBackend, orch, nil, bridge.Options{
```

- [ ] **Step 4: Build and run the full suite (non-regression)**

Run: `cd /home/shan/dev/herrscher && go build ./... && go test ./...`
Expected: PASS across all packages (the existing `core/bridge` tests — `TestPostResultEmitsViaGateway`, `TestRecordParticipantAppends`, `TestRecordParticipantEmptyPathNoop` — and everything else, including `TestCorePurity`/`TestHostPurity`). No behavior change.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/bridge/bridge.go bridge.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -q -m "refactor(bridge): extract per-message turn handler into runner (no behavior change)"
```

---

## Task 3: Emit structured turn events through the sink

**Files:**
- Modify: `core/bridge/bridge.go` — add emit helpers + emission calls in `handle`; wire the sink into the runner
- Test: `core/bridge/runner_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `core/bridge/runner_test.go`. It drives a single turn through `runner.handle` with fakes and asserts the emitted events and the gateway post. The fakes implement only the methods `handle` touches when `Progress == "off"` (no progress view → no `UpsertStatusMessage`).

```go
package bridge

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

// fakeSink collects emitted events in order.
type fakeSink struct{ events []control.Event }

func (s *fakeSink) Emit(e control.Event) { s.events = append(s.events, e) }

// fakeBackend emits one text event then returns a fixed reply.
type fakeBackend struct{ reply string }

func (b fakeBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	if onEvent != nil {
		onEvent(contracts.BackendEvent{Kind: "text", Detail: "thinking"})
	}
	return b.reply, nil
}
func (fakeBackend) Close() error { return nil }

// fakeGateway records posts/replies; the other methods are no-ops.
type fakeGateway struct{ posts []string }

func (fakeGateway) Manifest() contracts.Manifest { return contracts.Manifest{} }
func (g *fakeGateway) Post(_ context.Context, _ contracts.Conversation, text string) (contracts.MessageID, error) {
	g.posts = append(g.posts, text)
	return "1", nil
}
func (g *fakeGateway) Reply(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, text string) (contracts.MessageID, error) {
	g.posts = append(g.posts, text)
	return "1", nil
}
func (*fakeGateway) React(context.Context, contracts.Conversation, contracts.MessageID, string) error {
	return nil
}
func (*fakeGateway) Menu(context.Context, contracts.Conversation, contracts.MessageID, string, []contracts.Choice) error {
	return nil
}

// fakeReader is a ChannelReader whose methods handle never reaches when
// Progress is "off" (no UpsertStatusMessage) and there are no attachments.
type fakeReader struct{}

func (fakeReader) Enabled() bool          { return true }
func (fakeReader) DefaultChannel() string { return "c1" }
func (fakeReader) EnsureChannel(context.Context, string, string) (contracts.Channel, error) {
	return contracts.Channel{}, nil
}
func (fakeReader) Read(context.Context, string, int, string) ([]contracts.Message, error) {
	return nil, nil
}
func (fakeReader) Unreact(context.Context, string, string, string) error { return nil }
func (fakeReader) UpsertStatusMessage(context.Context, string, string, string) (string, error) {
	return "", nil
}

func TestHandleEmitsTurnEvents(t *testing.T) {
	gw := &fakeGateway{}
	sink := &fakeSink{}
	r := &runner{
		p:    fakeReader{},
		gw:   gw,
		resp: fakeBackend{reply: "done · 4 files changed"},
		conv: contracts.Conversation{Gateway: "discord", ID: "c1"},
		ch:   "c1",
		o:    Options{Progress: "off"},
		seen: map[string]bool{},
		sink: sink,
	}

	r.handle(context.Background(), contracts.Message{
		ID:         "m1",
		ChannelID:  "c1",
		Content:    "refactor the env loader",
		AuthorID:   "u1",
		AuthorName: "alice",
	})

	// Reply must have been posted to the gateway.
	if len(gw.posts) != 1 || gw.posts[0] != "done · 4 files changed" {
		t.Fatalf("gateway posts = %v, want one reply", gw.posts)
	}
	// Events in order: human, chunk (from the text event), reply(done).
	want := []control.Event{
		{T: "human", Who: "alice", Text: "refactor the env loader"},
		{T: "chunk", Text: "thinking"},
		{T: "reply", Text: "done · 4 files changed", Done: true},
	}
	if len(sink.events) != len(want) {
		t.Fatalf("emitted %d events, want %d: %+v", len(sink.events), len(want), sink.events)
	}
	for i := range want {
		if sink.events[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, sink.events[i], want[i])
		}
	}
}

func TestHandleNilSinkNoPanic(t *testing.T) {
	gw := &fakeGateway{}
	r := &runner{
		p:    fakeReader{},
		gw:   gw,
		resp: fakeBackend{reply: "ok"},
		conv: contracts.Conversation{Gateway: "discord", ID: "c1"},
		ch:   "c1",
		o:    Options{Progress: "off"},
		seen: map[string]bool{},
		sink: nil, // emission disabled
	}
	r.handle(context.Background(), contracts.Message{ID: "m1", ChannelID: "c1", Content: "hi", AuthorID: "u1", AuthorName: "bob"})
	if len(gw.posts) != 1 {
		t.Fatalf("gateway posts = %v, want one reply even with nil sink", gw.posts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/bridge/ -run TestHandle`
Expected: FAIL — `unknown field 'sink' in struct literal of type runner` (compile error; the field does not exist yet).

- [ ] **Step 3: Add the sink field, emit helpers, and emission calls**

In `core/bridge/bridge.go`:

1. Add a `sink EventSink` field to the `runner` struct (after `seen`):
```go
type runner struct {
	p    contracts.ChannelReader
	gw   contracts.Gateway
	resp contracts.Backend
	orch contracts.Orchestrator
	conv contracts.Conversation
	ch   string
	o    Options
	seen map[string]bool
	sink EventSink
}
```

2. Add two helper methods (anywhere after the `runner` type, e.g. right before `handle`):
```go
// emit forwards e to the sink when one is configured; a nil sink is a no-op.
func (r *runner) emit(e control.Event) {
	if r.sink != nil {
		r.sink.Emit(e)
	}
}

// emitBackend maps a backend progress event onto the bus vocabulary: assistant
// text becomes a chunk, a tool invocation becomes a status line. Other kinds
// (result/reset) carry no transcript text and are dropped.
func (r *runner) emitBackend(ev contracts.BackendEvent) {
	switch ev.Kind {
	case "text":
		r.emit(control.Event{T: "chunk", Text: ev.Detail})
	case "tool":
		r.emit(control.Event{T: "status", Text: strings.TrimSpace(ev.Tool + " " + ev.Detail)})
	}
}
```

3. In `handle`, emit a `human` event right after the bot guard / participant bookkeeping — place it immediately before the attachment download block:
```go
	logf(r.o.Verbose, "<%s> %s", m.AuthorName, oneline(m.Content))
	r.emit(control.Event{T: "human", Who: m.AuthorName, Text: m.Content})
```

4. In `handle`, make `onEvent` always tap the sink (even when the progress view is off, so chunk/status still emit). Replace the progress-view block:
```go
	var pv *progressView
	var onEvent func(contracts.BackendEvent)
	if r.o.Progress != "off" {
		post := func(id, content string) (string, error) {
			return r.p.UpsertStatusMessage(ctx, r.ch, id, content)
		}
		pv = newProgressView(post, r.o.Progress, r.o.ProgressKeep, time.Now())
		onEvent = pv.add
	}
```
with:
```go
	var pv *progressView
	if r.o.Progress != "off" {
		post := func(id, content string) (string, error) {
			return r.p.UpsertStatusMessage(ctx, r.ch, id, content)
		}
		pv = newProgressView(post, r.o.Progress, r.o.ProgressKeep, time.Now())
	}
	onEvent := func(ev contracts.BackendEvent) {
		if pv != nil {
			pv.add(ev)
		}
		r.emitBackend(ev)
	}
```
(`onEvent` is now always non-nil; `Respond` calling a do-nothing callback when both `pv` and `sink` are absent is harmless.)

5. In `handle`, emit the `reply` event right after the successful `postResult` call:
```go
	postResult(ctx, r.p, r.gw, r.conv, m.ID, out, r.resp, r.o)
	r.emit(control.Event{T: "reply", Text: out, Done: true})
```

6. Wire the sink into the runner built in `Run`. In the `r := &runner{...}` literal added in Task 2, add `sink: sink,`:
```go
	r := &runner{
		p:    p,
		gw:   gw,
		resp: resp,
		orch: orch,
		conv: conv,
		ch:   ch,
		o:    o,
		seen: map[string]bool{},
		sink: sink,
	}
```

7. Add the `control` import if it is not already present. `core/bridge/bridge.go` already imports `"github.com/Herrscherd/herrscher/core/internal/control"` (used by the existing `control.Listen` call), so no import change is needed — verify with `go build`.

- [ ] **Step 4: Run the new test, then the full suite**

Run: `cd /home/shan/dev/herrscher && go test ./core/bridge/ -run TestHandle -v`
Expected: PASS (`TestHandleEmitsTurnEvents`, `TestHandleNilSinkNoPanic`).

Run: `cd /home/shan/dev/herrscher && go build ./... && go test ./...`
Expected: PASS everywhere, including `TestCorePurity`/`TestHostPurity` and the pre-existing `core/bridge` tests. Discord behavior is unchanged (the live bridge passes `nil` for the sink).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/bridge/bridge.go core/bridge/runner_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -q -m "feat(bridge): emit human/status/chunk/reply turn events via injectable sink"
```

---

## Done criteria (Phase 2)

- `go test ./...` green, including `TestCorePurity` / `TestHostPurity` and the existing `core/bridge` + `core/internal/control` tests.
- `control.Event` + a JSON-lines codec exist and round-trip, tolerating the legacy bare-value pick line.
- The bridge's per-message work lives in a `runner.handle` method (thin `Run` loop).
- A turn emits `human` → `status`/`chunk` → `reply(done)` events through an injectable `EventSink`; with a `nil` sink (today's live path) behavior is byte-for-byte unchanged and Discord posting is untouched.
- No user-visible behavior change yet. Deferred to **Phase 3** (with the consumer that needs them): the persistent bidirectional socket transport (`control.Dial` + long-lived `Server` conn), serializing injected `input`/`pick` into the turn queue, supervisor `--control-socket` wiring, and gateway fan-out — plus the terminal gateway + in-process TUI that consume this `EventSink`.
