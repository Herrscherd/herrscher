package host

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func newMarkHub(t *testing.T, partDir string, sess state.Session) (*hub, *state.State) {
	t.Helper()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	return &hub{warnings: io.Discard, st: st, partDir: partDir, live: make(map[string]liveSession)}, st
}

func TestAnnounceInterruptedTurnAppendsNoticeAndClearsMark(t *testing.T) {
	partDir := t.TempDir()
	h, st := newMarkHub(t, partDir, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	h.announceInterruptedTurn(state.Session{Name: "main", InterruptedTurn: "turn-1"})

	entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10)
	if len(entries) != 1 || entries[0].Role != "notice" || entries[0].Kind != "turn_interrupted" || entries[0].Text == "" {
		t.Fatalf("transcript = %+v, want one turn_interrupted notice", entries)
	}
	if got, _ := st.FindSession("main"); got.InterruptedTurn != "" {
		t.Fatalf("mark = %q after announce, want cleared", got.InterruptedTurn)
	}
}

func TestAnnounceInterruptedTurnWithoutMarkWritesNothing(t *testing.T) {
	partDir := t.TempDir()
	h, _ := newMarkHub(t, partDir, state.Session{Name: "main"})
	h.announceInterruptedTurn(state.Session{Name: "main"})

	if entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10); len(entries) != 0 {
		t.Fatalf("transcript = %+v, want nothing without a mark", entries)
	}
}

func TestAnnounceInterruptedTurnKeepsMarkWhenTranscriptFails(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, st := newMarkHub(t, blocked, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	h.announceInterruptedTurn(state.Session{Name: "main", InterruptedTurn: "turn-1"})

	if got, _ := st.FindSession("main"); got.InterruptedTurn != "turn-1" {
		t.Fatalf("mark = %q after a failed announce, want kept for the next start", got.InterruptedTurn)
	}
}
