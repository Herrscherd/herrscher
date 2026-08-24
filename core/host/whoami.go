package host

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher/core/identity"
)

// WhoamiOut answers what `herrscher whoami` prints: the identity git describes
// for the current directory, as a report or as JSON. It is exported because the
// verb is not only a daemon verb — main dispatches it locally too, so that the
// one command an operator runs when they suspect a wrong identity works whether
// or not a daemon is up. Reading git needs no daemon; requiring one would put
// the diagnostic behind the thing it is used to diagnose.
func WhoamiOut(asJSON bool) (string, error) {
	cwd, _ := os.Getwd()
	id := identity.FromDir(cwd)
	if asJSON {
		b, err := json.Marshal(id)
		return string(b), err
	}
	return whoamiReport(id), nil
}

// whoamiReport renders an identity as one line per git key, each naming the key
// it came from. A key git did not answer is printed as unset rather than
// dropped: the operator ran this to learn what herrscher believes, and a missing
// line reads as a bug in the verb rather than as an absent configuration.
//
// The value column is sized to the widest value rather than to a constant: an
// email longer than the guess would otherwise push its own source annotation out
// of line, which reads as a rendering bug in the very command someone runs to
// check whether something is broken.
func whoamiReport(id identity.Identity) string {
	if id.Empty() {
		return "git has nothing to say about you here.\n" +
			"Set it with: git config --global user.name \"Your Name\" && git config --global user.email you@example.com"
	}
	rows := []struct{ label, value, key string }{
		{"name", id.Name, "user.name"},
		{"email", id.Email, "user.email"},
		{"github", id.GitHub, "github.user"},
	}
	const unset = "\u2014"
	width := utf8.RuneCountInString(unset)
	for _, r := range rows {
		if n := utf8.RuneCountInString(r.value); n > width {
			width = n
		}
	}
	var b strings.Builder
	for _, r := range rows {
		value, note := r.value, ""
		if value == "" {
			value, note = unset, ", unset"
		}
		// %-*s counts runes, not bytes, so an accented name lines up on its own.
		fmt.Fprintf(&b, "%-7s %-*s (git config %s%s)\n", r.label, width, value, r.key, note)
	}
	return strings.TrimRight(b.String(), "\n")
}
