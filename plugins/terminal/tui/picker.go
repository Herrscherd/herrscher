package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// resumeMax bounds how many rows the /resume picker shows at once.
const resumeMax = 8

// openResume populates the /resume picker with the hub's sessions, most-recently
// active first (by transcript LastTs), and opens the overlay. Sessions with no
// transcript sort last (empty LastTs).
func (m *model) openResume() {
	rows := append([]contracts.SessionInfo(nil), m.tm.Sessions()...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].LastTs > rows[j].LastTs })
	m.resumeRows = rows
	m.resumeIdx = 0
	m.resumeOpen = true
}

// clampResume keeps the selection index within the current row set.
func (m *model) clampResume() {
	n := len(m.resumeRows)
	if m.resumeIdx >= n {
		m.resumeIdx = n - 1
	}
	if m.resumeIdx < 0 {
		m.resumeIdx = 0
	}
}

func (m *model) moveResume(d int) { m.resumeIdx += d; m.clampResume() }

// chooseResume acts on the selected picker row: it opens (or focuses) the
// session's tab now — seeding scrollback from the recorded transcript — and, when
// the session is archived, returns a command that revives it in the daemon off
// the Update loop. Its result surfaces as a dispatchResultMsg in the tab.
func (m *model) chooseResume() tea.Cmd {
	if m.resumeIdx < 0 || m.resumeIdx >= len(m.resumeRows) {
		return nil
	}
	s := m.resumeRows[m.resumeIdx]
	m.ensureTab(s.ChannelID) // creates+seeds when new; returns existing when live
	m.active = s.ChannelID
	if tb := m.tabs[s.ChannelID]; tb != nil {
		tb.unread = false
	}
	m.syncViewport()
	if !s.Archived {
		return nil // live session: opened or focused, nothing to revive
	}
	name, ch, tm := s.Name, s.ChannelID, m.tm
	return func() tea.Msg {
		out, err := tm.Resume(name)
		return dispatchResultMsg{origin: ch, out: out, err: err}
	}
}

// resumeView renders the picker as an inline Claude menu (no border box): a dim
// header, then one row per session with its name, project, last-active timestamp,
// vendor, live/archived state, and a ⟲ when the backend can resume it. The
// selected row is prefixed ❯ in the warm accent.
func (m *model) resumeView() string {
	var b strings.Builder
	b.WriteString(dimStyle.Render("resume — ↑↓ select · Enter open · Esc cancel"))
	if len(m.resumeRows) == 0 {
		b.WriteString("\n" + dimStyle.Render("  (no sessions)"))
		return b.String()
	}
	for i, s := range m.resumeRows {
		if i >= resumeMax {
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … +%d more", len(m.resumeRows)-resumeMax)))
			break
		}
		state := "live"
		if s.Archived {
			state = "archived"
		}
		cols := []string{s.Name}
		if s.Project != "" {
			cols = append(cols, s.Project)
		}
		if s.LastTs != "" {
			cols = append(cols, s.LastTs)
		}
		if s.Vendor != "" {
			cols = append(cols, s.Vendor)
		}
		cols = append(cols, state)
		if s.Resumable {
			cols = append(cols, "⟲")
		}
		row := strings.Join(cols, " · ")
		if i == m.resumeIdx {
			row = warmStyle.Render(glyphCursor + " " + row)
		} else {
			row = dimStyle.Render("  " + row)
		}
		b.WriteString("\n" + row)
	}
	return b.String()
}

// resumeHeight is the rendered row count of the open picker (0 when closed), so
// chromeHeight can reserve space for it.
func (m *model) resumeHeight() int {
	if !m.resumeOpen {
		return 0
	}
	return lipgloss.Height(m.resumeView())
}

// switchMax bounds how many rows the /session switch picker shows at once.
const switchMax = 8

// openSwitch populates the invisible session switcher with the hub's live
// sessions (most-recently active first) and opens the overlay. Unlike /resume it
// lists only sessions already known to the TUI as tabs or advertised live, so a
// switch never revives an archived session — it only re-focuses an existing one.
func (m *model) openSwitch() {
	rows := append([]contracts.SessionInfo(nil), m.tm.Sessions()...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].LastTs > rows[j].LastTs })
	m.switchRows = rows
	m.switchIdx = 0
	m.switchOpen = true
}

// clampSwitch keeps the selection index within the current row set.
func (m *model) clampSwitch() {
	n := len(m.switchRows)
	if m.switchIdx >= n {
		m.switchIdx = n - 1
	}
	if m.switchIdx < 0 {
		m.switchIdx = 0
	}
}

func (m *model) moveSwitch(d int) { m.switchIdx += d; m.clampSwitch() }

