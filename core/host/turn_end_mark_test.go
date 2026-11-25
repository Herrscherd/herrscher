package host

import (
	"context"
	"io"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/outbox"
)

func completeTurnThenCrash(t *testing.T, text string) (*hub, *state.State, string) {
	t.Helper()
	partDir := t.TempDir()
	h, st := newMarkHub(t, partDir, state.Session{Name: "main"})
	h.openTurnMark("main", "turn-1")
	sink := h.liveSink(context.Background(), state.Session{Name: "main"})
	sink.TurnSettled = nil
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "main", nil, make(chan contracts.Event, 8), make(chan contracts.Event, 8), io.Discard)
	d.gate = noBudgetGate{}
	d.activeTurnID = "turn-1"
	d.sink = sink
	d.completeTurn(context.Background(), contracts.Event{T: eventwire.EventReply, Text: text, Done: true, TurnID: "turn-1"}, time.Now(), 0, outbox.Frame{})
	return h, st, partDir
}

func TestCompletedTurnWithReplyAnnouncesNothingAfterACrashBeforeSettling(t *testing.T) {
	h, st, partDir := completeTurnThenCrash(t, "fini")
	sess, _ := st.FindSession("main")
	h.announceInterruptedTurn(sess)

	entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10)
	if len(entries) != 1 || entries[0].Role != "assistant" || entries[0].Text != "fini" {
		t.Fatalf("transcript = %+v, want the reply alone", entries)
	}
	if got, _ := st.FindSession("main"); got.InterruptedTurn != "" {
		t.Fatalf("mark = %q after the start, want cleared", got.InterruptedTurn)
	}
}

func TestCompletedTurnWithoutOutputAnnouncesNothingAfterACrashBeforeSettling(t *testing.T) {
	h, st, partDir := completeTurnThenCrash(t, "")
	sess, _ := st.FindSession("main")
	h.announceInterruptedTurn(sess)

	if entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10); len(entries) != 0 {
		t.Fatalf("transcript = %+v, want no notice for a completed turn", entries)
	}
	if got, _ := st.FindSession("main"); got.InterruptedTurn != "" {
		t.Fatalf("mark = %q after the start, want cleared", got.InterruptedTurn)
	}
}
