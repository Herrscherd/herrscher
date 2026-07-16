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

// maxOutputRunes caps surfaced output. Beyond it the tail is dropped with a
// marker: tool errors put the useful message first, so keeping the head bounds
// the size without losing the reason.
const maxOutputRunes = 4096

const mask = "REDACTED"

// rule pairs a secret-shaped pattern with its replacement. Replacements keep any
// leading capture group ($1) so surrounding context (scheme, "Bearer ", field
// name) stays readable while the secret itself is masked.
type rule struct {
	re   *regexp.Regexp
	repl string
}

var rules = []rule{
	// scheme://user:pass@host → scheme://REDACTED@host (git remote URL userinfo)
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^\s:/@]+:[^\s/@]+@`), `${1}` + mask + `@`},
	// GitHub tokens: ghp_/gho_/ghs_/ghu_/ghr_ classic + github_pat_ fine-grained
	{regexp.MustCompile(`gh[posur]_[A-Za-z0-9]{20,}`), mask},
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), mask},
	// GitLab personal/project access token
	{regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{16,}`), mask},
	// Authorization: Bearer <token>
	{regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-~+/=]+`), `${1}` + mask},
	// token= / access_token= / password= / pwd= fields
	{regexp.MustCompile(`(?i)((?:access_)?token|password|passwd|pwd)=[^\s&"']+`), `${1}=` + mask},
}

// Output masks credential-shaped substrings in b and caps the result length. It
// trims surrounding whitespace, so callers need not TrimSpace first.
func Output(b []byte) string {
	s := strings.TrimSpace(string(b))
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	if rs := []rune(s); len(rs) > maxOutputRunes {
		s = string(rs[:maxOutputRunes]) + "… (truncated)"
	}
	return s
}
