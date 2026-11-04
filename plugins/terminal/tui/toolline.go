package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func toolFamilies() map[string]string {
	return map[string]string{
		"read":         familyRead,
		"glob":         familyRead,
		"grep":         familySearch,
		"websearch":    familySearch,
		"webfetch":     familyWeb,
		"edit":         familyWrite,
		"write":        familyWrite,
		"notebookedit": familyWrite,
		"bash":         familyRun,
		"task":         familyAgent,
		"agent":        familyAgent,
		"skill":        familyAgent,
	}
}

const (
	familyRead   = "read"
	familyWrite  = "write"
	familyRun    = "run"
	familySearch = "search"
	familyWeb    = "web"
	familyAgent  = "agent"
)

func toolVerb(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", ""
	}
	head, tail, _ := strings.Cut(trimmed, " ")
	fam, ok := toolFamilies()[strings.ToLower(head)]
	if !ok {
		return "", trimmed
	}
	return fam, strings.TrimSpace(tail)
}

const familyWidth = 7

const toolTargetColumn = 2 + familyWidth

const toolLineCap = 2

const minHangWidth = 8

func renderTool(text string, width int, view view) string {
	lines := strings.Split(text, "\n")
	family, rest := toolVerb(lines[0])
	gutter := view.st.glyphs.familyGlyph(family) + " "
	if family != "" {
		gutter += family + strings.Repeat(" ", max(1, familyWidth-len(family)))
	}
	var builder strings.Builder
	builder.WriteString(toolPart(gutter, rest, view.st.tool, width, view))
	for _, line := range lines[1:] {
		builder.WriteByte('\n')
		builder.WriteString(toolPart(resultGutter(view.st.glyphs), line, view.st.dim, width, view))
	}
	return builder.String()
}

func toolPart(gutter, body string, style lipgloss.Style, width int, view view) string {
	lines := hangingLines(gutter, body, width)
	if !view.expand && len(lines) > toolLineCap {
		lines = hangingLines(gutter, abbreviatePaths(body), width)
		if len(lines) > toolLineCap {
			hidden := len(lines) - toolLineCap
			lines = lines[:toolLineCap]
			lines[len(lines)-1] = withMoreMarker(lines[len(lines)-1], hidden, width, view.st.glyphs)
		}
	}
	for i, ln := range lines {
		lines[i] = style.Render(ln)
	}
	return strings.Join(lines, "\n")
}

func hangingLines(gutter, body string, width int) []string {
	offset := lipgloss.Width(gutter)
	if width-offset < minHangWidth {
		return wrapPlain(gutter+body, width)
	}
	wrapped := wrapPlain(body, width-offset)
	out := make([]string, len(wrapped))
	for lineIndex, line := range wrapped {
		if lineIndex == 0 {
			out[lineIndex] = gutter + line
			continue
		}
		out[lineIndex] = strings.Repeat(" ", offset) + line
	}
	return out
}

func wrapPlain(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	out := strings.Split(lipgloss.NewStyle().Width(width).Render(text), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

func withMoreMarker(line string, hidden, width int, glyphs glyphSet) string {
	marker := fmt.Sprintf("+%d %s", hidden, glyphs.moreLines)
	room := width - lipgloss.Width(marker) - 1
	if room < 1 {
		return line
	}
	line = truncate(line, room)
	return line + strings.Repeat(" ", room-lipgloss.Width(line)+1) + marker
}

const (
	pathDepth = 4

	pathKeep = 2
)

func abbreviatePaths(s string) string {
	fields := strings.Split(s, " ")
	for i, f := range fields {
		fields[i] = abbreviatePath(f)
	}
	return strings.Join(fields, " ")
}

func abbreviatePath(token string) string {
	cut := len(token) - len(strings.TrimLeft(token, `"'(`))
	opening, rest := token[:cut], token[cut:]
	trimmed := strings.TrimRight(rest, `;,)"'`)
	closing := rest[len(trimmed):]

	body, ok := strings.CutPrefix(trimmed, "/")
	if !ok {
		if body, ok = strings.CutPrefix(trimmed, "~/"); !ok {
			return token
		}
	}
	segs := strings.Split(body, "/")
	if n := len(segs); n > 0 && segs[n-1] == "" {
		segs = segs[:n-1]
	}
	if len(segs) < pathDepth {
		return token
	}
	return opening + "…/" + strings.Join(segs[len(segs)-pathKeep:], "/") + closing
}

func resultGutter(glyphs glyphSet) string {
	return "  " + glyphs.result + " "
}

func resultLine(text string, style lipgloss.Style, glyphs glyphSet) string {
	return style.Render(resultGutter(glyphs) + text)
}

func thinkingLine(text string, glyphs glyphSet) string {
	return glyphs.thinking + " " + text
}
