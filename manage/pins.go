package manage

import (
	"fmt"
	"sort"
	"strings"
)

// pinFile sits beside plugins.go rather than in the state directory: a pin
// describes the code that gets compiled, not the machine that runs it, so it
// belongs to the composition and should show up in a review of it.
const pinFile = ".herrscher-pins"

// readPins parses pin-file text into the set of pinned module paths. It takes
// text rather than a path so the format is testable without a filesystem.
//
// A malformed line is an error rather than a skip: silently ignoring it would
// silently un-pin a module, which is the one failure a pin exists to prevent.
func readPins(src string) (map[string]bool, error) {
	pins := map[string]bool{}
	for i, line := range strings.Split(src, "\n") {
		mod := strings.TrimSpace(line)
		if mod == "" || strings.HasPrefix(mod, "#") {
			continue
		}
		if strings.ContainsAny(mod, " \t") {
			return nil, fmt.Errorf("%s line %d: not a module path: %q", pinFile, i+1, mod)
		}
		pins[mod] = true
	}
	return pins, nil
}

// writePins renders the pinned set back to pin-file text, sorted so the file
// does not churn in a diff when the set is unchanged. Versions are deliberately
// absent: go.mod already holds them, and a second copy could disagree.
func writePins(pins map[string]bool) string {
	mods := make([]string, 0, len(pins))
	for m, pinned := range pins {
		if pinned {
			mods = append(mods, m)
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	for _, m := range mods {
		b.WriteString(m)
		b.WriteByte('\n')
	}
	return b.String()
}
