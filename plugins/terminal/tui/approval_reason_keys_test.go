package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCtrlCInsideTheApprovalReasonClosesTheBox(t *testing.T) {
	m, f := newApprovalModel(t, 100, "ls")
	target := m.tabs["a"]
	if target == nil {
		t.Fatal("the session tab must exist")
	}
	target.pendingApproval = true
	target.approval.reasonOpen = true
	target.busy = true
	if !m.handleApprovalKey(tea.KeyMsg{Type: tea.KeyCtrlC}) || target.approval.reasonOpen {
		t.Fatal("ctrl+c inside the reason box must close it, as escape does")
	}
	if target.interruptRequested || len(f.interrupted) > 0 {
		t.Fatal("ctrl+c inside the reason box must not interrupt the running turn")
	}
	target.approval.reasonOpen = true
	if !m.handleApprovalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) || target.approval.reason != "a" {
		t.Fatalf("a letter inside the reason box must still be typed, reason = %q", target.approval.reason)
	}
}
