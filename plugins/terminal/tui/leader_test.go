package tui

import "testing"

func TestLeaderTableHoldsEditorAndEnter(t *testing.T) {
	for _, key := range []string{"ctrl+e", "enter"} {
		if _, ok := leaderKeys[key]; !ok {
			t.Fatalf("leader table misses %q", key)
		}
	}
}

func TestRunLeaderUnknownKeyIsNotHandled(t *testing.T) {
	m, _ := queueFixture(t)
	if _, ok := m.runLeader("z"); ok {
		t.Fatal("an unknown leader key must fall through")
	}
}
