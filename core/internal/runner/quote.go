// Package runner decides where a command runs: here, or on another machine.
// It is the only place in core that knows ssh exists.
package runner

import "strings"

// shellQuote wraps s in single quotes so a POSIX shell reads it as one literal
// word. Single quotes suppress every expansion there is, which leaves exactly
// one character to handle: the single quote itself, closed and re-opened around
// an escaped one. This is the package's security boundary. Session names,
// branch names and worktree paths all travel through a remote shell command
// line, and any of them can carry a space, a quote or a semicolon.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteArgv renders argv as a shell command line, every word quoted.
func quoteArgv(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}
