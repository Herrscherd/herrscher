package tui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CommandSpec is one operator command advertised in the palette. Name is the
// verb path without the leading slash (e.g. "session create").
type CommandSpec struct {
	Name string
	Args string
	Desc string
}

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

func paletteWindow(n, sel, rows int) (start, end int) {
	if n <= rows {
		return 0, n
	}
	start = sel - rows + 1
	if start < 0 {
		start = 0
	}
	if start > n-rows {
		start = n - rows
	}
	return start, start + rows
}

func (m *model) paletteOpen() bool { return strings.HasPrefix(m.input.Value(), "/") }

func (m *model) filtered() []CommandSpec {
	if !m.paletteOpen() {
		return nil
	}
	return filterCommands(m.cmds, strings.TrimPrefix(m.input.Value(), "/"))
}

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

// ParseCommandSpecs turns the daemon's `commands --json` answer into palette
// entries, keeping only those whose leading verb allow accepts (a nil allow
// keeps everything). A frontend that asks the daemon what it dispatches cannot
// fall behind it, which a list re-typed here always eventually does.
//
// The required params become the entry's Args, in declaration order, since those
// are what the operator must supply; the optional ones are many (session create
// alone declares twenty) and would make every row unreadable.
func ParseCommandSpecs(raw []byte, allow func(verb string) bool) ([]CommandSpec, error) {
	var specs []struct {
		Path   []string `json:"path"`
		Help   string   `json:"help"`
		Params []struct {
			Name     string `json:"Name"`
			Required bool   `json:"Required"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &specs); err != nil {
		return nil, err
	}
	out := make([]CommandSpec, 0, len(specs))
	for _, s := range specs {
		if len(s.Path) == 0 || (allow != nil && !allow(s.Path[0])) {
			continue
		}
		c := CommandSpec{Name: strings.Join(s.Path, " "), Desc: s.Help}
		var args []string
		for _, p := range s.Params {
			if p.Required {
				args = append(args, "--"+p.Name+" <"+p.Name+">")
			}
		}
		c.Args = strings.Join(args, " ")
		out = append(out, c)
	}
	return out, nil
}

func mergeCommands(local, remote []CommandSpec) []CommandSpec {
	seen := make(map[string]bool, len(local))
	out := make([]CommandSpec, 0, len(local)+len(remote))
	for _, c := range local {
		seen[c.Name] = true
		out = append(out, c)
	}
	for _, c := range remote {
		if !seen[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

func localCommands() []CommandSpec {
	return []CommandSpec{
		{Name: "help", Desc: "show the shortcuts"},
		{Name: "clear", Desc: "clear this tab's transcript"},
		{Name: "session switch", Desc: "switch between live sessions"},
		{Name: "resume", Desc: "reopen an archived session"},
		{Name: "rename", Args: "<nom>", Desc: "rename this session"},
		{Name: "usage", Desc: "cost, tokens and context for this session"},
		{Name: "skills", Desc: "list available skills"},
		{Name: "capabilities", Desc: "what this terminal can render, and what it falls back to"},
		{Name: "plugins", Desc: "the plugins this binary is made of"},
		{Name: "attach", Args: "<path>", Desc: "stage a local file with the next message"},
		{Name: "copy", Args: "[reply|turn|all]", Desc: "put the last answer (or the turn, or everything) on the clipboard"},
		{Name: "mouse", Desc: "hand the mouse to the terminal so its own selection works (ctrl+g)"},
	}
}

func (m *model) paletteView() string {
	if !m.paletteOpen() {
		return ""
	}
	fc := m.filtered()
	if len(fc) == 0 {
		return dimStyle.Render("  (aucune correspondance)")
	}
	sel := m.palIdx
	if sel >= len(fc) {
		sel = len(fc) - 1
	}
	if sel < 0 {
		sel = 0
	}
	visible := m.overlayRows()
	start, end := paletteWindow(len(fc), sel, visible)
	rows := make([]overlayRow, 0, end-start)
	for _, c := range fc[start:end] {
		label := "/" + c.Name
		if c.Args != "" {
			label += " " + c.Args
		}
		rows = append(rows, overlayRow{label: label, detail: c.Desc})
	}
	footer := ""
	if len(fc) > visible {
		footer = fmt.Sprintf("%d/%d", sel+1, len(fc))
	}
	return floatingList(rows, sel-start, footer, m.overlayWidth())
}
