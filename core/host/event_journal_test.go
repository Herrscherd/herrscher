package host

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func journalTexts(events []wireEvent) []string {
	texts := make([]string, len(events))
	for idx := range events {
		texts[idx] = events[idx].T + ":" + events[idx].Text
	}
	return texts
}

func sameTexts(got []wireEvent, want ...string) bool {
	texts := journalTexts(got)
	if len(texts) != len(want) {
		return false
	}
	for idx := range want {
		if texts[idx] != want[idx] {
			return false
		}
	}
	return true
}

func TestJournalSinceReturnsWhatFollowed(t *testing.T) {
	journal := newEventJournal()
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	seen := journal.record("a", contracts.Event{T: eventwire.EventStatus, Text: "Read x"})
	journal.record("b", contracts.Event{T: eventwire.EventStatus, Text: "other"})
	journal.record("a", contracts.Event{T: eventwire.EventReply, Text: "r", Done: true})
	if got := journal.since("a", seen.Seq); !sameTexts(got, "reply:r") {
		t.Fatalf("since %d = %v", seen.Seq, journalTexts(got))
	}
	if got := journal.since("a", 0); len(got) != 3 {
		t.Fatalf("since 0 = %v, want the whole ring", journalTexts(got))
	}
}

func TestJournalSinceAheadOfTheDaemonReplaysTheRing(t *testing.T) {
	journal := newEventJournal()
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	if got := journal.since("a", 9000); !sameTexts(got, "human:q") {
		t.Fatalf("a seq from before a restart = %v, want the whole ring", journalTexts(got))
	}
}

func TestJournalMergesChunksWithoutRepeatingSeenText(t *testing.T) {
	journal := newEventJournal()
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	journal.record("a", contracts.Event{T: eventwire.EventChunk, Text: "hel"})
	seen := journal.record("a", contracts.Event{T: eventwire.EventChunk, Text: "lo "})
	last := journal.record("a", contracts.Event{T: eventwire.EventChunk, Text: "world"})
	if got := journal.currentTurn("a"); !sameTexts(got, "human:q", "chunk:hello world") || got[1].Seq != last.Seq {
		t.Fatalf("current turn = %v", journalTexts(got))
	}
	if got := journal.since("a", seen.Seq); !sameTexts(got, "chunk:world") {
		t.Fatalf("since the middle of a merged chunk = %v", journalTexts(got))
	}
}

func TestJournalCurrentTurnStartsAtTheLastHuman(t *testing.T) {
	journal := newEventJournal()
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "old"})
	journal.record("a", contracts.Event{T: eventwire.EventReply, Text: "done", Done: true})
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "new"})
	journal.record("a", contracts.Event{T: eventwire.EventStatus, Text: "Bash ls"})
	if got := journal.currentTurn("a"); !sameTexts(got, "human:new", "status:Bash ls") {
		t.Fatalf("current turn = %v", journalTexts(got))
	}
	if got := journal.currentTurn("none"); len(got) != 0 {
		t.Fatalf("unknown session = %v", journalTexts(got))
	}
}

func TestJournalIsBounded(t *testing.T) {
	journal := newEventJournal()
	for idx := range journalDepth + 10 {
		journal.record("a", contracts.Event{T: eventwire.EventStatus, Text: strconv.Itoa(idx)})
	}
	got := journal.since("a", 0)
	if len(got) != journalDepth || got[0].Text != "10" {
		t.Fatalf("ring holds %d events from %q, want %d from \"10\"", len(got), got[0].Text, journalDepth)
	}
	for idx := range journalSessions + 1 {
		journal.record("s"+strconv.Itoa(idx), contracts.Event{T: eventwire.EventStatus})
	}
	if len(journal.rings) != journalSessions {
		t.Fatalf("journal holds %d sessions, want %d", len(journal.rings), journalSessions)
	}
	if _, kept := journal.rings["a"]; kept {
		t.Fatal("the quietest session must be evicted first")
	}
}

func TestPublishStampsTheSeqItJournals(t *testing.T) {
	es := newEventSocket()
	defer es.closeAll()
	es.Publish("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
	es.Publish("a", contracts.Event{T: eventwire.EventStatus, Text: "x"})
	got := es.journal.since("a", 0)
	if len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("journal = %+v", got)
	}
	line, err := marshalWireEvent(got[1])
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Seq uint64 `json:"seq"`
	}
	if err := json.Unmarshal(line, &wire); err != nil || wire.Seq != 2 {
		t.Fatalf("wire seq = %d, %v", wire.Seq, err)
	}
}

func TestSessionWatchVerb(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "a", ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	journal := newEventJournal()
	journal.record("a", contracts.Event{T: eventwire.EventHuman, Text: "q"})
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
	if got, err := run(map[string]string{"name": "a"}); err != nil || !sameTexts(got, "human:q", "status:x") {
		t.Fatalf("current turn = %v, %v", journalTexts(got), err)
	}
	if got, err := run(map[string]string{"name": "a", "since": "1"}); err != nil || !sameTexts(got, "status:x") {
		t.Fatalf("since 1 = %v, %v", journalTexts(got), err)
	}
	if _, err := run(map[string]string{"name": "a", "since": "abc"}); err == nil {
		t.Fatal("a since that is not a number must be refused")
	}
	var missing *NotFoundError
	if _, err := run(map[string]string{"name": "ghost"}); !errors.As(err, &missing) {
		t.Fatalf("unknown session err = %v, want NotFoundError", err)
	}
}
