package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type overlayRow struct {
	mark   string
	label  string
	detail string
	hot    []int
	tag    string
}

const overlayLabelWidth = 16

func floatingList(rows []overlayRow, cursor int, footer string, width int, styles uiStyles) string {
	if len(rows) == 0 {
		return ""
	}
	out := make([]string, 0, len(rows)+1)
	for index, row := range rows {
		head := "  "
		if index == cursor {
			head = styles.glyphs.cursor + " "
		}
		label := row.label
		if len(row.hot) > 0 {
			label = highlightFuzzy(row.label, row.hot, lipgloss.NewStyle(), styles.accent)
		}
		if pad := overlayLabelWidth - lipgloss.Width(label); pad > 0 {
			label += strings.Repeat(" ", pad)
		}
		tail := row.detail
		if row.tag != "" {
			tail += "  " + styles.dim.Render(row.tag)
		}
		body := padTo(truncate("  "+head+row.mark+label+tail, width), width)
		if index == cursor {
			out = append(out, styles.overlaySel.Render(body))
			continue
		}
		out = append(out, styles.dim.Render(body))
	}
	if footer != "" {
		out = append(out, padTo(truncate("  "+footer, width), width))
	}
	return strings.Join(out, "\n")
}

func padTo(text string, width int) string {
	if pad := width - lipgloss.Width(text); pad > 0 {
		return text + strings.Repeat(" ", pad)
	}
	return text
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

func (m *model) overlayNav(action keyAction, move func(int)) bool {
	if run, ok := m.tables().nav[action]; ok {
		return run(m, move)
	}
	return false
}

func navRuns() (map[keyAction]navRun, error) {
	return dispatchOf(
		bind[navRun](actSelectPrevious, func(_ *model, move func(int)) bool { move(-1); return true }),
		bind[navRun](actSelectNext, func(_ *model, move func(int)) bool { move(1); return true }),
		bind[navRun](actSelectPageUp, func(app *model, move func(int)) bool {
			move(-app.overlayRows())
			return true
		}),
		bind[navRun](actSelectPageDown, func(app *model, move func(int)) bool {
			move(app.overlayRows())
			return true
		}),
		bind[navRun](actSelectFirst, func(_ *model, move func(int)) bool { move(-overlayJump); return true }),
		bind[navRun](actSelectLast, func(_ *model, move func(int)) bool { move(overlayJump); return true }),
	)
}
