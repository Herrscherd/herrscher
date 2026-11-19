package runner

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxRemoteStderr = 512

const truncatedMark = "… [tronqué]"

var terminalSequence = regexp.MustCompile(`\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[\x40-\x5a\x5c-\x5f]`)

func boundedStderr(raw []byte) string {
	clean := terminalSequence.ReplaceAllString(string(raw), "")
	clean = strings.Map(controlAsSpace, clean)
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

func controlAsSpace(char rune) rune {
	if unicode.IsControl(char) {
		return ' '
	}
	return char
}
