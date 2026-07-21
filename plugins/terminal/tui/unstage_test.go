package tui

import "testing"

// TestRemoveLastPending pops staged attachments newest-first and reports whether
// anything was removed, so Ctrl+U can fall through to the composer when empty.
func TestRemoveLastPending(t *testing.T) {
	m := newTestModel()
	m.pending = []Attachment{{Name: "a.png"}, {Name: "b.png"}}

	if !m.removeLastPending() {
		t.Fatal("removing from a non-empty stage must report true")
	}
	if len(m.pending) != 1 || m.pending[0].Name != "a.png" {
		t.Fatalf("must drop the newest attachment, got %v", m.pending)
	}
	if !m.removeLastPending() || len(m.pending) != 0 {
		t.Fatalf("must drop the remaining attachment, got %v", m.pending)
	}
	if m.removeLastPending() {
		t.Fatal("removing from an empty stage must report false")
	}
}
