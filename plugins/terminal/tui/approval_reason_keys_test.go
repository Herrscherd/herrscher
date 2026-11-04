package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCtrlCInsideTheApprovalReasonFallsThroughToInterrupt(t *testing.T) {
	m, _ := newApprovalModel(t, 100, "ls")
	target := m.tabs["a"]
	if target == nil {
		t.Fatal("the session tab must exist")
	}
	target.pendingApproval = true
	target.approval.reasonOpen = true
	if m.handleApprovalKey(tea.KeyMsg{Type: tea.KeyCtrlC}) {
		t.Fatal("ctrl+c inside the reason box must reach app:interrupt, not be swallowed")
	}
	if !m.handleApprovalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) || target.approval.reason != "a" {
		t.Fatalf("a letter inside the reason box must still be typed, reason = %q", target.approval.reason)
	}
}
