package tui

import tea "github.com/charmbracelet/bubbletea"

// The window captures the mouse so the wheel can scroll a transcript the
// alternate screen gives no scrollback for. The price is the terminal's own
// click-drag selection, which most terminals hand back under Shift — most, not
// all, and not always the way its owner configured it. So the capture is a
// setting rather than a fact: toggleMouse gives the mouse to the terminal, and
// the window keeps working without it (pgup/pgdn, ctrl+s search, /copy).

// toggleMouse releases the mouse to the terminal, or takes it back. It returns
// the command that actually changes the terminal's mode, so the state and the
// escape sequence can never disagree.
func (m *model) toggleMouse() tea.Cmd {
	m.mouseFree = !m.mouseFree
	if m.mouseFree {
		m.flash = "mouse released — drag to select as usual · pgup/pgdn scroll · ctrl+g takes it back"
		return tea.DisableMouse
	}
	m.flash = "mouse captured — the wheel scrolls the transcript again"
	return tea.EnableMouseCellMotion
}

// mouseStatus is the status-bar segment naming the released mouse, and "" while
// the window holds it (the default needs no announcement).
func (m *model) mouseStatus() string {
	if !m.mouseFree {
		return ""
	}
	return dimStyle.Render("mouse → terminal")
}
