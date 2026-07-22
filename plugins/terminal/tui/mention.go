package tui

import (
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// mentionMax bounds how many path matches the @ completion shows at once.
const mentionMax = 6

// mentionWord returns the @-word ending at the cursor and its start index, with
// ok=false when the cursor is not inside an @-mention. The word runs from the
// last whitespace before the cursor up to the cursor.
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

// mentionMatches lists entries in dir whose name has the given prefix, directories
// suffixed with "/", sorted. It returns nil on any read error, so @ completion
// degrades to free text (the backend still resolves the mention) rather than
// failing when the session's worktree is unavailable.
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

// completeMention replaces the @-word under the cursor with @match, returning the
// new input and cursor position. A no-op when the cursor is not in a mention.
func completeMention(input string, cursor int, match string) (string, int) {
	start, _, ok := mentionWord(input, cursor)
	if !ok {
		return input, cursor
	}
	newWord := "@" + match
	out := input[:start] + newWord + input[cursor:]
	return out, start + len(newWord)
}

// worktreeDir resolves the active session's run directory for @ completion: the
// session's own worktree/run dir (from SessionInfo.Dir), falling back to the
// gateway process's working directory when the session inherits the launcher cwd
// (e.g. a shared terminal session) or is not yet known.
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

// mentionCursor is the composer's current byte offset, so @ detection and
// completion operate on the same position the user is editing.
func (m *model) mentionCursor() int {
	// textarea exposes a rune column on the current line; the composer is
	// single-line for mentions, so the value length up to the cursor suffices.
	return len(m.input.Value())
}

// mentionOpen reports whether the composer word under the cursor is an @-mention
// with at least one path match — the inline completion list is shown only then.
func (m *model) mentionOpen() bool {
	return len(m.mentionRows()) > 0
}

// mentionRows are the current @ completion matches (nil when not in a mention).
// It is called several times per render, so the underlying directory listing is
// memoized on the model keyed by dir+prefix — the ReadDir runs at most once per
// distinct (dir, prefix) rather than once per call.
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

// clampMention keeps the selection index within the current match set.
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

// completeActiveMention replaces the @-word with the selected match as plain
// text (the backend resolves @ mentions itself; the TUI reads no file contents).
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

// mentionView renders the @ completion list, reusing the inline menu style: the
// selected path prefixed ❯ in the warm accent, the rest dim, no border.
func (m *model) mentionView() string {
	rows := m.mentionRows()
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	for i, p := range rows {
		var row string
		if i == m.mentionIdx {
			row = warmStyle.Render(glyphCursor + " @" + p)
		} else {
			row = dimStyle.Render("  @" + p)
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(row)
	}
	return b.String()
}

// mentionHeight is the rendered row count of the open @ list (0 when closed).
func (m *model) mentionHeight() int {
	if !m.mentionOpen() {
		return 0
	}
	return lipgloss.Height(m.mentionView())
}
