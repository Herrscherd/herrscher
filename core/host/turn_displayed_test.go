package host

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/outbox"
)

func completeTurnCapturing(t *testing.T, d *sessionDriver, reply contracts.Event, frame outbox.Frame) (contracts.Event, state.TranscriptEntry) {
	t.Helper()
	var entries []state.TranscriptEntry
	var replies []contracts.Event
	d.sink.Transcript = func(e state.TranscriptEntry) { entries = append(entries, e) }
	d.emitTap = func(e contracts.Event) {
		if e.T == eventwire.EventReply {
			replies = append(replies, e)
		}
	}
	d.terms = func() modelTerms { return modelTerms{rates: sonnetRates()} }

	d.completeTurn(context.Background(), reply, time.Now(), 0, frame)

	if len(replies) != 1 || len(entries) != 1 {
		t.Fatalf("tapped replies = %d, transcript entries = %d, want one each", len(replies), len(entries))
	}
	return replies[0], entries[0]
}

func assertSameShownText(t *testing.T, tapped contracts.Event, entry state.TranscriptEntry) {
	t.Helper()
	line := scrollbackLineOf(entry)
	if line.Text != tapped.Text {
		t.Fatalf("scrollback shows %q, the tap delivered %q: they must be the same text", line.Text, tapped.Text)
	}
	if entry.Text != "hello" {
		t.Fatalf("transcript Text = %q, want the model text %q", entry.Text, "hello")
	}
	if strings.Contains(entry.Text, "big.bin") {
		t.Fatalf("transcript Text carries the shown notice: %q", entry.Text)
	}
	if !strings.Contains(line.Text, "big.bin") {
		t.Fatalf("shown text %q lacks the refused file name", line.Text)
	}
}

func TestRefusedFilesShowTheSameTextInTapAndScrollback(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	frame.Refused = []string{"big.bin"}
	d := newOutboundDriver([]contracts.Gateway{&textRecordingGateway{}}, root)

	tapped, entry := completeTurnCapturing(t, d, contracts.Event{T: eventwire.EventReply, Text: "hello", Done: true}, frame)

	assertSameShownText(t, tapped, entry)
}

func TestSaidElsewhereShowsTheSameTextInTapAndScrollback(t *testing.T) {
	d := newOutboundDriver([]contracts.Gateway{&routedFileGateway{}}, t.TempDir())
	d.saidElsewhere = true

	tapped, entry := completeTurnCapturing(t, d, contracts.Event{T: eventwire.EventReply, Text: "hello", Done: true}, outbox.Frame{Refused: []string{"big.bin"}})

	assertSameShownText(t, tapped, entry)
}

func TestScrollbackShowsTextForEntriesWithoutDisplayedText(t *testing.T) {
	if line := scrollbackLineOf(state.TranscriptEntry{Role: "assistant", Text: "old", TurnID: "t1"}); line.Text != "old" {
		t.Fatalf("scrollback text = %q, want the transcript Text", line.Text)
	}
	if line := scrollbackLineOf(state.TranscriptEntry{Role: "assistant", Text: "hello", Displayed: "hello\n\nnotice"}); line.Text != "hello\n\nnotice" {
		t.Fatalf("scrollback text = %q, want the displayed text", line.Text)
	}
}

func TestNoticeOnlyReplyIsRecordedWithItsNotice(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	frame.Refused = []string{"big.bin"}
	d := newOutboundDriver([]contracts.Gateway{&textRecordingGateway{}}, root)

	tapped, entry := completeTurnCapturing(t, d, contracts.Event{T: eventwire.EventReply, Done: true}, frame)

	if entry.Text != "" {
		t.Fatalf("transcript Text = %q, want the empty model text", entry.Text)
	}
	line := scrollbackLineOf(entry)
	if line.Text != tapped.Text || !strings.Contains(line.Text, "big.bin") {
		t.Fatalf("scrollback shows %q, the tap delivered %q: the notice must replay as it was shown", line.Text, tapped.Text)
	}
}

func transcriptJSONKeys(t *testing.T, entry state.TranscriptEntry) map[string]json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestDisplayedKeyIsAbsentFromTranscriptJSONWhenEmpty(t *testing.T) {
	keys := transcriptJSONKeys(t, state.TranscriptEntry{Role: "assistant", Text: "hello"})
	if _, present := keys["displayed"]; present {
		t.Fatalf("entry without displayed text encodes the displayed key, want it omitted: %v", keys)
	}
}

func TestDisplayedKeyIsPresentInTranscriptJSONWhenSet(t *testing.T) {
	keys := transcriptJSONKeys(t, state.TranscriptEntry{Role: "assistant", Text: "hello", Displayed: "hello\n\nnotice"})
	if _, present := keys["displayed"]; !present {
		t.Fatalf("entry with displayed text encodes %v, want the displayed key present", keys)
	}
}
