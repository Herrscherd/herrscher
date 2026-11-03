// Package identity answers who the human at the keyboard is, by asking the one
// thing on the machine that already knows: git's own config. It is the twin of
// core/scope. That package names the project a directory belongs to, this one
// names the person working in it, and it holds the same two rules. It depends
// on nothing but the standard library, and a git that is absent, broken or
// simply unconfigured produces a silence rather than an error, because not
// knowing who someone is has never been a reason to refuse to run.
package identity

import (
	"context"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

const gitTimeout = 2 * time.Second

// Identity is what git knows about the human at the keyboard. Every field is
// optional; the zero value means git had nothing to say.
type Identity struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	GitHub string `json:"github"`
}

// FromDir reads the identity git would use for a commit made in dir.
//
// It asks `git config --get`, and asks it from within dir, so precedence is
// git's answer rather than ours: a repository-local user.email wins over the
// global one exactly as it would on a commit. That is what lets this feature
// need no configuration of its own; an operator who has already told git who
// they are has already told herrscher.
//
// A dir that is no repository is not a silence: git falls back to the global
// config, and answers. That is deliberate. A session started somewhere that
// happens not to be a checkout still has a human running it, and refusing to
// name them there would make the feature absent exactly where the operator has
// no repository-local config to lean on.
//
// The GitHub id is read from github.user, the key gh and hub write. It is
// deliberately NOT derived from the origin remote: a remote's owner is
// frequently an organisation rather than the human, so deriving it would
// manufacture a wrong identity wearing the shape of a right one.
func FromDir(ctx context.Context, dir string) Identity {
	if dir == "" {
		return Identity{}
	}
	return Identity{
		Name:   gitConfig(ctx, dir, "user.name"),
		Email:  gitConfig(ctx, dir, "user.email"),
		GitHub: gitConfig(ctx, dir, "github.user"),
	}
}

func gitConfig(ctx context.Context, dir, key string) string {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "config", "--get", key).Output()
	if err != nil {
		return ""
	}
	return flatten(string(out))
}

func flatten(v string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, v))
}

// Empty reports whether git answered nothing at all.
func (i Identity) Empty() bool { return i.Name == "" && i.Email == "" && i.GitHub == "" }

// String renders the identity on one line, omitting the parts git did not
// answer: "Nom <mail> (@github)". An empty Identity renders as "", so a caller
// can test the rendering rather than the struct when that is what it appends.
func (i Identity) String() string {
	parts := make([]string, 0, 3)
	if i.Name != "" {
		parts = append(parts, i.Name)
	}
	if i.Email != "" {
		parts = append(parts, "<"+i.Email+">")
	}
	if i.GitHub != "" {
		parts = append(parts, "(@"+i.GitHub+")")
	}
	return strings.Join(parts, " ")
}
