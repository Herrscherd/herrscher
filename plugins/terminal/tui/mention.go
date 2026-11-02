package tui

import (
	"os"
	"sort"
	"strings"
)

const mentionMax = 6

func mentionWord(input string, cursor int) (start int, prefix string, ok bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(input) {
		cursor = len(input)
	}
	start = strings.LastIndexAny(input[:cursor], " \t\n") + 1
	word := input[start:cursor]
	if !strings.HasPrefix(word, "@") {
		return 0, "", false
	}
	return start, word[1:], true
}

func mentionMatches(dir, prefix string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
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
	if name := m.sessionName(m.active); name != "" {
		for _, s := range m.tm.Sessions() {
			if s.Name == name && s.Dir != "" {
				return s.Dir
			}
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
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
	n := len(m.mentionRows())
	if m.mentionIdx >= n {
		m.mentionIdx = n - 1
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
	rows := make([]overlayRow, 0, len(paths))
	for _, p := range paths {
		rows = append(rows, overlayRow{label: "@" + p})
	}
	return floatingList(rows, m.mentionIdx, "Tab compléter · Échap fermer", m.overlayWidth())
}
