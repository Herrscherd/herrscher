package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	statuslineFieldsKey = "fields"
	statuslineSourceKey = "source"
	configDirMode       = 0o755
	defaultConfigMode   = 0o644
)

type tomlEntry struct {
	section string
	key     string
	value   string
	first   int
	last    int
}

type tomlKeyValue struct {
	key   string
	value string
}

func scanTomlEntries(lines []string) []tomlEntry {
	var entries []tomlEntry
	section := ""
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = tomlSectionName(line)
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		entry := tomlEntry{section: section, key: strings.TrimSpace(key), first: i, last: i}
		value := strings.TrimSpace(raw)
		for strings.HasPrefix(value, "[") && !strings.Contains(value, "]") && entry.last+1 < len(lines) {
			entry.last++
			value += "\n" + lines[entry.last]
		}
		entry.value = value
		entries = append(entries, entry)
	}
	return entries
}

func tomlStringArray(raw string) ([]string, bool) {
	start := strings.IndexByte(raw, '[')
	if start < 0 {
		return nil, false
	}
	items := []string{}
	rest := raw[start+1:]
	for {
		rest = strings.TrimLeft(rest, " \t\r\n,")
		if rest == "" {
			return nil, false
		}
		switch rest[0] {
		case ']':
			return items, true
		case '#':
			newline := strings.IndexByte(rest, '\n')
			if newline < 0 {
				return nil, false
			}
			rest = rest[newline:]
		case '"':
			end := basicStringEnd(rest)
			if end < 0 {
				return nil, false
			}
			value, err := strconv.Unquote(rest[:end+1])
			if err != nil {
				return nil, false
			}
			items = append(items, value)
			rest = rest[end+1:]
		case '\'':
			end := strings.IndexByte(rest[1:], '\'')
			if end < 0 {
				return nil, false
			}
			items = append(items, rest[1:end+1])
			rest = rest[end+2:]
		default:
			return nil, false
		}
	}
}

func formatTomlStringArray(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = strconv.Quote(item)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func writeTomlKeys(text, section string, pairs ...tomlKeyValue) string {
	lines := []string{}
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	var missing []string
	for _, pair := range pairs {
		entry, found := findTomlEntry(lines, section, pair.key)
		if pair.value == "" {
			if found {
				lines = replaceLines(lines, entry.first, entry.last, nil)
			}
			continue
		}
		line := pair.key + " = " + pair.value
		if !found {
			missing = append(missing, line)
			continue
		}
		lines = replaceLines(lines, entry.first, entry.last, []string{line})
	}
	if len(missing) > 0 {
		lines = insertTomlLines(lines, section, missing)
	}
	return strings.Join(lines, "\n") + "\n"
}

func findTomlEntry(lines []string, section, key string) (tomlEntry, bool) {
	for _, entry := range scanTomlEntries(lines) {
		if entry.section == section && entry.key == key {
			return entry, true
		}
	}
	return tomlEntry{}, false
}

func replaceLines(lines []string, first, last int, block []string) []string {
	out := make([]string, 0, len(lines)-(last-first)+len(block))
	out = append(out, lines[:first]...)
	out = append(out, block...)
	return append(out, lines[last+1:]...)
}

func insertTomlLines(lines []string, section string, block []string) []string {
	header := tomlHeaderIndex(lines, section)
	if header >= 0 {
		out := make([]string, 0, len(lines)+len(block))
		out = append(out, lines[:header+1]...)
		out = append(out, block...)
		return append(out, lines[header+1:]...)
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	lines = append(lines, "["+section+"]")
	return append(lines, block...)
}

func tomlHeaderIndex(lines []string, section string) int {
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "[[") && tomlSectionName(line) == section {
			return i
		}
	}
	return -1
}

func saveStatuslineChoice(path string, bar *barState) error {
	if path == "" {
		return nil
	}
	return updateTomlFile(path, statuslineSection,
		tomlKeyValue{key: statuslineSourceKey, value: strconv.Quote(bar.config.Source)},
		tomlKeyValue{key: statuslineFieldsKey, value: formatTomlStringArray(bar.tickedIDs())},
	)
}

func updateTomlFile(path, section string, pairs ...tomlKeyValue) error {
	data, err := os.ReadFile(path)
	mode := os.FileMode(defaultConfigMode)
	switch {
	case err == nil:
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	text := writeTomlKeys(string(data), section, pairs...)
	return writeFileAtomic(path, []byte(text), mode)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".terminal-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr, os.Chmod(name, mode)); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func (b *barState) tickedIDs() []string {
	ticked := []string{}
	for _, id := range b.fieldIDs() {
		if b.isShown(id) {
			ticked = append(ticked, id)
		}
	}
	return ticked
}

func (b *barState) restoreFields(ticked []string) {
	if ticked == nil {
		return
	}
	listed := make(map[string]bool, len(ticked))
	order := make([]string, 0, len(barFieldSpecs()))
	for _, id := range ticked {
		if hasBarField(id) && !listed[id] {
			listed[id] = true
			order = append(order, id)
		}
	}
	for _, id := range defaultBarOrder() {
		if !listed[id] {
			order = append(order, id)
		}
	}
	b.order = order
	b.toggled = make(map[string]bool, len(barFieldSpecs()))
	for _, spec := range barFieldSpecs() {
		b.toggled[spec.id] = listed[spec.id]
	}
}

func hasBarField(id string) bool {
	for _, spec := range barFieldSpecs() {
		if spec.id == id {
			return true
		}
	}
	return false
}
