package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func pressCtrlC(m *model) (tea.Cmd, bool) {
	return m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
}

func TestCtrlCClosesEachDialogWithoutQuittingOrInterrupting(t *testing.T) {
	cases := map[string]func(t *testing.T) (*model, *fakeBackend, func() bool){
		"diff commit dialog": func(_ *testing.T) (*model, *fakeBackend, func() bool) {
			f := &fakeBackend{sessions: namedSession()}
			m := modeSession(f)
			m.diff.open, m.diff.dlg = true, dialogCommit
			return m, f, func() bool { return m.diff.dlg == dialogNone }
		},
		"diff message edit": func(_ *testing.T) (*model, *fakeBackend, func() bool) {
			f := &fakeBackend{sessions: namedSession()}
			m := modeSession(f)
			m.diff.open, m.diff.dlg, m.diff.editing = true, dialogCommit, true
			return m, f, func() bool { return !m.diff.editing }
		},
		"approval reason": func(t *testing.T) (*model, *fakeBackend, func() bool) {
			t.Helper()
			m, f := newApprovalModel(t, 100, "ls")
			target := m.tabs["a"]
			target.pendingApproval, target.approval.reasonOpen = true, true
			return m, f, func() bool { return !target.approval.reasonOpen }
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m, f, closed := setup(t)
			m.tabs["a"].busy = true
			cmd, _ := pressCtrlC(m)
			if isQuit(cmd) {
				t.Fatal("ctrl+c in an open dialog must not quit the TUI")
			}
			if !closed() {
				t.Fatal("ctrl+c in an open dialog must close it, as escape does")
			}
			if m.tabs["a"].interruptRequested || len(f.interrupted) > 0 {
				t.Fatal("ctrl+c in an open dialog must not interrupt the running turn")
			}
		})
	}
}

func TestCtrlCReachesTheInterruptFromTheDeclaredFallThroughContexts(t *testing.T) {
	cases := map[string]func(t *testing.T) *model{
		"approval": func(t *testing.T) *model {
			t.Helper()
			m, _ := newApprovalModel(t, 100, "ls")
			m.tabs["a"].pendingApproval = true
			return m
		},
		"autocomplete": func(_ *testing.T) *model {
			m := modeSession(&fakeBackend{sessions: namedSession()})
			m.input.SetValue("/")
			return m
		},
		"mention": func(_ *testing.T) *model {
			m := modeSession(&fakeBackend{sessions: namedSession()})
			m.input.SetValue("@")
			return m
		},
		"queue": func(_ *testing.T) *model {
			m := modeSession(&fakeBackend{sessions: namedSession()})
			m.queueFocus = true
			return m
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m := setup(t)
			m.tabs["a"].busy = true
			cmd, _ := pressCtrlC(m)
			if isQuit(cmd) || !m.tabs["a"].interruptRequested {
				t.Fatalf("ctrl+c in %s must reach app:interrupt and interrupt the busy turn", name)
			}
		})
	}
}

func TestCtrlDQuitGuardNamesTheKeyPressed(t *testing.T) {
	m := modeSession(&fakeBackend{sessions: namedSession()})
	m.ensureTab("b").busy = true
	want := m.hintOf(actExit)
	if want == m.hintOf(actInterrupt) {
		t.Fatal("the test needs ctrl+d and ctrl+c to have distinct hints")
	}
	cmd, _ := pressCtrlD(t, m)
	if isQuit(cmd) {
		t.Fatal("the first ctrl+d with a busy session must only arm the guard")
	}
	if !strings.Contains(m.flash, want+" à nouveau pour quitter") {
		t.Fatalf("the guard must name the key pressed (%s), got flash %q", want, m.flash)
	}
}

func TestCtrlCInTheDiffNoteDoesNothing(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	m.diff.open, m.diff.dlg, m.diff.field = true, dialogNote, "wip"
	m.tabs["a"].busy = true
	cmd, _ := pressCtrlC(m)
	if isQuit(cmd) {
		t.Fatal("ctrl+c in the diff note must not quit the TUI")
	}
	if m.diff.dlg != dialogNote || m.diff.field != "wip" {
		t.Fatalf("ctrl+c in the diff note must leave the note and its text, got dlg=%v field=%q", m.diff.dlg, m.diff.field)
	}
	if m.tabs["a"].interruptRequested || len(f.interrupted) > 0 {
		t.Fatal("ctrl+c in the diff note must not interrupt the running turn")
	}
}

func TestCtrlCClosingKeepsTheTypedText(t *testing.T) {
	cases := map[string]func(t *testing.T) (*model, func() string, func() bool){
		"diff commit dialog": func(_ *testing.T) (*model, func() string, func() bool) {
			m := modeSession(&fakeBackend{sessions: namedSession()})
			m.diff.open, m.diff.dlg, m.diff.message = true, dialogCommit, "feat: draft"
			return m, func() string { return m.diff.message }, func() bool { return m.diff.dlg == dialogNone }
		},
		"diff message edit": func(_ *testing.T) (*model, func() string, func() bool) {
			m := modeSession(&fakeBackend{sessions: namedSession()})
			m.diff.open, m.diff.dlg, m.diff.editing, m.diff.message = true, dialogCommit, true, "feat: draft"
			return m, func() string { return m.diff.message }, func() bool { return !m.diff.editing }
		},
		"approval reason": func(t *testing.T) (*model, func() string, func() bool) {
			t.Helper()
			m, _ := newApprovalModel(t, 100, "ls")
			target := m.tabs["a"]
			target.pendingApproval, target.approval.reasonOpen, target.approval.reason = true, true, "too risky"
			return m, func() string { return target.approval.reason }, func() bool { return !target.approval.reasonOpen }
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m, text, closed := setup(t)
			want := text()
			pressCtrlC(m)
			if !closed() {
				t.Fatal("ctrl+c must close the dialog")
			}
			if got := text(); got != want {
				t.Fatalf("ctrl+c must keep the typed text, got %q want %q", got, want)
			}
		})
	}
}
