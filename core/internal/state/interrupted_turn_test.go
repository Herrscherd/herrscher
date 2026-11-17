package state

import (
	"path/filepath"
	"testing"
)

func TestSetInterruptedTurnPersistsAndClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main"}))
	must(t, s.SetInterruptedTurn("main", "turn-7"))
	reloaded, err := LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.InterruptedTurn != "turn-7" {
		t.Fatalf("InterruptedTurn = %q, want turn-7", got.InterruptedTurn)
	}
	must(t, s.SetInterruptedTurn("main", ""))
	reloaded, err = LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.InterruptedTurn != "" {
		t.Fatalf("InterruptedTurn = %q after clear, want empty", got.InterruptedTurn)
	}
}

func TestSetInterruptedTurnUnknownSessionIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := s.SetInterruptedTurn("ghost", "turn-1"); err != nil {
		t.Fatalf("unknown session must be a silent no-op, got %v", err)
	}
}

func TestSetInterruptedTurnUnchangedIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main", InterruptedTurn: "turn-1"}))
	if err := s.SetInterruptedTurn("main", "turn-1"); err != nil {
		t.Fatalf("unchanged mark must be a no-op, got %v", err)
	}
}

func TestFreshAccountClearsInterruptedTurn(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main", ResumeToken: "sid-1", InterruptedTurn: "turn-1"}))
	if _, err := s.SetAccount("main", "work", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.FindSession("main"); got.InterruptedTurn != "" {
		t.Fatalf("a fresh conversation must drop the mark, got %q", got.InterruptedTurn)
	}
}

func TestSameAccountKeepsInterruptedTurn(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main", ResumeToken: "sid-1", InterruptedTurn: "turn-1"}))
	if _, err := s.SetAccount("main", "work", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.FindSession("main"); got.InterruptedTurn != "turn-1" {
		t.Fatalf("a kept conversation must keep the mark, got %q", got.InterruptedTurn)
	}
}

func TestBackendTargetChangeClearsInterruptedTurn(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main", ResumeToken: "sid-1", InterruptedTurn: "turn-1"}))
	if _, err := s.SetBackendTarget("main", "other", "cmd", "model"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.FindSession("main"); got.InterruptedTurn != "" {
		t.Fatalf("a new backend must drop the mark, got %q", got.InterruptedTurn)
	}
}
