// Package redact scrubs credential-shaped substrings from external command
// output before it is surfaced in an error, and caps the size so a misbehaving
// tool can't flood logs. It is deliberately conservative: it masks the
// well-known secret shapes (URL userinfo, GitHub/GitLab tokens, bearer and
// password fields) and leaves the rest of the diagnostic intact, so a failing
// `git pull` / `gh` / `glab` invocation still reports something actionable.
package redact

import (
	"regexp"
	"strings"
)

const maxOutputRunes = 4096

const mask = "REDACTED"

type rule struct {
	re   *regexp.Regexp
	repl string
}

var rules = []rule{
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^\s:/@]+:[^\s/@]+@`), `${1}` + mask + `@`},
	{regexp.MustCompile(`gh[posur]_[A-Za-z0-9]{20,}`), mask},
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), mask},
	{regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{16,}`), mask},
	{regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-~+/=]+`), `${1}` + mask},
	{regexp.MustCompile(`(?i)((?:access_)?token|password|passwd|pwd)=[^\s&"']+`), `${1}=` + mask},
	{regexp.MustCompile(`(?i)\b(api_?key|key)=[^\s&"']+`), `${1}=` + mask},
	{regexp.MustCompile(`(?i)(x-api-key:\s*)[^\s"']+`), `${1}` + mask},
}

// Output masks credential-shaped substrings in b and caps the result length. It
// trims surrounding whitespace, so callers need not TrimSpace first.
func Output(b []byte) string {
	text := strings.TrimSpace(string(b))
	for _, r := range rules {
		text = r.re.ReplaceAllString(text, r.repl)
	}
	if rs := []rune(text); len(rs) > maxOutputRunes {
		text = string(rs[:maxOutputRunes]) + "… (truncated)"
	}
	return text
}
