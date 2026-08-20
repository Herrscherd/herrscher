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

// TestMemoryRootsPreferTheMemoryFields pins the precedence every memory-scoping
// path relies on: a memory root answers first, and the placement field of the
// same name is only the fallback for the sessions that never set one.
func TestMemoryRootsPreferTheMemoryFields(t *testing.T) {
	for _, tc := range []struct {
		name           string
		sess           Session
		project, agent string
	}{
		{"memory roots win", Session{Project: "placed", Agent: "worker", MemoryProject: "learned", MemoryAgent: "tui"}, "learned", "tui"},
		{"placement is the fallback", Session{Project: "placed", Agent: "worker"}, "placed", "worker"},
		{"nothing set", Session{}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project, agent := tc.sess.MemoryRoots()
			if project != tc.project || agent != tc.agent {
				t.Fatalf("MemoryRoots() = %q, %q; want %q, %q", project, agent, tc.project, tc.agent)
			}
		})
	}
}
