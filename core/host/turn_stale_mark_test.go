package host

import (
	"os"
	"path/filepath"
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

func TestOpenTurnMarkKeepsTheStaleMarkWhenItsNoticeIsRefused(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, st := newMarkHub(t, blocked, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	h.openTurnMark("main", "turn-2")

	if got, _ := st.FindSession("main"); got.InterruptedTurn != "turn-1" {
		t.Fatalf("mark = %q, want the stale turn-1 kept until its notice is written", got.InterruptedTurn)
	}
}

func TestStaleNoticeIsWrittenOnceWhenALaterOpenCanWrite(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "transcripts")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, st := newMarkHub(t, blocked, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	h.openTurnMark("main", "turn-2")
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	h.openTurnMark("main", "turn-3")
	h.openTurnMark("main", "turn-4")

	var notices int
	for _, e := range state.ReadTranscript(state.TranscriptPath(blocked, "main"), 10) {
		if e.Kind == turnInterruptedKind && e.TurnID == "turn-1" {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("turn-1 notices = %d, want exactly one", notices)
	}
	if got, _ := st.FindSession("main"); got.InterruptedTurn != "turn-4" {
		t.Fatalf("mark = %q, want turn-4", got.InterruptedTurn)
	}
}

func TestStaleNoticeSurvivesTheSettlingOfTheTurnThatCouldNotOpen(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "transcripts")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, _ := newMarkHub(t, blocked, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	h.openTurnMark("main", "turn-2")
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	h.clearTurnMark("main", "turn-2")
	h.openTurnMark("main", "turn-3")

	entries := state.ReadTranscript(state.TranscriptPath(blocked, "main"), 10)
	if len(entries) != 1 || entries[0].Kind != turnInterruptedKind || entries[0].TurnID != "turn-1" {
		t.Fatalf("transcript = %+v, want the notice of the stale turn-1 after turn-2 settles", entries)
	}
}
