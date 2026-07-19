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

// filterCommands returns the commands whose Name contains the query (case-insensitive),
// preserving order. An empty query returns every command.
func filterCommands(cmds []CommandSpec, query string) []CommandSpec {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return cmds
	}
	var out []CommandSpec
	for _, c := range cmds {
		if strings.Contains(strings.ToLower(c.Name), query) {
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

// completePal fills the input with the selected command's name, ready for its args.
func (m *model) completePal() {
	fc := m.filtered()
	if len(fc) == 0 {
		return
	}
	if m.palIdx >= len(fc) {
		m.palIdx = len(fc) - 1
	}
	m.input.SetValue("/" + fc[m.palIdx].Name + " ")
	m.input.CursorEnd()
}

// paletteView renders the filtered command list as a bordered box; empty when closed.
func (m *model) paletteView() string {
	if !m.paletteOpen() {
		return ""
	}
	fc := m.filtered()
	var b strings.Builder
	b.WriteString(statusStyle.Render("commands"))
	if len(fc) == 0 {
		b.WriteString("\n" + statusStyle.Render("  (no match)"))
	}
	for i, c := range fc {
		if i >= paletteMax {
			b.WriteString("\n" + statusStyle.Render(fmt.Sprintf("  … +%d more", len(fc)-paletteMax)))
			break
		}
		row := "/" + c.Name
		if c.Args != "" {
			row += " " + c.Args
		}
		if i == m.palIdx {
			row = paletteSelStyle.Render(glyphYou + " " + row)
		} else {
			row = "  " + row
		}
		if c.Desc != "" {
			row += "  " + statusStyle.Render(c.Desc)
		}
		b.WriteString("\n" + row)
	}
	return paletteBorder.Render(b.String())
}

// paletteHeight is the rendered row count of the open palette (0 when closed), so
// chromeHeight can reserve space for it.
func (m *model) paletteHeight() int {
	if !m.paletteOpen() {
		return 0
	}
	return lipgloss.Height(m.paletteView())
}
