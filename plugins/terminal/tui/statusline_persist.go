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
	configDirMode       = 0o750
	defaultConfigMode   = 0o644
	tempConfigPattern   = ".terminal-*.toml"
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
	for index := range lines {
		line := strings.TrimSpace(lines[index])
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
		entry := tomlEntry{section: section, key: strings.TrimSpace(key), first: index, last: index}
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

type tomlArrayStep struct {
	value    string
	hasValue bool
	rest     string
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
		if rest[0] == ']' {
			return items, true
		}
		step, ok := readTomlArrayStep(rest)
		if !ok {
			return nil, false
		}
		if step.hasValue {
			items = append(items, step.value)
		}
		rest = step.rest
	}
}

func readTomlArrayStep(rest string) (tomlArrayStep, bool) {
	switch rest[0] {
	case '#':
		return skipTomlComment(rest)
	case '"':
		return readBasicArrayString(rest)
	case '\'':
		return readLiteralArrayString(rest)
	default:
		return tomlArrayStep{}, false
	}
}

func skipTomlComment(rest string) (tomlArrayStep, bool) {
	newline := strings.IndexByte(rest, '\n')
	if newline < 0 {
		return tomlArrayStep{}, false
	}
	return tomlArrayStep{rest: rest[newline:]}, true
}

func readBasicArrayString(rest string) (tomlArrayStep, bool) {
	end := basicStringEnd(rest)
	if end < 0 {
		return tomlArrayStep{}, false
	}
	value, err := strconv.Unquote(rest[:end+1])
	if err != nil {
		return tomlArrayStep{}, false
	}
	return tomlArrayStep{value: value, hasValue: true, rest: rest[end+1:]}, true
}

func readLiteralArrayString(rest string) (tomlArrayStep, bool) {
	end := strings.IndexByte(rest[1:], '\'')
	if end < 0 {
		return tomlArrayStep{}, false
	}
	return tomlArrayStep{value: rest[1 : end+1], hasValue: true, rest: rest[end+2:]}, true
}

func formatTomlStringArray(items []string) string {
	quoted := make([]string, len(items))
	for index, item := range items {
		quoted[index] = strconv.Quote(item)
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
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "[[") && tomlSectionName(line) == section {
			return index
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
	data, mode, err := readConfigFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := writeTomlKeys(string(data), section, pairs...)
	return writeFileAtomic(path, []byte(text), mode)
}

func readConfigFile(path string) ([]byte, os.FileMode, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, defaultConfigMode, err
	}
	data, mode, readErr := readFromRoot(root, filepath.Base(path))
	return data, mode, errors.Join(readErr, root.Close())
}

func readFromRoot(root *os.Root, name string) ([]byte, os.FileMode, error) {
	info, err := root.Stat(name)
	if err != nil {
		return nil, defaultConfigMode, err
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, defaultConfigMode, err
	}
	return data, info.Mode().Perm(), nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, tempConfigPattern)
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := stageTempFile(tmp, data, mode); err != nil {
		return errors.Join(err, os.Remove(name))
	}
	if err := os.Rename(name, path); err != nil {
		return errors.Join(err, os.Remove(name))
	}
	return nil
}

func stageTempFile(tmp *os.File, data []byte, mode os.FileMode) error {
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	return errors.Join(writeErr, closeErr, os.Chmod(tmp.Name(), mode))
}

func (b *barState) tickedIDs() []string {
	ticked := []string{}
	for _, fieldID := range b.fieldIDs() {
		if b.isShown(fieldID) {
			ticked = append(ticked, fieldID)
		}
	}
	return ticked
}

func (b *barState) restoreFields(ticked []string) {
	if ticked == nil {
		return
	}
	listed := make(map[string]bool, len(barFieldSpecs()))
	order := make([]string, 0, len(barFieldSpecs()))
	for _, fieldID := range ticked {
		if hasBarField(fieldID) && !listed[fieldID] {
			listed[fieldID] = true
			order = append(order, fieldID)
		}
	}
	for _, fieldID := range defaultBarOrder() {
		if !listed[fieldID] {
			order = append(order, fieldID)
		}
	}
	b.order = order
	b.toggled = make(map[string]bool, len(barFieldSpecs()))
	for _, spec := range barFieldSpecs() {
		b.toggled[spec.id] = listed[spec.id]
	}
}

func hasBarField(fieldID string) bool {
	for _, spec := range barFieldSpecs() {
		if spec.id == fieldID {
			return true
		}
	}
	return false
}
