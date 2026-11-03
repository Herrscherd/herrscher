package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher/core/identity"
)

// WhoamiOut answers what `herrscher whoami` prints: the identity git describes
// for the current directory, as a report or as JSON. It is exported because the
// verb is not only a daemon verb; main dispatches it locally too, so that the
// one command an operator runs when they suspect a wrong identity works whether
// or not a daemon is up. Reading git needs no daemon; requiring one would put
// the diagnostic behind the thing it is used to diagnose.
func WhoamiOut(ctx context.Context, asJSON bool) (string, error) {
	cwd, cwdErr := os.Getwd()
	logCleanup(cwdErr)
	who := identity.FromDir(ctx, cwd)
	if asJSON {
		b, err := json.Marshal(who)
		return string(b), err
	}
	return whoamiReport(who), nil
}

func whoamiReport(who identity.Identity) string {
	if who.Empty() {
		return "git has nothing to say about you here.\n" +
			"Set it with: git config --global user.name \"Your Name\" && git config --global user.email you@example.com"
	}
	rows := []struct{ label, value, key string }{
		{paramName, who.Name, "user.name"},
		{"email", who.Email, "user.email"},
		{"github", who.GitHub, "github.user"},
	}
	const unset = "-"
	width := utf8.RuneCountInString(unset)
	for _, r := range rows {
		if n := utf8.RuneCountInString(r.value); n > width {
			width = n
		}
	}
	var builder strings.Builder
	for _, row := range rows {
		value, note := row.value, ""
		if value == "" {
			value, note = unset, ", unset"
		}
		fmt.Fprintf(&builder, "%-7s %-*s (git config %s%s)\n", row.label, width, value, row.key, note)
	}
	return strings.TrimRight(builder.String(), "\n")
}
