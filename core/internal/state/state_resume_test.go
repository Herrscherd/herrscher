package state

import (
	"path/filepath"
	"testing"
)

func TestSetResumeTokenPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	if err := s.AddSession(Session{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetResumeToken("main", "sid-1"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.FindSession("main")
	if got.ResumeToken != "sid-1" {
		t.Fatalf("want sid-1, got %q", got.ResumeToken)
	}
}

func TestSetResumeTokenUnknownSessionIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := s.SetResumeToken("ghost", "sid"); err != nil {
		t.Fatalf("unknown session must be a silent no-op, got %v", err)
	}
}

func TestSetResumeTokenUnchangedIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	_ = s.AddSession(Session{Name: "main", ResumeToken: "sid-1"})
	if err := s.SetResumeToken("main", "sid-1"); err != nil {
		t.Fatalf("unchanged token must be a no-op, got %v", err)
	}
}
