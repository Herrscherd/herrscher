package tui

import (
	"strings"
	"unicode/utf8"
)

const (
	asciiEllipsis   = "..."
	asciiEnterLabel = "enter"
)

func asciiSubstitutes() map[rune]string {
	return map[rune]string{
		'…': asciiEllipsis, '·': ".", '•': "*", '●': "*", '○': "o", '◆': "*", '◇': "*", '◈': "*",
		'▸': ">", '▾': "v", '▶': ">", '◂': "<", '▹': ">", '❯': ">", '⎿': "\\", '└': "\\", '├': "+",
		'✓': "v", '✔': "v", '✗': "x", '✕': "x", '⚠': "!", '⚡': "*", '✳': "*",
		'↑': "^", '↓': "v", '→': ">", '←': "<", '↵': "<", '↩': "<", '↪': ">", '⏎': "<",
		'⇧': "^", '⇥': ">", '⇞': "^", '⇟': "v", '⌃': "^", '⌥': "~", '⌫': "<", '⌕': "?", '⌖': "+",
		'−': "-", '\u2013': "-", '\u2014': "-", '‒': "-", '█': "#", '▀': "#", '▌': "|", '▍': "|", '▎': "|",
		'░': ".", '▓': "#", '▒': "#", '━': "=", '═': "=", '─': "-", '┄': "-", '┈': "-", '╌': "-",
		'│': "|", '┃': "|", '┆': "|", '║': "|", '┊': "|", '╎': "|", '┌': "+", '┐': "+",
		'╭': "+", '╮': "+", '╰': "+", '╯': "+", '┘': "+", '┬': "+", '┴': "+", '┼': "+", '┤': "+",
		'«': "<<", '»': ">>", '‹': "<", '›': ">", '’': "'", '‘': "'", '“': "\"", '”': "\"",
		' ': " ", '​': "", '‍': "", '️': "", 'œ': "oe", 'Œ': "OE", 'æ': "ae", 'Æ': "AE",
		'ß': "ss", '📎': "+", '⟲': "@",
		'é': "e", 'è': "e", 'ê': "e", 'ë': "e", 'É': "E", 'È': "E", 'Ê': "E", 'Ë': "E",
		'à': "a", 'â': "a", 'ä': "a", 'À': "A", 'Â': "A", 'Ä': "A",
		'ç': "c", 'Ç': "C", 'î': "i", 'ï': "i", 'Î': "I", 'Ï': "I",
		'ô': "o", 'ö': "o", 'Ô': "O", 'Ö': "O", 'ù': "u", 'û': "u", 'ü': "u", 'Ù': "U", 'Û': "U", 'Ü': "U",
		'ÿ': "y", 'Ÿ': "Y",
	}
}

func asciiOnly(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	substitutes := asciiSubstitutes()
	for _, character := range text {
		if character < utf8.RuneSelf {
			out.WriteRune(character)
			continue
		}
		out.WriteString(asciiSubstitute(substitutes, character))
	}
	return out.String()
}

func asciiSubstitute(substitutes map[rune]string, character rune) string {
	if sub, ok := substitutes[character]; ok {
		return sub
	}
	switch {
	case character >= 0x2800 && character <= 0x28ff:
		return "*"
	case character >= 0x2500 && character <= 0x257f:
		return "+"
	case character >= 0x2580 && character <= 0x259f:
		return "#"
	default:
		return "?"
	}
}
