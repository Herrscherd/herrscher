package state

import (
	"path/filepath"
	"testing"
)

func TestSetPausedReasonPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	if err := s.AddSession(Session{Name: "game"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPausedReason("game", "cost"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.FindSession("game")
	if got.PausedReason != "cost" {
		t.Fatalf("want cost, got %q", got.PausedReason)
	}

	// Clearing resumes the session.
	if err := s.SetPausedReason("game", ""); err != nil {
		t.Fatal(err)
	}
	reloaded2, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got2, _ := reloaded2.FindSession("game")
	if got2.PausedReason != "" {
		t.Fatalf("want cleared, got %q", got2.PausedReason)
	}
}

func TestSetPausedReasonUnknownSessionIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := s.SetPausedReason("ghost", "cost"); err != nil {
		t.Fatalf("unknown session must be a silent no-op, got %v", err)
	}
}

func TestSetPausedReasonUnchangedIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	_ = s.AddSession(Session{Name: "game", PausedReason: "cost"})
	if err := s.SetPausedReason("game", "cost"); err != nil {
		t.Fatalf("unchanged reason must be a no-op, got %v", err)
	}
}
