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

func interruptionDriver(t *testing.T, partDir string) *sessionDriver {
	t.Helper()
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "main", nil, make(chan contracts.Event, 8), make(chan contracts.Event, 8), io.Discard)
	d.gate = noBudgetGate{}
	d.activeTurnID = "turn-1"
	d.sink.Transcript = func(e state.TranscriptEntry) error {
		return state.AppendTranscript(state.TranscriptPath(partDir, "main"), e)
	}
	return d
}

func finishTurn(d *sessionDriver, text string, outcome contracts.TurnOutcome) {
	d.completeTurn(context.Background(), contracts.Event{T: eventwire.EventReply, Text: text, Done: true, Outcome: outcome, TurnID: "turn-1"}, time.Now(), 0, outbox.Frame{})
}

func TestInterruptedTurnWithoutOutputRecordsTheInterruption(t *testing.T) {
	partDir := t.TempDir()
	finishTurn(interruptionDriver(t, partDir), "", contracts.TurnInterrupted)

	entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10)
	if len(entries) != 1 || entries[0].Kind != turnInterruptedKind || entries[0].Role != "notice" || entries[0].TurnID != "turn-1" {
		t.Fatalf("transcript = %+v, want one turn_interrupted notice for turn-1", entries)
	}
}

func TestInterruptedTurnWithoutOutputShowsTheNoticeInHistory(t *testing.T) {
	partDir := t.TempDir()
	finishTurn(interruptionDriver(t, partDir), "", contracts.TurnInterrupted)

	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), partDir: partDir})
	lines := h.Scrollback("main")
	if len(lines) != 1 || lines[0].Notice == nil || lines[0].Notice.Code != contracts.CodeTurnInterrupted {
		t.Fatalf("scrollback = %+v, want the interruption notice", lines)
	}
}

func TestInterruptedTurnWithPartialOutputKeepsItAndRecordsTheInterruption(t *testing.T) {
	partDir := t.TempDir()
	finishTurn(interruptionDriver(t, partDir), "half a thought", contracts.TurnInterrupted)

	entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10)
	if len(entries) != 2 || entries[0].Role != "assistant" || entries[0].Text != "half a thought" || entries[1].Kind != turnInterruptedKind {
		t.Fatalf("transcript = %+v, want the partial reply then the interruption", entries)
	}
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), partDir: partDir})
	lines := h.Scrollback("main")
	if len(lines) != 2 || lines[0].Text != "half a thought" || lines[1].Notice == nil {
		t.Fatalf("scrollback = %+v, want the partial reply then the interruption notice", lines)
	}
}

func TestCompletedTurnRecordsNoInterruption(t *testing.T) {
	partDir := t.TempDir()
	finishTurn(interruptionDriver(t, partDir), "fini", contracts.TurnOutcome(""))

	for _, entry := range state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10) {
		if entry.Kind == turnInterruptedKind {
			t.Fatalf("transcript = %+v, a completed turn must not record an interruption", entry)
		}
	}
}
