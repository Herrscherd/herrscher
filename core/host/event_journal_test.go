package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
	"github.com/Herrscherd/herrscher/core/timeline"
)

const journalTestBase = 1000

func journalTexts(events []wireEvent) []string {
	texts := make([]string, len(events))
	for idx := range events {
		texts[idx] = events[idx].T + ":" + events[idx].Text + events[idx].Value
	}
	return texts
}

func sameTexts(got []wireEvent, want ...string) bool {
	return strings.Join(journalTexts(got), "|") == strings.Join(want, "|")
}

func since(seq uint64) *uint64 {
	return &seq
}

func catchUpNow(journal *eventJournal, cursor EventCursor) []wireEvent {
	return journal.catchUp(cursor, journal.current())
}

func TestJournalSeqStartsAboveEveryEarlierDaemon(t *testing.T) {
	before := uint64(time.Now().UnixMicro())
	journal := newEventJournal()
	if first := journal.record("a", contracts.Event{T: eventwire.EventHuman}); first.Seq <= before {
		t.Fatalf("first seq %d must be above the clock at start %d", first.Seq, before)
	}
}

func TestJournalCatchUpReturnsWhatFollowed(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	seen := journal.record("a", contracts.Event{T: eventwire.EventStatus, Text: "Read x"})
	journal.record("b", contracts.Event{T: eventwire.EventStatus, Text: "other"})
	journal.record("a", contracts.Event{T: eventwire.EventReply, Text: "r", Done: true})
	if got := catchUpNow(journal, EventCursor{Session: "a", Since: since(seen.Seq)}); !sameTexts(got, "reply:r", "caught_up:idle") {
		t.Fatalf("since %d = %v", seen.Seq, journalTexts(got))
	}
	if got := catchUpNow(journal, EventCursor{Session: "a", Since: since(journalTestBase)}); len(got) != 4 {
		t.Fatalf("since the daemon start = %v, want the whole ring", journalTexts(got))
	}
}

func TestJournalCatchUpFromAnEarlierDaemonReportsAGap(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	want := []string{"gap:expired", "human:q", "caught_up:running"}
	if got := catchUpNow(journal, EventCursor{Session: "a", Since: since(journalTestBase - 1)}); !sameTexts(got, want...) {
		t.Fatalf("a seq from before a restart = %v, want %v", journalTexts(got), want)
	}
	if got := catchUpNow(journal, EventCursor{Session: "a", Since: since(journalTestBase * 2)}); !sameTexts(got, want...) {
		t.Fatalf("a seq ahead of the daemon = %v, want %v", journalTexts(got), want)
	}
}

func TestJournalCatchUpPastATrimmedRingReportsAGap(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	first := journal.record("a", contracts.Event{T: eventwire.EventStatus, Text: "0"})
	for idx := range journalDepth {
		journal.record("a", contracts.Event{T: eventwire.EventStatus, Text: strconv.Itoa(idx + 1)})
	}
	got := catchUpNow(journal, EventCursor{Session: "a", Since: since(first.Seq - 1)})
	if got[0].T != eventwire.EventGap || got[1].Text != "1" {
		t.Fatalf("a seq trimmed out of the ring = %v", journalTexts(got[:2]))
	}
	got = catchUpNow(journal, EventCursor{Session: "a", Since: since(first.Seq)})
	if got[0].T == eventwire.EventGap {
		t.Fatal("the last trimmed seq leaves nothing missing")
	}
}

func TestJournalMergesChunksWithoutRepeatingSeenText(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	journal.record("a", contracts.Event{T: eventwire.EventChunk, Text: "hel"})
	seen := journal.record("a", contracts.Event{T: eventwire.EventChunk, Text: "lo "})
	upTo := journal.current()
	last := journal.record("a", contracts.Event{T: eventwire.EventChunk, Text: "world"})
	if got := catchUpNow(journal, EventCursor{Session: "a"}); !sameTexts(got, "human:q", "chunk:hello world", "caught_up:running") || got[1].Seq != last.Seq {
		t.Fatalf("current turn = %v", journalTexts(got))
	}
	if got := catchUpNow(journal, EventCursor{Session: "a", Since: since(seen.Seq)}); !sameTexts(got, "chunk:world", "caught_up:running") {
		t.Fatalf("since the middle of a merged chunk = %v", journalTexts(got))
	}
	if got := journal.catchUp(EventCursor{Session: "a"}, upTo); !sameTexts(got, "human:q", "chunk:hello ", "caught_up:running") || got[1].Seq != seen.Seq {
		t.Fatalf("a catch-up bounded before the last chunk = %v", journalTexts(got))
	}
}

