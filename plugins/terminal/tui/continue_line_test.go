package tui

import "testing"

// The modified-Enter chords are the better gesture when the terminal delivers
// them, and plenty of terminals do not: Alt+Enter gets eaten by the window
// manager, Shift+Enter is indistinguishable from Enter without an extended
// keyboard protocol. A trailing backslash needs nothing from the terminal.
func TestATrailingBackslashBreaksTheLineInsteadOfSending(t *testing.T) {
	m := newTestModel()
	m.input.SetValue(`first line \`)
	if !m.continueLine() {
		t.Fatal("a trailing backslash must take the Enter")
	}
	if got := m.input.Value(); got != "first line \n" {
		t.Fatalf("value = %q, want the backslash replaced by a newline", got)
	}
}

func TestAnOrdinaryDraftStillSends(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("just a message")
	if m.continueLine() {
		t.Fatal("a draft with no trailing backslash must submit")
	}
}

// An escaped backslash is a literal one the operator typed, not a continuation.
func TestAnEscapedBackslashIsNotAContinuation(t *testing.T) {
	m := newTestModel()
	m.input.SetValue(`a windows path C:\\`)
	if m.continueLine() {
		t.Fatal("an escaped backslash must not swallow the send")
	}
}
