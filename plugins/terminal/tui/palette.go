package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CommandSpec is one operator command advertised in the palette. Name is the
// verb path without the leading slash (e.g. "session create").
type CommandSpec struct {
	Name string
	Args string
	Desc string
}

// filterCommands returns the palette matches for a query (case-insensitive),
// preserving order. An empty query returns every command. A command matches
// when its name contains the query (browsing) or the query has advanced into
// that command's arguments (so a fully-typed command keeps highlighting its
// own row instead of collapsing to "no match").
func filterCommands(cmds []CommandSpec, query string) []CommandSpec {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return cmds
	}
	var out []CommandSpec
	for _, c := range cmds {
		name := strings.ToLower(c.Name)
		if strings.Contains(name, query) || strings.HasPrefix(query, name+" ") {
			out = append(out, c)
		}
	}
	return out
}

// paletteMax bounds how many matches the palette shows at once.
const paletteMax = 6

// paletteOpen reports whether the input is a slash-command in progress.
func (m *model) paletteOpen() bool { return strings.HasPrefix(m.input.Value(), "/") }

// filtered returns the palette matches for the current input (nil when closed).
func (m *model) filtered() []CommandSpec {
	if !m.paletteOpen() {
		return nil
	}
	return filterCommands(m.cmds, strings.TrimPrefix(m.input.Value(), "/"))
}

// clampPal keeps the selection index within the current match set.
func (m *model) clampPal() {
	n := len(m.filtered())
	if m.palIdx >= n {
		m.palIdx = n - 1
	}
	if m.palIdx < 0 {
		m.palIdx = 0
	}
}

func (m *model) movePal(d int) { m.palIdx += d; m.clampPal() }

// completePal fills the input with the selected command, pre-seeding its first
// flag (e.g. "/session create --name ") so a discovering operator lands on the
// value to type rather than having to know the flag name.
func (m *model) completePal() {
	fc := m.filtered()
	if len(fc) == 0 {
		return
	}
	if m.palIdx >= len(fc) {
		m.palIdx = len(fc) - 1
	}
	c := fc[m.palIdx]
	val := "/" + c.Name + " "
	if flag := strings.Fields(c.Args); len(flag) > 0 && strings.HasPrefix(flag[0], "--") {
		val += flag[0] + " "
	}
	m.input.SetValue(val)
	m.input.CursorEnd()
}

// defaultCommands is the Claude-parity slash-command set the palette seeds with
// when the backend advertises none: the conversation/session verbs a user who
// knows Claude Code reaches for. The backend's own command list, when present,
// takes precedence (see newModel).
func defaultCommands() []CommandSpec {
	return []CommandSpec{
		{Name: "clear", Desc: "clear the conversation"},
		{Name: "help", Desc: "show shortcuts"},
		{Name: "session switch", Args: "<name>", Desc: "switch session"},
		{Name: "session create", Args: "--name", Desc: "start a session"},
		{Name: "resume", Args: "<name>", Desc: "reopen a session"},
		{Name: "skills", Desc: "list available skills"},
	}
}

// paletteView renders the filtered command list as an inline Claude menu: rows
// directly under the input, the selected row prefixed ❯ in the warm accent,
// descriptions dim, no border box. Empty when closed.
func (m *model) paletteView() string {
	if !m.paletteOpen() {
		return ""
	}
	fc := m.filtered()
	if len(fc) == 0 {
		return dimStyle.Render("  (no match)")
	}
	var b strings.Builder
	for i, c := range fc {
		if i >= paletteMax {
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … +%d more", len(fc)-paletteMax)))
			break
		}
		label := "/" + c.Name
		if c.Args != "" {
			label += " " + c.Args
		}
		var row string
		if i == m.palIdx {
			row = warmStyle.Render(glyphCursor + " " + label)
		} else {
			row = dimStyle.Render("  " + label)
		}
		if c.Desc != "" {
			row += "  " + dimStyle.Render(c.Desc)
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(row)
	}
	return b.String()
}

// paletteHeight is the rendered row count of the open palette (0 when closed), so
// chromeHeight can reserve space for it.
func (m *model) paletteHeight() int {
	if !m.paletteOpen() {
		return 0
	}
	return lipgloss.Height(m.paletteView())
}
