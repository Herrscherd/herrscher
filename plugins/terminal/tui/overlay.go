package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type overlayRow struct {
	mark   string
	label  string
	detail string
}

const overlayLabelWidth = 16

var overlaySelStyle = lipgloss.NewStyle().Background(lipgloss.Color(colSelBg)).Foreground(lipgloss.Color(colAccent))

func floatingList(rows []overlayRow, cursor int, footer string, width int) string {
	if len(rows) == 0 {
		return ""
	}
	out := make([]string, 0, len(rows)+1)
	for i, r := range rows {
		head := "  "
		if i == cursor {
			head = glyphCursor + " "
		}
		label := r.label
		if pad := overlayLabelWidth - lipgloss.Width(label); pad > 0 {
			label += strings.Repeat(" ", pad)
		}
		body := padTo(truncate("  "+head+r.mark+label+r.detail, width), width)
		if i == cursor {
			out = append(out, overlaySelStyle.Render(body))
			continue
		}
		out = append(out, dimStyle.Render(body))
	}
	if footer != "" {
		out = append(out, padTo(truncate("  "+footer, width), width))
	}
	return strings.Join(out, "\n")
}

func padTo(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func (m *model) overlayWidth() int {
	if m.width < 1 {
		return 0
	}
	return m.width
}

// The overlays used to draw a fixed six to ten rows whatever the window was, so
// a palette of thirty-five verbs showed six of them on a forty-row terminal and
// the rest were reached one arrow press at a time. The count now follows the
// window, bounded so a short terminal still keeps a transcript above the list and
// a tall one does not turn a menu into a page.
const (
	overlayRowsMin   = 5
	overlayRowsMax   = 12
	overlayRowsShare = 3
)

func (m *model) overlayRows() int {
	rows := m.height / overlayRowsShare
	if rows > overlayRowsMax {
		rows = overlayRowsMax
	}
	if rows < overlayRowsMin {
		rows = overlayRowsMin
	}
	return rows
}

// overlayJump is the distance Home and End move: further than any list is long,
// clamped by the caller's own clamp to the first or last row.
const overlayJump = 1 << 30

// overlayNav applies the navigation keys every windowed overlay shares, and
// reports whether the key was one of them. Arrows alone left a long list a
// thirty-press walk; a page and an end are what makes it browsable.
func (m *model) overlayNav(msg tea.KeyMsg, move func(int)) bool {
	switch msg.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		move(-1)
	case tea.KeyDown, tea.KeyCtrlN:
		move(1)
	case tea.KeyPgUp:
		move(-m.overlayRows())
	case tea.KeyPgDown:
		move(m.overlayRows())
	case tea.KeyHome:
		move(-overlayJump)
	case tea.KeyEnd:
		move(overlayJump)
	default:
		return false
	}
	return true
}
