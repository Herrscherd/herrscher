package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Herrscherd/herrscher/core/termpaint"
	"github.com/Herrscherd/herrscher/core/usage"
)

const (
	frameEdgeLead = 1
	frameGutter   = 2
	frameSides    = 2
)

type overlayFrame struct {
	title  string
	right  string
	footer string
	body   []string
}

func (m *model) frameWidth() int {
	if m.width <= 0 {
		return defaultWidth
	}
	return m.width
}

func (m *model) renderFrame(frame overlayFrame) string {
	boxes := m.st.boxes(m.caps.Boxes)
	width := m.frameWidth()
	inner := m.frameInner()
	side := m.st.dim.Render(boxes.v)
	body := frame.body
	top, fitted := frameEdge([2]string{boxes.tl, boxes.tr}, frame.title, frame.right, width, m.st.dim, boxes.h)
	if !fitted && frame.right != "" {
		body = append([]string{frame.right}, body...)
	}
	lines := []string{top}
	gutter := strings.Repeat(" ", frameGutter)
	for _, line := range body {
		line = cutText(line, inner)
		lines = append(lines, side+gutter+termpaint.PadRight(line, inner)+gutter+side)
	}
	bottom, footerFitted := frameEdge([2]string{boxes.bl, boxes.br}, "", frame.footer, width, m.st.dim, boxes.h)
	if !footerFitted && frame.footer != "" {
		lines = append(lines, side+gutter+termpaint.PadRight(cutText(frame.footer, inner), inner)+gutter+side)
	}
	lines = append(lines, bottom)
	return strings.Join(lines, "\n")
}

func frameEdge(corners [2]string, left, right string, width int, style lipgloss.Style, horizontal string) (string, bool) {
	inner := width - composerBorderCols - frameSides*frameEdgeLead
	leftPart, rightPart := framed(left), framed(right)
	fitted := true
	if lipgloss.Width(leftPart)+lipgloss.Width(rightPart) > inner {
		rightPart = ""
		fitted = false
	}
	leftPart = cutText(leftPart, max(inner, 0))
	fill := max(inner-lipgloss.Width(leftPart)-lipgloss.Width(rightPart), 0)
	lead := style.Render(corners[0] + strings.Repeat(horizontal, frameEdgeLead))
	tail := style.Render(strings.Repeat(horizontal, frameEdgeLead) + corners[1])
	return lead + leftPart + style.Render(strings.Repeat(horizontal, fill)) + rightPart + tail, fitted
}

func framed(text string) string {
	if text == "" {
		return ""
	}
	return " " + text + " "
}

func spreadLine(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < frameGutter {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) reportPalette() termpaint.Palette {
	return termpaint.Palette{
		Accent:  styleOf(m.st.accent),
		Fill:    styleOf(m.st.accent),
		Marker:  styleOf(m.st.strong),
		Dim:     styleOf(m.st.dim),
		Strong:  styleOf(m.st.strong),
		Bad:     styleOf(m.st.red),
		Warn:    styleOf(m.st.warn),
		Good:    styleOf(m.st.green),
		Group:   styleOf(m.st.violet),
		Inverse: styleOf(m.st.overlaySel),
	}
}

func styleOf(style lipgloss.Style) termpaint.Style {
	return func(text string) string { return style.Render(text) }
}

func (m *model) frameInner() int {
	return m.frameWidth() - composerBorderCols - frameSides*frameGutter
}

func (m *model) cardEdges() usage.CardEdges {
	boxes := m.st.boxes(m.caps.Boxes)
	switch boxes.h {
	case "─":
		return usage.UnicodeEdges()
	case " ":
		return usage.CardEdges{TopLeft: " ", TopRight: " ", BottomLeft: " ", BottomRight: " ", TeeLeft: " ", TeeRight: " ", Horizontal: " ", Vertical: " "}
	}
	return usage.CardEdges{TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+", TeeLeft: "+", TeeRight: "+", Horizontal: "-", Vertical: "|"}
}
