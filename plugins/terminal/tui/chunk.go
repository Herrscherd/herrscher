package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func (tb *tab) appendChunk(text string) bool {
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleAgent && tb.entries[n-1].streaming {
		tb.entries[n-1].text += text
		return true
	}
	if strings.TrimSpace(text) == "" {
		tb.pendingBlank.WriteString(text)
		return false
	}
	pending := tb.pendingBlank.String() + text
	tb.appendEntry(entry{role: roleAgent, text: dropBlankLeadingLines(pending), streaming: true})
	return true
}

func dropBlankLeadingLines(text string) string {
	body := strings.TrimLeftFunc(text, unicode.IsSpace)
	blank := text[:len(text)-len(body)]
	last := strings.LastIndexFunc(blank, isLineBreak)
	if last < 0 {
		return text
	}
	_, size := utf8.DecodeRuneInString(blank[last:])
	return text[last+size:]
}

func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', '\u0085', '\u2028', '\u2029':
		return true
	}
	return false
}

func (tb *tab) endStream() {
	tb.pendingBlank.Reset()
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleAgent {
		tb.entries[n-1].streaming = false
	}
}
