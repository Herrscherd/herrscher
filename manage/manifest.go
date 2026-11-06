package manage

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
)

var (
	errMarkerNotFound  = clitext.SentinelError("managed region: marker not found")
	errDuplicateMarker = clitext.SentinelError("duplicate marker")
	errMarkerOrder     = clitext.SentinelError("end marker precedes begin marker")
)

const (
	beginMarker = "// herrscher:plugins"
	endMarker   = "// herrscher:end"
)

func importLine(module string) string { return fmt.Sprintf("\t_ %q", module) }

func parseModule(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "_ ") {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(trimmed[2:]), `"`), true
}

func listPlugins(src string) ([]string, error) {
	region, _, _, err := region(src)
	if err != nil {
		return nil, err
	}
	var mods []string
	for _, line := range region {
		if m, ok := parseModule(line); ok {
			mods = append(mods, m)
		}
	}
	return mods, nil
}

func addPlugin(src, module string) (string, bool, error) {
	mods, err := listPlugins(src)
	if err != nil {
		return src, false, err
	}
	for _, m := range mods {
		if m == module {
			return src, false, nil
		}
	}
	lines := strings.Split(src, "\n")
	for idx, line := range lines {
		if strings.TrimSpace(line) == endMarker {
			out := append([]string{}, lines[:idx]...)
			out = append(out, importLine(module))
			out = append(out, lines[idx:]...)
			return strings.Join(out, "\n"), true, nil
		}
	}
	return src, false, failure(errMarkerNotFound, clitext.ManageEndMarkerNotFound, clitext.Params{clitext.ParamMarker: strconv.Quote(endMarker)})
}

func removePlugin(src, module string) (string, bool, error) {
	if _, _, _, err := region(src); err != nil {
		return src, false, err
	}
	lines := strings.Split(src, "\n")
	var out []string
	changed := false
	inRegion := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case beginMarker:
			inRegion = true
		case endMarker:
			inRegion = false
		}
		if inRegion {
			if m, ok := parseModule(line); ok && m == module {
				changed = true
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), changed, nil
}

func setPlugins(src string, modules []string) (string, error) {
	lines := strings.Split(src, "\n")
	_, begin, end, err := region(src)
	if err != nil {
		return src, err
	}
	out := append([]string{}, lines[:begin+1]...)
	for _, m := range modules {
		out = append(out, importLine(m))
	}
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n"), nil
}

func region(src string) ([]string, int, int, error) {
	all := strings.Split(src, "\n")
	begin, end := -1, -1
	for idx, line := range all {
		switch strings.TrimSpace(line) {
		case beginMarker:
			if begin >= 0 {
				return nil, begin, end, failure(errDuplicateMarker, clitext.ManageDuplicateMarker, clitext.Params{clitext.ParamMarker: strconv.Quote(beginMarker)})
			}
			begin = idx
		case endMarker:
			if end >= 0 {
				return nil, begin, end, failure(errDuplicateMarker, clitext.ManageDuplicateMarker, clitext.Params{clitext.ParamMarker: strconv.Quote(endMarker)})
			}
			end = idx
		}
	}
	if begin < 0 || end < 0 {
		return nil, begin, end, failure(errMarkerNotFound, clitext.ManageRegionNotFound, nil)
	}
	if end < begin {
		return nil, begin, end, failure(errMarkerOrder, clitext.ManageMarkerOrder, nil)
	}
	return all[begin+1 : end], begin, end, nil
}
