package manage

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var errNotModulePath = errors.New("not a module path")

const pinFile = ".herrscher-pins"

func readPins(src string) (map[string]bool, error) {
	pins := make(map[string]bool)
	for idx, line := range strings.Split(src, "\n") {
		mod := strings.TrimSpace(line)
		if mod == "" || strings.HasPrefix(mod, "#") {
			continue
		}
		if strings.ContainsAny(mod, " \t") {
			return nil, fmt.Errorf("%s line %d: %w: %q", pinFile, idx+1, errNotModulePath, mod)
		}
		pins[mod] = true
	}
	return pins, nil
}

func writePins(pins map[string]bool) string {
	mods := make([]string, 0, len(pins))
	for module, pinned := range pins {
		if pinned {
			mods = append(mods, module)
		}
	}
	sort.Strings(mods)
	var out strings.Builder
	for _, m := range mods {
		_, _ = out.WriteString(m)
		_ = out.WriteByte('\n')
	}
	return out.String()
}
