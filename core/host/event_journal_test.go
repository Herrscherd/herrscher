package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
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

func TestReconcileAnnouncesACloseAndARename(t *testing.T) {
	var published []wireEvent
	journal := newEventJournalFrom(journalTestBase)
	journal.record("old", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	h := &hub{sessions: &sessionTable{journal: journal}, eventPublisher: func(session string, event contracts.Event) {
		published = append(published, wireEvent{Event: event, Session: session})
	}}
	persisted := map[string]state.Session{"new": {Name: "new", Incarnation: "inc1"}, "other": {Name: "other", Incarnation: "inc2", Archived: true}}
	h.announceClosed("old", renamedTo("old", "inc1", persisted))
	h.announceClosed("gone", renamedTo("gone", "inc2", persisted))
	if len(published) != 2 || published[0].Value != eventwire.ClosedRenamed || published[0].Text != "new" || published[1].Value != "" || published[1].T != eventwire.EventClosed {
		t.Fatalf("published = %+v", published)
	}
	if _, moved := journal.rings["new"]; !moved {
		t.Fatal("a rename must move the ring to the new name")
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
