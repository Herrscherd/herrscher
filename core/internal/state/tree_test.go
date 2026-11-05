package state

import (
	"slices"
	"testing"
)

func TestTreeRootIsTheTopmostExistingSession(t *testing.T) {
	all := []Session{
		{Name: "top"},
		{Name: "mid", Parent: "top"},
		{Name: "leaf", Parent: "mid"},
		{Name: "orphan", Parent: "gone"},
	}
	if got := TreeRoot(all, "leaf"); got != "top" {
		t.Fatalf("root of leaf = %q, want top", got)
	}
	if got := TreeRoot(all, "orphan"); got != "orphan" {
		t.Fatalf("root of an orphan whose parent is gone = %q, want orphan", got)
	}
}

func TestTreeRootStopsOnACycle(t *testing.T) {
	all := []Session{{Name: "a", Parent: "b"}, {Name: "b", Parent: "a"}}
	if got := TreeRoot(all, "a"); got == "" {
		t.Fatal("a cycle must still yield a root")
	}
	if got := TreeChain(all, "a"); !slices.Equal(got, []string{"a", "b", "a"}) {
		t.Fatalf("chain = %v, want the walk stopping at the repeated name", got)
	}
}
