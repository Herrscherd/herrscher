package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestRecordEntryStampsTheActiveTurn(t *testing.T) {
	var got []state.TranscriptEntry
	d := newSessionDriver("s", nil, make(chan contracts.Event, 8), make(chan contracts.Event, 8), io.Discard)
	d.sink.Transcript = func(e state.TranscriptEntry) { got = append(got, e) }

	d.activeTurnID = "turn-7"
	d.recordEntry("assistant", "hi", 0, turnUsage{})
	d.activeTurnID = ""
	d.recordEntry("notice", "hors tour", 0, turnUsage{})

	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[0].TurnID != "turn-7" {
		t.Fatalf("in-turn entry TurnID = %q, want turn-7", got[0].TurnID)
	}
	if got[1].TurnID != "" {
		t.Fatalf("off-turn entry TurnID = %q, want empty", got[1].TurnID)
	}
}

func TestScrollbackCarriesTurnIDAndOldTranscriptReadsWithout(t *testing.T) {
	partDir := t.TempDir()
	path := state.TranscriptPath(partDir, "s")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"ts":"t","role":"user","text":"avant"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.AppendTranscript(path, state.TranscriptEntry{Ts: "t", Role: "assistant", Text: "ok", TurnID: "turn-7"}); err != nil {
		t.Fatal(err)
	}

	got := scrollbackOf(partDir, "s")
	want := []contracts.ScrollbackLine{
		{Role: "user", Text: "avant"},
		{Role: "assistant", Text: "ok", TurnID: "turn-7"},
	}
	if len(got) != len(want) {
		t.Fatalf("scrollback = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTerminalReplyCarriesItsTurnID(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from, io.Discard)
	d.activeTurnID = "turn-7"
	var replies []contracts.Event
	d.emitTap = func(e contracts.Event) {
		if e.T == eventwire.EventReply && e.Done {
			replies = append(replies, e)
		}
	}

	from <- contracts.Event{T: eventwire.EventReply, Done: true, Text: "ok"}
	if !d.awaitTurn(context.Background(), tokenGuard{}) {
		t.Fatal("turn abandoned")
	}

	if len(replies) == 0 {
		t.Fatal("no terminal reply emitted")
	}
	for _, reply := range replies {
		if reply.TurnID != "turn-7" {
			t.Fatalf("reply TurnID = %q, want turn-7", reply.TurnID)
		}
	}
}
