package host

import (
	"context"
	"io"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func abandonDriver(h *hub, toBridge, from chan contracts.Event) *sessionDriver {
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "main", nil, toBridge, from, io.Discard)
	d.gate = noBudgetGate{}
	d.sink = sessionSink{
		Transcript:  func(e state.TranscriptEntry) error { return h.appendTranscript("main", e) },
		TurnOpened:  func(turnID string) { h.openTurnMark("main", turnID) },
		TurnSettled: func(turnID string) { h.clearTurnMark("main", turnID) },
	}
	return d
}

func startTurn(t *testing.T, d *sessionDriver) (contracts.Event, tokenGuard) {
	t.Helper()
	event, guard, ok := d.prepareTurn(context.Background(), queued{ev: contracts.Event{T: eventwire.EventInput, Text: "question"}})
	if !ok {
		t.Fatal("prepareTurn refused the turn")
	}
	return event, guard
}

func refuseNotices(d *sessionDriver) {
	transcript := d.sink.Transcript
	d.sink.Transcript = func(e state.TranscriptEntry) error {
		if e.Kind == turnInterruptedKind {
			return errTraceRefused
		}
		return transcript(e)
	}
}

func TestTurnAbandonedWhileAwaitingWritesItsNoticeBeforeSettling(t *testing.T) {
	partDir := t.TempDir()
	h, st := newMarkHub(t, partDir, state.Session{Name: "main"})
	from := make(chan contracts.Event)
	close(from)
	d := abandonDriver(h, make(chan contracts.Event, 8), from)
	event, guard := startTurn(t, d)
	d.runTurn(context.Background(), event, guard)

	entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10)
	if len(entries) != 2 || entries[0].Role != "user" || entries[1].Kind != turnInterruptedKind || entries[1].TurnID != event.TurnID {
		t.Fatalf("transcript = %+v, want the question then the interruption of its turn", entries)
	}
	if got, _ := st.FindSession("main"); got.InterruptedTurn != "" {
		t.Fatalf("mark = %q, want cleared once the notice is written", got.InterruptedTurn)
	}
}

func TestTurnAbandonedKeepsItsMarkWhenTheNoticeIsRefused(t *testing.T) {
	partDir := t.TempDir()
	h, st := newMarkHub(t, partDir, state.Session{Name: "main"})
	from := make(chan contracts.Event)
	close(from)
	d := abandonDriver(h, make(chan contracts.Event, 8), from)
	refuseNotices(d)
	event, guard := startTurn(t, d)
	d.runTurn(context.Background(), event, guard)

	entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10)
	if len(entries) != 1 || entries[0].Role != "user" {
		t.Fatalf("transcript = %+v, want the question alone when the notice is refused", entries)
	}
	if got, _ := st.FindSession("main"); got.InterruptedTurn != event.TurnID {
		t.Fatalf("mark = %q, want %q kept for the next start", got.InterruptedTurn, event.TurnID)
	}
}

func TestTurnAbandonedWhenTheBridgeCannotBeReachedWritesItsNotice(t *testing.T) {
	partDir := t.TempDir()
	h, st := newMarkHub(t, partDir, state.Session{Name: "main"})
	d := abandonDriver(h, make(chan contracts.Event), make(chan contracts.Event))
	close(d.hangup)
	event, guard := startTurn(t, d)
	d.runTurn(context.Background(), event, guard)

	entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10)
	if len(entries) != 2 || entries[1].Kind != turnInterruptedKind {
		t.Fatalf("transcript = %+v, want the question then the interruption", entries)
	}
	if got, _ := st.FindSession("main"); got.InterruptedTurn != "" {
		t.Fatalf("mark = %q, want cleared", got.InterruptedTurn)
	}
}

func TestTurnAbandonedByHostStopLeavesItsMarkForTheNextStart(t *testing.T) {
	partDir := t.TempDir()
	h, st := newMarkHub(t, partDir, state.Session{Name: "main"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := abandonDriver(h, make(chan contracts.Event), make(chan contracts.Event))
	event, guard, ok := d.prepareTurn(ctx, queued{ev: contracts.Event{T: eventwire.EventInput, Text: "question"}})
	if !ok {
		t.Fatal("prepareTurn refused the turn")
	}
	d.runTurn(ctx, event, guard)

	if entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10); len(entries) != 1 {
		t.Fatalf("transcript = %+v, want the question alone while the host stops", entries)
	}
	sess, _ := st.FindSession("main")
	if sess.InterruptedTurn != event.TurnID {
		t.Fatalf("mark = %q, want %q kept for the next start", sess.InterruptedTurn, event.TurnID)
	}
	h.announceInterruptedTurn(sess)
	if entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10); len(entries) != 2 || entries[1].Kind != turnInterruptedKind {
		t.Fatalf("transcript = %+v, want the notice written at the next start", entries)
	}
}