func TestJournalCurrentTurnAndItsState(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "old"})
	journal.record("a", contracts.Event{T: eventwire.EventReply, Text: "done", Done: true})
	if got := catchUpNow(journal, EventCursor{Session: "a"}); !sameTexts(got, "human:old", "reply:done", "caught_up:idle") {
		t.Fatalf("a finished turn = %v", journalTexts(got))
	}
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "new"})
	journal.record("a", contracts.Event{T: eventwire.EventStatus, Text: "Bash ls"})
	if got := catchUpNow(journal, EventCursor{Session: "a"}); !sameTexts(got, "human:new", "status:Bash ls", "caught_up:running") {
		t.Fatalf("current turn = %v", journalTexts(got))
	}
	if got := catchUpNow(journal, EventCursor{Session: "none"}); !sameTexts(got, "caught_up:empty") {
		t.Fatalf("unknown session = %v", journalTexts(got))
	}
}

func TestJournalIsBoundedInEventsAndBytes(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	for idx := range journalDepth + 10 {
		journal.record("a", contracts.Event{T: eventwire.EventStatus, Text: strconv.Itoa(idx)})
	}
	held := journal.rings["a"].entries
	if len(held) != journalDepth || held[0].Text != "10" {
		t.Fatalf("ring holds %d events from %q, want %d from \"10\"", len(held), held[0].Text, journalDepth)
	}
	big := strings.Repeat("x", journalChunkBytes)
	for range journalRingBytes/journalChunkBytes + 4 {
		journal.record("big", contracts.Event{T: eventwire.EventChunk, Text: big})
	}
	if ring := journal.rings["big"]; ring.bytes > journalRingBytes || len(ring.entries) < 2 {
		t.Fatalf("ring holds %d bytes in %d entries, cap %d", ring.bytes, len(ring.entries), journalRingBytes)
	}
	for range journalChunkMarks + 1 {
		journal.record("marks", contracts.Event{T: eventwire.EventChunk})
	}
	if entries := journal.rings["marks"].entries; len(entries) != 2 || len(entries[0].marks) != journalChunkMarks {
		t.Fatalf("a merged chunk must stop at %d marks, got %d entries", journalChunkMarks, len(entries))
	}
}

func TestJournalEvictsTheQuietestSessionAndRemembersIt(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	seen := journal.record("a", contracts.Event{T: eventwire.EventStatus})
	for idx := range journalSessions {
		journal.record("s"+strconv.Itoa(idx), contracts.Event{T: eventwire.EventStatus})
	}
	if len(journal.rings) != journalSessions {
		t.Fatalf("journal holds %d sessions, want %d", len(journal.rings), journalSessions)
	}
	if _, kept := journal.rings["a"]; kept {
		t.Fatal("the quietest session must be evicted first")
	}
	if got := catchUpNow(journal, EventCursor{Session: "a", Since: since(seen.Seq - 1)}); !sameTexts(got, "gap:expired", "caught_up:empty") {
		t.Fatalf("an evicted session = %v", journalTexts(got))
	}
}

