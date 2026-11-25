package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestOpenTurnMarkAnnouncesAStaleTurnBeforeReplacingIt(t *testing.T) {
	partDir := t.TempDir()
	h, st := newMarkHub(t, partDir, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	h.openTurnMark("main", "turn-2")

	entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10)
	if len(entries) != 1 || entries[0].Kind != turnInterruptedKind || entries[0].TurnID != "turn-1" {
		t.Fatalf("transcript = %+v, want the notice of the stale turn-1", entries)
	}
	if got, _ := st.FindSession("main"); got.InterruptedTurn != "turn-2" {
		t.Fatalf("mark = %q, want turn-2", got.InterruptedTurn)
	}
}
