package manage

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
)

var errNotModulePath = errors.New(string(clitext.ManagePinsLine))

const pinFile = ".herrscher-pins"

func readPins(src string) (map[string]bool, error) {
	pins := make(map[string]bool)
	for idx, line := range strings.Split(src, "\n") {
		mod := strings.TrimSpace(line)
		if mod == "" || strings.HasPrefix(mod, "#") {
			continue
		}
		if strings.ContainsAny(mod, " \t") {
			return nil, failure(errNotModulePath, clitext.ManagePinsLine, clitext.Params{clitext.ParamFile: pinFile, clitext.ParamLine: idx + 1, clitext.ParamModule: strconv.Quote(mod)})
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
		out.WriteString(m)
		_ = out.WriteByte('\n')
	}
	return out.String()
}