func TestJournalRenameMovesTheRing(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	journal.record("old", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	journal.rename("old", "new")
	got := catchUpNow(journal, EventCursor{Session: "new"})
	if !sameTexts(got, "human:q", "caught_up:running") || got[0].Session != "new" {
		t.Fatalf("renamed ring = %+v", got)
	}
	if _, kept := journal.rings["old"]; kept {
		t.Fatal("the old name must keep no ring")
	}
}

func TestPublishStampsTheSeqItJournals(t *testing.T) {
	es := newJournaledEventSocket(newEventJournalFrom(journalTestBase))
	defer es.closeAll()
	es.Publish("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	es.Publish("a", contracts.Event{T: eventwire.EventStatus, Text: "x"})
	got := catchUpNow(es.journal, EventCursor{Session: "a", Since: since(journalTestBase)})
	if len(got) != 3 || got[0].Seq != journalTestBase+1 || got[1].Seq != journalTestBase+2 {
		t.Fatalf("journal = %+v", got)
	}
}

type socketReader struct {
	t       *testing.T
	conn    net.Conn
	scanner *bufio.Scanner
}

func subscribeToSocket(t *testing.T, es *eventSocket, cursor *EventCursor) *socketReader {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { logCleanup(client.Close()) })
	es.add(server)
	if cursor != nil {
		line, err := json.Marshal(cursor)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	return &socketReader{t: t, conn: client, scanner: bufio.NewScanner(client)}
}

func (r *socketReader) next() wireEvent {
	r.t.Helper()
	logCleanup(r.conn.SetReadDeadline(time.Now().Add(5 * time.Second)))
	if !r.scanner.Scan() {
		r.t.Fatalf("socket closed: %v", r.scanner.Err())
	}
	var event wireEvent
	if err := json.Unmarshal(r.scanner.Bytes(), &event); err != nil {
		r.t.Fatal(err)
	}
	return event
}

func (r *socketReader) read(count int) []wireEvent {
	r.t.Helper()
	events := make([]wireEvent, count)
	for idx := range events {
		events[idx] = r.next()
	}
	return events
}

func TestEventSocketReplaysTheCursorBeforeLiveEvents(t *testing.T) {
	es := newJournaledEventSocket(newEventJournalFrom(journalTestBase))
	defer es.closeAll()
	seen := es.journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	es.Publish("a", contracts.Event{T: eventwire.EventStatus, Text: "missed"})
	reader := subscribeToSocket(t, es, &EventCursor{Session: "a", Since: since(seen.Seq)})
	es.Publish("a", contracts.Event{T: eventwire.EventStatus, Text: "live"})
	if got := reader.read(3); !sameTexts(got, "status:missed", "caught_up:running", "status:live") {
		t.Fatalf("socket catch-up = %v", journalTexts(got))
	}
}

func TestEventSocketWithoutCursorStreamsLiveOnly(t *testing.T) {
	es := newJournaledEventSocket(newEventJournalFrom(journalTestBase))
	defer es.closeAll()
	es.Publish("a", contracts.Event{T: eventwire.EventStatus, Text: "before"})
	reader := subscribeToSocket(t, es, nil)
	es.Publish("a", contracts.Event{T: eventwire.EventStatus, Text: "live"})
	if got := reader.read(1); !sameTexts(got, "status:live") {
		t.Fatalf("a subscriber without cursor = %v", journalTexts(got))
	}
}

func TestEventSocketMarksDroppedLines(t *testing.T) {
	es := newJournaledEventSocket(newEventJournalFrom(journalTestBase))
	defer es.closeAll()
	reader := subscribeToSocket(t, es, nil)
	time.Sleep(cursorWait * 2)
	for idx := range subscriberBuffer * 2 {
		es.Publish("a", contracts.Event{T: eventwire.EventStatus, Text: strconv.Itoa(idx)})
	}
	for range subscriberBuffer * 2 {
		if event := reader.next(); event.T == eventwire.EventGap {
			if event.Value != eventwire.GapDropped {
				t.Fatalf("gap value = %q", event.Value)
			}
			return
		}
	}
	t.Fatal("a subscriber that lost lines must be told")
}

func TestEventSocketSignalsACursorItCouldNotRead(t *testing.T) {
	es := newJournaledEventSocket(newEventJournalFrom(journalTestBase))
	defer es.closeAll()
	server, client := net.Pipe()
	t.Cleanup(func() { logCleanup(client.Close()) })
	es.add(server)
	if _, err := client.Write([]byte(`{"session":"a"`)); err != nil {
		t.Fatal(err)
	}
	reader := &socketReader{t: t, conn: client, scanner: bufio.NewScanner(client)}
	if event := reader.next(); event.T != eventwire.EventGap || event.Value != eventwire.GapDropped {
		t.Fatalf("a cursor cut by the deadline must be answered by a dropped gap, got %+v", event)
	}
}

func TestEventSocketSignalsACursorThatCameTooLate(t *testing.T) {
	es := newJournaledEventSocket(newEventJournalFrom(journalTestBase))
	defer es.closeAll()
	server, client := net.Pipe()
	t.Cleanup(func() { logCleanup(client.Close()) })
	es.add(server)
	time.Sleep(cursorWait * 2)
	logCleanup(client.SetWriteDeadline(time.Now().Add(5 * time.Second)))
	if _, err := client.Write([]byte(`{"session":"a"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	reader := &socketReader{t: t, conn: client, scanner: bufio.NewScanner(client)}
	if event := reader.next(); event.T != eventwire.EventGap || event.Value != eventwire.GapDropped {
		t.Fatalf("a cursor sent after the deadline must be answered by a dropped gap, got %+v", event)
	}
}

func sessionVerbs(t *testing.T, verbs map[string]func() error) *cli.Registry {
	t.Helper()
	reg := cli.NewRegistry(clitext.NewMessages())
	for verb, change := range verbs {
		if err := reg.Add(contracts.New("session", verb).Do(func(context.Context, contracts.Input) (contracts.Result, error) {
			return contracts.Result{}, change()
		})); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func TestDispatchAnnouncesARenameAndAClose(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	for _, name := range []string{"old", "gone"} {
		if err := st.AddSession(state.Session{Name: name, ChannelID: "c-" + name}); err != nil {
			t.Fatal(err)
		}
	}
	sup := supervisor.NewSupervisor(t.Context(), "/nonexistent/herrscher", testRuntime(t))
	partDir := t.TempDir()
	hdl := newManagerHandler(newHostMessages(), clitext.NewMessages(), Deps{}, Options{DefaultCmd: "claude"}, partDir, st, sup, stoppingWorktrees{}, serviceUpdater{}, agent.NewStore(t.TempDir()), nil)
	reg := sessionVerbs(t, map[string]func() error{"touch": func() error { return nil }})
	for _, cmd := range hdl.Commands() {
		if err := reg.Add(cmd); err != nil {
			t.Fatal(err)
		}
	}
	h := newHub(st, sup, nil, partDir, reg, nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages(), nil, nil))
	h.runtime = isolatedRuntimeDir(t)
	t.Cleanup(h.stopLive)
	events := newJournaledEventSocket(h.eventJournal())
	defer events.closeAll()
	h.eventPublisher = events.Publish
	dispatch := func(args ...string) {
		t.Helper()
		if _, err := h.Dispatch(t.Context(), args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	dispatch("session", "touch")
	events.Publish("old", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	dispatch("session", "rename", "--name", "old", "--to", "new")
	dispatch("session", "archive", "--name", "gone")
	journal := h.eventJournal()
	renamed := journal.catchUp(EventCursor{Session: "new"}, journal.current())
	if !sameTexts(renamed, "human:q", "caught_up:running") {
		t.Fatalf("the renamed session must keep its events, got %v", journalTexts(renamed))
	}
	notice := journal.rings["old"].entries
	if len(notice) != 1 || notice[0].T != eventwire.EventClosed || notice[0].Value != eventwire.ClosedRenamed || notice[0].Text != "new" {
		t.Fatalf("a watcher of the old name must be told the new one, got %+v", notice)
	}
	closed := journal.rings["gone"].entries
	if len(closed) != 1 || closed[0].T != eventwire.EventClosed || closed[0].Value != "" {
		t.Fatalf("an archived session must be announced closed, got %+v", closed)
	}
}

func TestSessionWatchVerb(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "a", ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	journal := newEventJournalFrom(journalTestBase)
	first := journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	journal.record("a", contracts.Event{T: eventwire.EventStatus, Text: "x"})
	verb := sessionWatchVerb(newHostMessages(), st, journal)
	run := func(args map[string]string) ([]wireEvent, error) {
		res, err := verb.Run(context.Background(), contracts.Input{Args: args})
		if err != nil {
			return nil, err
		}
		data, isText := res.Data().(string)
		if !isText {
			t.Fatalf("session watch data = %T, want JSON text", res.Data())
		}
		var events []wireEvent
		if err := json.Unmarshal([]byte(data), &events); err != nil {
			return nil, err
		}
		return events, nil
	}
	if got, err := run(map[string]string{"name": "A"}); err != nil || !sameTexts(got, "human:q", "status:x", "caught_up:running") {
		t.Fatalf("current turn = %v, %v", journalTexts(got), err)
	}
	if got, err := run(map[string]string{"name": "a", "since": strconv.FormatUint(first.Seq, 10)}); err != nil || !sameTexts(got, "status:x", "caught_up:running") {
		t.Fatalf("since the first event = %v, %v", journalTexts(got), err)
	}
	if _, err := run(map[string]string{"name": "a", "since": "abc"}); err == nil {
		t.Fatal("a since that is not a number must be refused")
	}
	var missing *NotFoundError
	if _, err := run(map[string]string{"name": "ghost"}); !errors.As(err, &missing) {
		t.Fatalf("unknown session err = %v, want NotFoundError", err)
	}
}

func TestJournalCostCountsEveryToolCallField(t *testing.T) {
	bare := journalEntry{wireEvent: wireEvent{Event: contracts.Event{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{}}}}
	filled := journalEntry{wireEvent: wireEvent{Event: contracts.Event{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{ID: "id", Tool: "tool", Detail: "detail", Error: "error"}}}}
	want := len("id") + len("tool") + len("detail") + len("error")
	if got := filled.cost() - bare.cost(); got != want {
		t.Fatalf("the tool call's text adds %d to the cost, want %d", got, want)
	}
}

func TestJournalStoresToolDetailAndErrorWithinTheBound(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	huge := strings.Repeat("e", 1<<20)
	for range journalDepth {
		journal.record("tools", contracts.Event{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{ID: "c", Tool: "bash", Detail: huge, Error: huge, State: contracts.ToolCallFailed}})
	}
	ring := journal.rings["tools"]
	if ring.bytes > journalRingBytes {
		t.Fatalf("ring holds %d bytes, cap %d: the tool fields are not counted", ring.bytes, journalRingBytes)
	}
	var counted, held int
	for idx := range ring.entries {
		counted += ring.entries[idx].cost()
		if call := ring.entries[idx].ToolCall; call != nil {
			held += len(call.Detail) + len(call.Error)
		}
	}
	if counted != ring.bytes {
		t.Fatalf("ring accounts %d bytes, its entries cost %d", ring.bytes, counted)
	}
	if held > journalRingBytes {
		t.Fatalf("ring keeps %d bytes of tool detail and error, cap %d", held, journalRingBytes)
	}
	last := ring.entries[len(ring.entries)-1].ToolCall
	if len(last.Error) != timeline.MaxFieldBytes || len(last.Detail) != timeline.MaxFieldBytes {
		t.Fatalf("stored detail %d and error %d bytes, want both cut to %d", len(last.Detail), len(last.Error), timeline.MaxFieldBytes)
	}
}

func recordToolErrorOfAMebibyte(journal *eventJournal) {
	huge := strings.Repeat("e", 1<<20)
	journal.record("tools", contracts.Event{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{ID: "c", Tool: "bash", Error: huge, State: contracts.ToolCallFailed}})
}

func TestJournalReleasesTheToolTextItCuts(t *testing.T) {
	journal := newEventJournalFrom(journalTestBase)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range 128 {
		recordToolErrorOfAMebibyte(journal)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	const margin = 32 << 20
	if retained := int64(after.HeapAlloc) - int64(before.HeapAlloc); retained > margin {
		t.Fatalf("the journal still holds %d bytes after GC for 128 errors of 1 MiB, want under %d: the cut keeps the whole error alive", retained, margin)
	}
	runtime.KeepAlive(journal)
}
