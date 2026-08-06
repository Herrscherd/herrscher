package tui

import (
	"encoding/json"
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

// paletteWindow is the slice of matches to draw, given the selection. The
// palette used to truncate at paletteMax from the head, which made every verb
// past the sixth unreachable: ↑↓ moved a selection nobody could see, and the
// only way to a late command was to type enough of its name to filter the
// others out — which requires already knowing it, the one thing a menu exists
// to spare you.
//
// The window slides only far enough to hold the selection, so browsing from the
// top keeps the first rows still until the cursor actually reaches the bottom
// edge. It returns the offset too, so the caller can tell which drawn row is the
// selected one.
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

// mergeCommands puts the frontend's own verbs in front of the daemon's and drops
// any duplicate the daemon also declares. The local ones used to be replaced
// outright the moment a backend advertised a single command, which is how a
// palette lost /help and /clear by connecting to a daemon.
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

// localCommands are the verbs the TUI answers itself, without the daemon: they
// open an overlay or edit the view, and there is nothing on the other side of
// the socket to run them. They lead the palette because they are the ones with
// no other way in — every daemon verb can also be typed at a shell.
//
// This list must stay in step with the branches handleEnter intercepts;
// palette_test.go asserts that each one is actually handled locally.
func localCommands() []CommandSpec {
	return []CommandSpec{
		{Name: "help", Desc: "show the shortcuts"},
		{Name: "clear", Desc: "clear this tab's transcript"},
		{Name: "session switch", Desc: "switch between live sessions"},
		{Name: "resume", Desc: "reopen an archived session"},
		{Name: "usage", Desc: "cost, tokens and context for this session"},
		{Name: "skills", Desc: "list available skills"},
		{Name: "attach", Args: "<path>", Desc: "stage a local file with the next message"},
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
	sel := m.palIdx
	if sel >= len(fc) {
		sel = len(fc) - 1
	}
	if sel < 0 {
		sel = 0
	}
	start, end := paletteWindow(len(fc), sel, paletteMax)
	var b strings.Builder
	for i, c := range fc[start:end] {
		i += start
		// The name is the thing being chosen and the args are a reminder of what
		// it will ask for; drawing both dim made every row one grey smear, and a
		// menu you have to read word by word is a menu you stop opening.
		label := "/" + c.Name
		var row string
		switch {
		case i == sel:
			row = accentStyle.Render(glyphCursor + " " + label)
		default:
			row = textStyle.Render("  " + label)
		}
		if c.Args != "" {
			row += dimStyle.Render(" " + c.Args)
		}
		if c.Desc != "" {
			row += "  " + dimStyle.Render(c.Desc)
		}
		if i > start {
			b.WriteByte('\n')
		}
		b.WriteString(row)
	}
	// With more matches than rows, the window alone gives no sense of where in
	// the list the cursor is or how much is left — so say it outright.
	if len(fc) > paletteMax {
		b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  %d/%d", sel+1, len(fc))))
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
