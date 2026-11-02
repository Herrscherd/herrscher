package manage

import (
	"fmt"
	"strings"
)

const (
	beginMarker = "// herrscher:plugins"
	endMarker   = "// herrscher:end"
)

func importLine(module string) string { return fmt.Sprintf("\t_ %q", module) }

func parseModule(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "_ ") {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(s[2:]), `"`), true
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
	for i, line := range lines {
		if strings.TrimSpace(line) == endMarker {
			out := append([]string{}, lines[:i]...)
			out = append(out, importLine(module))
			out = append(out, lines[i:]...)
			return strings.Join(out, "\n"), true, nil
		}
	}
	return src, false, fmt.Errorf("end marker %q not found", endMarker)
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

func region(src string) (lines []string, begin, end int, err error) {
	all := strings.Split(src, "\n")
	begin, end = -1, -1
	for i, line := range all {
		switch strings.TrimSpace(line) {
		case beginMarker:
			if begin >= 0 {
				return nil, begin, end, fmt.Errorf("duplicate %q marker", beginMarker)
			}
			begin = i
		case endMarker:
			if end >= 0 {
				return nil, begin, end, fmt.Errorf("duplicate %q marker", endMarker)
			}
			end = i
		}
	}
	if begin < 0 || end < 0 {
		return nil, begin, end, fmt.Errorf("managed region markers not found")
	}
	if end < begin {
		return nil, begin, end, fmt.Errorf("end marker precedes begin marker")
	}
	return all[begin+1 : end], begin, end, nil
}
