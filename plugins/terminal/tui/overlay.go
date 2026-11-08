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

const (
	overlayLabelWidth = 16
	overlayLabelGap   = 2
	overlayLabelShare = 2
)

func floatingList(rows []overlayRow, cursor int, footer string, width int, styles uiStyles) string {
	if len(rows) == 0 {
		return ""
	}
	labelWidth := overlayLabelColumn(rows, width)
	out := make([]string, 0, len(rows)+1)
	for index, row := range rows {
		out = append(out, overlayLine(row, index == cursor, labelWidth, width, styles))
	}
	if footer != "" {
		out = append(out, padTo(truncate("  "+styles.dim.Render(footer), width), width))
	}
	return strings.Join(out, "\n")
}

func overlayLabelColumn(rows []overlayRow, width int) int {
	column := overlayLabelWidth
	for _, row := range rows {
		column = max(column, lipgloss.Width(row.mark+row.label)+overlayLabelGap)
	}
	return min(column, max(overlayLabelWidth, width/overlayLabelShare))
}

func overlayLine(row overlayRow, selected bool, labelWidth, width int, styles uiStyles) string {
	head, labelStyle, detailStyle := "  ", styles.text, styles.dim
	if selected {
		head = styles.glyphs.cursor + " "
		labelStyle = styles.accent.Bold(true)
		detailStyle = styles.accent
	}
	label := labelStyle.Render(row.label)
	if len(row.hot) > 0 {
		label = highlightFuzzy(row.label, row.hot, labelStyle, labelStyle.Bold(true))
	}
	label = labelStyle.Render(row.mark) + label
	if pad := labelWidth - lipgloss.Width(label); pad > 0 {
		label += strings.Repeat(" ", pad)
	}
	tail := detailStyle.Render(row.detail)
	if row.tag != "" {
		tail += "  " + styles.dim.Render(row.tag)
	}
	return padTo(truncate("  "+labelStyle.Render(head)+label+tail, width), width)
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
