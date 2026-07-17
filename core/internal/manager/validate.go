package manager

import (
	"path/filepath"
	"regexp"
	"strings"
)

// sessionNameRe constrains a session name to a safe slug: it becomes both a
// filesystem path (<repo>/.herrscher-sessions/<name>) and a git branch
// (session/<name>), so anything outside this set could traverse directories or
// forge odd refs even though the caller is allowlisted.
var sessionNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// slugInvalidRe matches any run of characters that are not allowed inside a
// session slug; slugify collapses each such run to a single '-'.
var slugInvalidRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// slugify turns a free-form session name into a safe slug: lowercase, runs of
// invalid characters (spaces, punctuation, …) collapse to '-', and leading or
// trailing separators are trimmed. It returns "" when nothing usable remains
// (e.g. an all-emoji name), letting the caller emit a clear error. The result
// is always accepted by sessionNameRe, which stays as the final guard.
func slugify(name string) string {
	s := slugInvalidRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-_")
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-_")
	}
	return s
}

// projectRe constrains a workspace project name to a single safe path segment
// (no "/", no "..", no spaces), so workspace+project cannot escape the root.
var projectRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// repoFor resolves the git repo root a session operates on: the workspace root
// when no project sub-dir applies, else <workspace>/<project>. A project names a
// workspace sub-dir ONLY when a workspace root is configured; in cwd mode
// (workspace == "") the session runs in the checkout root and the project is a
// mere logical label, so it must never steer the repo path.
func repoFor(workspace, project string) string {
	if workspace == "" || project == "" {
		return workspace
	}
	return filepath.Join(workspace, project)
}
