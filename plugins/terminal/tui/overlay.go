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

const overlayJump = 1 << 30

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
