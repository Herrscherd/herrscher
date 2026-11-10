package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var inlineMarkdown = regexp.MustCompile("\\*\\*([^*\\n]+)\\*\\*|__([^_\\n]+)__|`([^`\\n]+)`|\\[([^\\]\\n]+)\\]\\(([^)\\s]+)\\)")

func styleInlineMarkdown(text string, plain, strong, code lipgloss.Style) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = styleInlineLine(line, plain, strong, code)
	}
	return strings.Join(lines, "\n")
}

func styleInlineLine(text string, plain, strong, code lipgloss.Style) string {
	var out strings.Builder
	last := 0
	for _, match := range inlineMarkdown.FindAllStringSubmatchIndex(text, -1) {
		out.WriteString(plain.Render(text[last:match[0]]))
		switch {
		case match[2] >= 0:
			out.WriteString(strong.Render(text[match[2]:match[3]]))
		case match[4] >= 0:
			out.WriteString(strong.Render(text[match[4]:match[5]]))
		case match[6] >= 0:
			out.WriteString(code.Render(text[match[6]:match[7]]))
		default:
			out.WriteString(strong.Render(text[match[8]:match[9]]))
			out.WriteString(plain.Render(" (" + text[match[10]:match[11]] + ")"))
		}
		last = match[1]
	}
	out.WriteString(plain.Render(text[last:]))
	return out.String()
}
