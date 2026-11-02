package manage

import (
	"fmt"
	"sort"
	"strings"
)

const pinFile = ".herrscher-pins"

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
