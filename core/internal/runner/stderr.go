package runner

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxRemoteStderr = 512

const truncatedMark = "…"

var terminalSequence = regexp.MustCompile(`\x1b\[[\x30-\x3f]*(?:[\x40-\x7e]|[\x20-\x2f]*)|\x1b\][^\x07\x1b\n\r]*(?:\x07|\x1b\\)|\x1b\][^\x07\x1b\s]*|\x1b[\x40-\x5a\x5c-\x5f]`)

func boundedStderr(raw []byte) string {
	clean := terminalSequence.ReplaceAllString(string(raw), "")
	clean = strings.Map(cleanRune, clean)
	clean = strings.Join(strings.Fields(clean), " ")
	if len(clean) <= maxRemoteStderr {
		return clean
	}
	start := len(clean) - maxRemoteStderr
	for !utf8.RuneStart(clean[start]) {
		start++
	}
	return truncatedMark + " " + clean[start:]
}

func cleanRune(char rune) rune {
	switch {
	case isBidiControl(char):
		return -1
	case unicode.IsControl(char):
		return ' '
	}
	return char
}

func isBidiControl(char rune) bool {
	return char == '\u061c' ||
		char == '\u200e' || char == '\u200f' ||
		('\u202a' <= char && char <= '\u202e') ||
		('\u2066' <= char && char <= '\u2069')
}