// chooseSwitch focuses the selected session's tab, swapping the active transcript
// without any visible tab strip — the "invisible" multi-session switch.
func (m *model) chooseSwitch() {
	if m.switchIdx < 0 || m.switchIdx >= len(m.switchRows) {
		return
	}
	s := m.switchRows[m.switchIdx]
	m.ensureTab(s.ChannelID)
	m.active = s.ChannelID
	if tb := m.tabs[s.ChannelID]; tb != nil {
		tb.unread = false
	}
	m.syncViewport()
}

// switchView renders the session switcher as an inline Claude menu (no border):
// a dim header then one row per session, the selected row prefixed ❯ in the warm
// accent and a • unread marker for background sessions with new output.
func (m *model) switchView() string {
	var b strings.Builder
	b.WriteString(dimStyle.Render("switch — ↑↓ select · Enter focus · Esc cancel"))
	if len(m.switchRows) == 0 {
		b.WriteString("\n" + dimStyle.Render("  (no sessions)"))
		return b.String()
	}
	for i, s := range m.switchRows {
		if i >= switchMax {
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … +%d more", len(m.switchRows)-switchMax)))
			break
		}
		label := s.Name
		if tb := m.tabs[s.ChannelID]; tb != nil && tb.unread {
			label += " " + glyphUnread
		}
		if i == m.switchIdx {
			b.WriteString("\n" + warmStyle.Render(glyphCursor+" "+label))
		} else {
			b.WriteString("\n" + dimStyle.Render("  "+label))
		}
	}
	return b.String()
}

// switchHeight is the rendered row count of the open switcher (0 when closed).
func (m *model) switchHeight() int {
	if !m.switchOpen {
		return 0
	}
	return lipgloss.Height(m.switchView())
}

// ChoiceOption is one selectable answer in a permission menu: Label is shown to
// the operator, Value is the frame value submitted to the backend when chosen.
type ChoiceOption struct {
	Label string
	Value string
}

// PendingChoice is an active allow/deny permission menu awaiting a selection.
// Question frames what is being permitted (e.g. "run Bash(rm -rf build/)");
// Channel is the session the answer is submitted back to.
type PendingChoice struct {
	Question string
	Options  []ChoiceOption
	Channel  string
}

// openChoice presents a permission menu, resetting the selection to the first
// (safe) option and resizing the viewport to make room for the inline menu.
func (m *model) openChoice(pc *PendingChoice) {
	m.choice = pc
	m.choiceIdx = 0
	m.applySize()
	m.syncViewport()
}

// clampChoice keeps the selection index within the option set.
func (m *model) clampChoice() {
	if m.choice == nil {
		return
	}
	n := len(m.choice.Options)
	if m.choiceIdx >= n {
		m.choiceIdx = n - 1
	}
	if m.choiceIdx < 0 {
		m.choiceIdx = 0
	}
}

func (m *model) moveChoice(d int) { m.choiceIdx += d; m.clampChoice() }

// chooseChoice submits the selected option's value back to the session as a pick
// and dismisses the menu. With no pick-frame seam in the TUI backend, the value
// rides the normal Submit path (see design §4.4) — the hub interprets it as the
// answer to its outstanding Menu prompt.
func (m *model) chooseChoice() {
	if m.choice == nil {
		return
	}
	if m.choiceIdx >= 0 && m.choiceIdx < len(m.choice.Options) {
		ch := m.choice.Channel
		if ch == "" {
			ch = m.active
		}
		m.tm.Submit(ch, m.choice.Options[m.choiceIdx].Value, nil)
	}
	m.choice = nil
	m.choiceIdx = 0
	m.applySize()
	m.syncViewport()
}

// choiceView renders the permission menu in the Claude shape: a framing header
// naming the action, then numbered options with the selected one prefixed ❯ in
// the warm accent, the rest dim, and the final (deny) row tagged (esc).
func (m *model) choiceView() string {
	if m.choice == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(warmStyle.Render(glyphTool + " " + m.choice.Question))
	for i, o := range m.choice.Options {
		label := fmt.Sprintf("%d. %s", i+1, o.Label)
		if i == len(m.choice.Options)-1 {
			label += "  (esc)"
		}
		if i == m.choiceIdx {
			b.WriteString("\n" + warmStyle.Render(glyphCursor+" "+label))
		} else {
			b.WriteString("\n" + dimStyle.Render("  "+label))
		}
	}
	return b.String()
}

// choiceHeight is the rendered row count of the open permission menu (0 when
// closed), so chromeHeight can reserve space for it.
func (m *model) choiceHeight() int {
	if m.choice == nil {
		return 0
	}
	return lipgloss.Height(m.choiceView())
}
