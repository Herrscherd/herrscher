// Package shellquote writes values a POSIX shell reads back as one literal word.
package shellquote

import "strings"

// Quote returns s between single quotes, so a POSIX shell reads it as one word with nothing expanded.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Join quotes each word of argv and joins them with spaces, so a POSIX shell reads back the same argv.
func Join(argv []string) string {
	quoted := make([]string, len(argv))
	for i, word := range argv {
		quoted[i] = Quote(word)
	}
	return strings.Join(quoted, " ")
}
