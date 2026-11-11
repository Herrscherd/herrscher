package tui

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

var inlineMarkdown = regexp.MustCompile("\\*\\*([^*\\n]+)\\*\\*|__([^_\\n]+)__|`([^`\\n]+)`|\\[([^\\]\\n]+)\\]\\(([^()\\s]+(?:\\([^()\\s]*\\)[^()\\s]*)*)\\)")

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
	for from := 0; from < len(text); {
		loc := inlineMarkdown.FindStringSubmatchIndex(text[from:])
		if loc == nil {
			break
		}
		for index := range loc {
			if loc[index] >= 0 {
				loc[index] += from
			}
		}
		if loc[4] >= 0 && !isStrongUnderscoreSpan(text, loc[0], loc[1]) {
			from = loc[0] + len("__")
			continue
		}
		out.WriteString(plain.Render(text[last:loc[0]]))
		switch {
		case loc[2] >= 0:
			out.WriteString(strong.Render(text[loc[2]:loc[3]]))
		case loc[4] >= 0:
			out.WriteString(strong.Render(text[loc[4]:loc[5]]))
		case loc[6] >= 0:
			out.WriteString(code.Render(text[loc[6]:loc[7]]))
		default:
			out.WriteString(strong.Render(text[loc[8]:loc[9]]))
			out.WriteString(plain.Render(" (" + text[loc[10]:loc[11]] + ")"))
		}
		last = loc[1]
		from = loc[1]
	}
	out.WriteString(plain.Render(text[last:]))
	return out.String()
}

func isStrongUnderscoreSpan(text string, start, end int) bool {
	if before, _ := utf8.DecodeLastRuneInString(text[:start]); start > 0 && isWordRune(before) {
		return false
	}
	after := text[end:]
	if after != "" {
		next, _ := utf8.DecodeRuneInString(after)
		if isWordRune(next) {
			return false
		}
		if strings.ContainsRune("./-\\", next) {
			beyond, _ := utf8.DecodeRuneInString(after[1:])
			if isWordRune(beyond) {
				return false
			}
		}
	}
	inner := text[start+len("__") : end-len("__")]
	return strings.ContainsFunc(inner, unicode.IsSpace)
}

func isWordRune(char rune) bool {
	return char == '_' || unicode.IsLetter(char) || unicode.IsDigit(char)
}
