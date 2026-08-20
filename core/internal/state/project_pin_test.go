package state

import (
	"path/filepath"
	"testing"
)

func TestSetProjectPinnedPersistsAndPins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	if err := s.AddSession(Session{Name: "main", MemoryProject: "herrscher"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectPinned("main", "neublox"); err != nil {
		t.Fatal(err)
	}
	// Re-read from disk: the point is that the next start sees the settled project.
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.FindSession("main")
	if got.MemoryProject != "neublox" || !got.ProjectPinned {
		t.Fatalf("MemoryProject=%q ProjectPinned=%v, want neublox/true", got.MemoryProject, got.ProjectPinned)
	}
}

func TestSetProjectPinnedUnknownSessionIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := s.SetProjectPinned("ghost", "x"); err != nil {
		t.Fatalf("unknown session must be a silent no-op, got %v", err)
	}
}

func TestSetProjectPinnedUnchangedIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	_ = s.AddSession(Session{Name: "main", MemoryProject: "neublox", ProjectPinned: true})
	if err := s.SetProjectPinned("main", "neublox"); err != nil {
		t.Fatalf("an already-settled project must be a no-op, got %v", err)
	}
}
