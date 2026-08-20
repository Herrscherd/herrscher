// Package scope answers which memory project a piece of work belongs to. The
// terminal gateway asks at launch, from the directory the operator started in;
// the bridge asks again on a session's first prompt, against the projects the
// vault already knows. Neither of those two may import the other, so the rule
// lives here, in a leaf that depends on nothing but the standard library and the
// contracts that define what a scope name looks like.
package scope

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// gitTimeout bounds the single git call ProjectFromDir makes. A launch must not
// hang because a repository's objects live on a stalled network mount.
const gitTimeout = 2 * time.Second

// maxScopeLen mirrors the length projectRe allows in core/internal/manager, so a
// name this package hands out can always be persisted as a project.
const maxScopeLen = 128

// ProjectFromDir names the memory project work done in dir belongs to: the git
// repository dir is in, folded to a single stable scope segment. It answers ""
// when dir is in no repository, or when the repository's name carries nothing
// nameable — an unscoped session, which is what every session gets today, so ""
// is never a failure, only a silence.
//
// It resolves the repository's *common* git directory rather than the worktree,
// so every worktree of one repository answers with one project.
func ProjectFromDir(dir string) string {
	if dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return ""
	}
	// git answers relative to dir when it feels like it (".git" from a repo root).
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	common = filepath.Clean(common)
	// <root>/.git → <root>. A bare repository is named by its own directory,
	// conventionally <root>.git, so drop the suffix rather than the segment.
	root := common
	if filepath.Base(common) == ".git" {
		root = filepath.Dir(common)
	} else {
		root = strings.TrimSuffix(common, ".git")
	}
	return nameOf(filepath.Base(filepath.Clean(root)))
}

// MatchProject picks, among the projects the vault already knows, the one a
// prompt is about. It answers "" when the prompt names none of them, which means
// "keep whatever the session launched with".
//
// The rule is deliberately the dullest one that can work: a known project named
// as a whole word wins, the earliest mention breaks a tie, and the longest name
// breaks a tie at the same place, so "herrscher-docs" is not read as
// "herrscher". This is the piece of the design most likely to be wrong in the
// field. It takes its whole world as arguments precisely so the vault-side
// registry that replaces it changes nothing above this line.
func MatchProject(prompt string, known []string) string {
	if prompt == "" || len(known) == 0 {
		return ""
	}
	lower := strings.ToLower(prompt)
	best, at := "", -1
	for _, k := range known {
		n := nameOf(k)
		if n == "" {
			continue
		}
		i := indexWord(lower, n)
		if i < 0 {
			continue
		}
		if at < 0 || i < at || (i == at && len(n) > len(best)) {
			best, at = n, i
		}
	}
	return best
}

// Name folds raw into the scope segment contracts would use, or "" when raw
// holds nothing nameable. Callers that take a project name from a human — an
// environment variable, a flag — run it through this so a value with a space in
// it is folded rather than refused: a typo in a setting must not be the reason a
// window opens on nothing.
func Name(raw string) string { return nameOf(raw) }

// nameOf folds raw into the scope segment contracts would use, or "" when raw
// holds nothing nameable. The empty answer matters: the normaliser's own
// fallback is the literal "scope", and a session filed under a project called
// "scope" is worse than one filed under no project at all.
func nameOf(raw string) string {
	nameable := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			nameable = true
			break
		}
	}
	if !nameable {
		return ""
	}
	n := contracts.NormalizeScope(raw)
	if len(n) > maxScopeLen {
		return ""
	}
	return n
}

// indexWord finds needle in hay on word boundaries, so "neublox" is found in
// "je bosse sur neublox" but not inside "neubloxide". It returns -1 for no match.
func indexWord(hay, needle string) int {
	for i := 0; i <= len(hay)-len(needle); {
		j := strings.Index(hay[i:], needle)
		if j < 0 {
			return -1
		}
		at := i + j
		if boundaryBefore(hay, at) && boundaryAt(hay, at+len(needle)) {
			return at
		}
		i = at + 1
	}
	return -1
}

// boundaryBefore reports whether the rune ending at byte i is a word boundary,
// and boundaryAt whether the one starting at byte i is. Both decode a whole rune
// rather than looking at one byte: the lead byte of "…" or of a non-breaking
// space is 0xE2/0xC2, which as a bare rune is a letter, and a prompt written in
// French is full of them. The start and end of the prompt count as boundaries.
func boundaryBefore(s string, i int) bool {
	if i <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return notWord(r)
}

func boundaryAt(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return notWord(r)
}

func notWord(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
