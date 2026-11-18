package tui

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func (tb *tab) appendChunk(text string) bool {
	if count := len(tb.entries); count > 0 && tb.entries[count-1].role == roleAgent && tb.entries[count-1].streaming {
		tb.entries[count-1].raw += text
		tb.entries[count-1].text += termpaint.EscapeLines(text).String()
		return true
	}
	if strings.TrimSpace(text) == "" {
		tb.pendingBlank.WriteString(text)
		return false
	}
	pending := tb.pendingBlank.String() + text
	tb.appendEntry(entry{role: roleAgent, text: dropBlankLeadingLines(pending), streaming: true})
	tb.streamedIDs = append(tb.streamedIDs, tb.entries[len(tb.entries)-1].id)
	return true
}

func (tb *tab) discardStreamed() {
	tb.endStream()
	kept := tb.entries[:0]
	for _, item := range tb.entries {
		if !slices.Contains(tb.streamedIDs, item.id) {
			kept = append(kept, item)
		}
	}
	tb.entries = kept
	tb.streamedIDs = nil
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
	if count := len(tb.entries); count > 0 && tb.entries[count-1].role == roleAgent {
		tb.entries[count-1].streaming = false
	}
}
