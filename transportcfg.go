package main

import (
	"os"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// remoteCategories reads HERRSCHER_REMOTE (comma-separated category names) into
// a set. Empty/unset => all-local, today's behaviour.
func remoteCategories() map[contracts.Category]bool {
	out := map[contracts.Category]bool{}
	for _, c := range strings.Split(os.Getenv("HERRSCHER_REMOTE"), ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			out[contracts.Category(c)] = true
		}
	}
	return out
}
