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

// resumeView renders the picker as a bordered box: one row per session with its
// name, project, last-active timestamp, vendor, live/archived state, and a ⟲ when
// the backend can resume it.
func (m *model) resumeView() string {
	var b strings.Builder
	b.WriteString(statusStyle.Render("resume — ↑↓ select · Enter open · Esc cancel"))
	if len(m.resumeRows) == 0 {
		b.WriteString("\n" + statusStyle.Render("  (no sessions)"))
		return paletteBorder.Render(b.String())
	}
	for i, s := range m.resumeRows {
		if i >= resumeMax {
			b.WriteString("\n" + statusStyle.Render(fmt.Sprintf("  … +%d more", len(m.resumeRows)-resumeMax)))
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
			row = paletteSelStyle.Render(glyphYou + " " + row)
		} else {
			row = "  " + statusStyle.Render(row)
		}
		b.WriteString("\n" + row)
	}
	return paletteBorder.Render(b.String())
}

// resumeHeight is the rendered row count of the open picker (0 when closed), so
// chromeHeight can reserve space for it.
func (m *model) resumeHeight() int {
	if !m.resumeOpen {
		return 0
	}
	return lipgloss.Height(m.resumeView())
}
