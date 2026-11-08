package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// CommandSpec is one operator command advertised in the palette. Name is the
// verb path without the leading slash (e.g. "session create").
type CommandSpec struct {
	Name string
	Args string
	Desc string
	PR   string
}

const (
	flagSeparator  = " --"
	cmdSkills      = "skills"
	cmdTheme       = "theme"
	cmdKeybindings = "keybindings"
	cmdQueue       = "queue"
	cmdPlan        = "plan"
)

func commandQuery(query string) string {
	if at := strings.Index(query, flagSeparator); at >= 0 {
		query = query[:at]
	}
	return strings.TrimSpace(strings.ToLower(query))
}

func filterCommands(cmds []CommandSpec, query string) []CommandSpec {
	query = commandQuery(query)
	if query == "" {
		return cmds
	}
	type scored struct {
		cmd   CommandSpec
		score int
	}
	var found []scored
	for _, cmd := range cmds {
		if hit, ok := fuzzyMatch(query, cmd.Name); ok {
			found = append(found, scored{cmd: cmd, score: hit.score})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	out := make([]CommandSpec, 0, len(found))
	for _, f := range found {
		out = append(out, f.cmd)
	}
	return out
}

func shiftPositions(positions []int, offset int) []int {
	out := make([]int, len(positions))
	for index, position := range positions {
		out[index] = position + offset
	}

	return out
}

func paletteWindow(count, sel, rows int) (from, to int) {
	if count <= rows {
		return 0, count
	}
	start := sel - rows + 1
	if start < 0 {
		start = 0
	}
	if start > count-rows {
		start = count - rows
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
	count := len(m.filtered())
	if m.palIdx >= count {
		m.palIdx = count - 1
	}
	if m.palIdx < 0 {
		m.palIdx = 0
	}
}

func (m *model) movePal(d int) { m.palIdx += d; m.clampPal() }

func (m *model) completePal() {
	filtered := m.filtered()
	if len(filtered) == 0 {
		return
	}
	if m.palIdx >= len(filtered) {
		m.palIdx = len(filtered) - 1
	}
	cmd := filtered[m.palIdx]
	val := "/" + cmd.Name + " "
	if flag := strings.Fields(cmd.Args); len(flag) > 0 && strings.HasPrefix(flag[0], "--") {
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
	for _, text := range specs {
		if len(text.Path) == 0 || (allow != nil && !allow(text.Path[0])) {
			continue
		}
		spec := CommandSpec{Name: strings.Join(text.Path, " "), Desc: text.Help}
		var args []string
		for _, param := range text.Params {
			if param.Required {
				args = append(args, "--"+param.Name+" <"+param.Name+">")
			}
		}
		spec.Args = strings.Join(args, " ")
		out = append(out, spec)
	}
	return out, nil
}

func mergeCommands(local, remote []CommandSpec) []CommandSpec {
	seen := make(map[string]bool, len(local))
	out := make([]CommandSpec, 0, len(local)+len(remote))
	for _, cmd := range local {
		seen[cmd.Name] = true
		out = append(out, cmd)
	}
	for _, cmd := range remote {
		if !seen[cmd.Name] {
			out = append(out, cmd)
		}
	}
	return out
}

const cmdAccount = "account"

func localCommands(trans translator) []CommandSpec {
	return []CommandSpec{
		{Name: "help", Desc: trans.t("tui.palette.cmd.help", nil)},
		{Name: "clear", Desc: trans.t("tui.palette.cmd.clear", nil)},
		{Name: "session switch", Desc: trans.t("tui.palette.cmd.session_switch", nil)},
		{Name: "resume", Desc: trans.t("tui.palette.cmd.resume", nil)},
		{Name: "agents", Desc: trans.t("tui.palette.cmd.agents", nil)},
		{Name: verbRename, Args: "<nom>", Desc: trans.t("tui.palette.cmd.rename", nil)},
		{Name: verbHandoff, Args: "<nom>", Desc: trans.t("tui.palette.cmd.handoff", nil)},
		{Name: "usage", Desc: trans.t("tui.palette.cmd.usage", nil)},
		{Name: "context", Desc: trans.t("tui.palette.cmd.context", nil)},
		{Name: "health", Desc: trans.t("tui.palette.cmd.health", nil)},
		{Name: "notify", Desc: trans.t("tui.palette.cmd.notify", nil)},
		{Name: cmdSkills, Desc: trans.t("tui.palette.cmd.skills", nil)},
		{Name: cmdTheme, Desc: trans.t("tui.palette.cmd.theme", nil)},
		{Name: "capabilities", Desc: trans.t("tui.palette.cmd.capabilities", nil)},
		{Name: "plugins", Desc: trans.t("tui.palette.cmd.plugins", nil)},
		{Name: cmdAccount, Args: "[profil] [fresh]", Desc: trans.t("tui.palette.cmd.account", nil)},
		{Name: "attach", Args: "<path>", Desc: trans.t("tui.palette.cmd.attach", nil)},
		{Name: "copy", Args: "[reply|turn|all]", Desc: trans.t("tui.palette.cmd.copy", nil)},
		{Name: "review", Desc: trans.t("tui.palette.cmd.review", nil)},
		{Name: "mouse", Desc: trans.t("tui.palette.cmd.mouse", nil)},
		{Name: cmdKeybindings, Desc: trans.t("tui.palette.cmd.keybindings", nil)},
		{Name: "statusline", Desc: trans.t("tui.palette.cmd.statusline", nil)},
		{Name: cmdQueue, Desc: trans.t("tui.palette.cmd.queue", nil), PR: "PR 2"},
	}
}

func (m *model) paletteView() string {
	if !m.paletteOpen() {
		return ""
	}
	filtered := m.filtered()
	if len(filtered) == 0 {
		return m.st.dim.Render("  (" + m.tr.t("tui.palette.no_match", nil) + ")")
	}
	sel := m.palIdx
	if sel >= len(filtered) {
		sel = len(filtered) - 1
	}
	if sel < 0 {
		sel = 0
	}
	visible := m.overlayRows()
	start, end := paletteWindow(len(filtered), sel, visible)
	rows := make([]overlayRow, 0, end-start)
	for _, cmd := range filtered[start:end] {
		label := "/" + cmd.Name
		if cmd.Args != "" {
			label += " " + cmd.Args
		}
		row := overlayRow{label: label, detail: cmd.Desc, tag: cmd.PR}
		if hit, ok := fuzzyMatch(commandQuery(m.input.Value()[1:]), cmd.Name); ok {
			row.hot = shiftPositions(hit.positions, len("/"))
		}
		rows = append(rows, row)
	}
	footer := ""
	if len(filtered) > visible {
		footer = fmt.Sprintf("%d/%d", sel+1, len(filtered))
	}
	return floatingList(rows, sel-start, footer, m.overlayWidth(), m.st)
}
