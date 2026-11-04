package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const mentionMax = 6

func mentionWord(input string, cursor int) (int, string, bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(input) {
		cursor = len(input)
	}
	start := strings.LastIndexAny(input[:cursor], " \t\n") + 1
	word := input[start:cursor]
	if !strings.HasPrefix(word, "@") {
		return 0, "", false
	}
	return start, word[1:], true
}

const (
	mentionDepth   = 4
	mentionEntries = 4000
)

var mentionSkipped = map[string]bool{".git": true, "node_modules": true, "vendor": true}

func mentionCandidates(dir string) []string {
	var out []string
	var walk func(rel string, depth int)
	walk = func(rel string, depth int) {
		if len(out) >= mentionEntries {
			return
		}
		ents, err := os.ReadDir(filepath.Join(dir, rel))
		if err != nil {
			return
		}
		for _, item := range ents {
			name := item.Name()
			if mentionSkipped[name] || (item.IsDir() && strings.HasPrefix(name, ".")) {
				continue
			}
			path := filepath.ToSlash(filepath.Join(rel, name))
			if item.IsDir() {
				path += "/"
			}
			out = append(out, path)
			if item.IsDir() && depth < mentionDepth {
				walk(filepath.Join(rel, name), depth+1)
			}
		}
	}
	walk("", 1)
	return out
}

func mentionMatches(dir, prefix string) []string {
	if _, err := os.ReadDir(dir); err != nil {
		return nil
	}
	candidates := mentionCandidates(dir)
	if prefix == "" {
		var top []string
		for _, c := range candidates {
			if !strings.Contains(strings.TrimSuffix(c, "/"), "/") {
				top = append(top, c)
			}
		}
		sort.Strings(top)
		return top
	}
	return rankFuzzy(prefix, candidates)
}

func completeMention(input string, cursor int, match string) (string, int) {
	start, _, ok := mentionWord(input, cursor)
	if !ok {
		return input, cursor
	}
	newWord := "@" + match
	out := input[:start] + newWord + input[cursor:]
	return out, start + len(newWord)
}

func (m *model) worktreeDir() string {
	return m.sessionDir(m.active)
}

func (m *model) mentionCursor() int {
	value := m.input.Value()
	lines := strings.Split(value, "\n")
	row := m.input.Line()
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		return len(value)
	}
	offset := 0
	for i := range row {
		offset += len(lines[i]) + 1
	}
	runes := []rune(lines[row])
	info := m.input.LineInfo()
	col := info.StartColumn + info.ColumnOffset
	if col < 0 {
		col = 0
	}
	if col > len(runes) {
		col = len(runes)
	}
	offset += len(string(runes[:col]))
	if offset > len(value) {
		return len(value)
	}
	return offset
}

func (m *model) mentionOpen() bool {
	return len(m.mentionRows()) > 0
}

func (m *model) mentionRows() []string {
	_, prefix, ok := mentionWord(m.input.Value(), m.mentionCursor())
	if !ok {
		return nil
	}
	key := m.worktreeDir() + "\x00" + prefix
	if key != m.mentionCacheKey {
		rows := mentionMatches(m.worktreeDir(), prefix)
		if len(rows) > mentionMax {
			rows = rows[:mentionMax]
		}
		m.mentionCacheKey = key
		m.mentionCacheRows = rows
	}
	return m.mentionCacheRows
}

func (m *model) clampMention() {
	count := len(m.mentionRows())
	if m.mentionIdx >= count {
		m.mentionIdx = count - 1
	}
	if m.mentionIdx < 0 {
		m.mentionIdx = 0
	}
}

func (m *model) moveMention(d int) { m.mentionIdx += d; m.clampMention() }

func (m *model) completeActiveMention() {
	rows := m.mentionRows()
	if len(rows) == 0 {
		return
	}
	if m.mentionIdx >= len(rows) {
		m.mentionIdx = len(rows) - 1
	}
	out, cur := completeMention(m.input.Value(), m.mentionCursor(), rows[m.mentionIdx])
	m.input.SetValue(out)
	m.input.SetCursor(cur)
	m.mentionIdx = 0
}

func (m *model) mentionView() string {
	paths := m.mentionRows()
	if len(paths) == 0 {
		return ""
	}
	_, prefix, _ := mentionWord(m.input.Value(), m.mentionCursor())
	rows := make([]overlayRow, 0, len(paths))
	for _, p := range paths {
		row := overlayRow{label: "@" + p}
		if hit, ok := fuzzyMatch(prefix, p); ok {
			row.hot = shiftPositions(hit.positions, len("@"))
		}
		rows = append(rows, row)
	}
	return floatingList(rows, m.mentionIdx, "Tab compléter · Échap fermer", m.overlayWidth(), m.st)
}
