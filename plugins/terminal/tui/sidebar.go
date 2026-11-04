package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type sidebarMode int

const (
	sidebarAuto sidebarMode = iota
	sidebarShown
	sidebarHidden
)

const (
	sidebarAutoWidth     = 110
	sidebarMinCols       = 34
	sidebarMaxCols       = 40
	sidebarShortRows     = 24
	sidebarTodoRows      = 6
	sidebarAgentRows     = 4
	sidebarFileRows      = 6
	sidebarSeparatorCols = 1
	sidebarIndent        = " "
)

func (m *model) sidebarVisible() bool {
	if m.innerWidth() < narrowStatus {
		return false
	}
	switch m.sidebar {
	case sidebarShown:
		return true
	case sidebarHidden:
		return false
	}
	return m.innerWidth() >= sidebarAutoWidth
}

func (m *model) toggleSidebar() {
	if m.sidebarVisible() {
		m.sidebar = sidebarHidden
	} else {
		m.sidebar = sidebarShown
	}
	m.relayout()
}

func (m *model) sidebarWidth() int {
	if !m.sidebarVisible() {
		return 0
	}
	return min(max(m.innerWidth()/3, sidebarMinCols), sidebarMaxCols)
}

func (m *model) transcriptWidth() int {
	if !m.sidebarVisible() {
		return m.innerWidth()
	}
	return m.innerWidth() - m.sidebarWidth() - sidebarSeparatorCols
}

func (m *model) sidebarPane(height int) []string {
	if height < 1 || !m.sidebarVisible() {
		return nil
	}
	width := m.sidebarWidth()
	var body []string
	if tab := m.tabs[m.active]; tab != nil {
		body = m.sidebarBody(tab, width)
	}
	return fitColumn(body, height, width)
}

func (m *model) joinSidebar(body string, side []string) string {
	width := m.transcriptWidth()
	sep := m.st.dim.Render(glyphFilet)
	lines := strings.Split(body, "\n")
	for index := range lines {
		right := ""
		if index < len(side) {
			right = side[index]
		}
		lines[index] = padCells(truncate(lines[index], width), width) + sep + right
	}
	return strings.Join(lines, "\n")
}

func (m *model) sidebarBody(tab *tab, width int) []string {
	info, _ := m.activeInfo()
	short := m.height < sidebarShortRows
	sections := [][]string{
		m.sessionSection(tab, info, m.gitStateOf(info.Dir).branch, width),
		m.todoSection(tab, width, short),
		m.agentSection(tab, width, time.Now(), short),
		m.filesSection(tab, info.Dir, width, short),
		m.contextSection(tab, info, width, short),
	}
	var out []string
	for _, section := range sections {
		if len(section) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, section...)
	}
	return out
}

func (m *model) sessionSection(tab *tab, info contracts.SessionInfo, branch string, width int) []string {
	title := m.st.text.Bold(true).Render(tab.label)
	if !tab.openedAt.IsZero() {
		title += m.st.dim.Render(" · " + formatDuration(time.Since(tab.openedAt)))
	}
	lines := []string{m.sideHead("Session", "", width), sideRow(title, width)}
	var where []string
	if info.Project != "" {
		where = append(where, m.st.text.Render(info.Project))
	}
	if branch != "" {
		where = append(where, m.st.accent.Render(branch))
	}
	if len(where) > 0 {
		lines = append(lines, sideRow(strings.Join(where, m.st.dim.Render(" : ")), width))
	}
	if info.Dir != "" {
		lines = append(lines, sideRow(m.st.dim.Render("worktree ")+m.st.text.Render(shortPath(info.Dir)), width))
	}
	if len(info.Gateways) > 0 {
		lines = append(lines, sideRow(m.st.dim.Render("via ")+m.st.text.Render(strings.Join(info.Gateways, ", ")), width))
	}
	return lines
}

func (m *model) todoSection(tab *tab, width int, short bool) []string {
	if len(tab.todos) == 0 {
		return nil
	}
	done, active := 0, 0
	for index, todo := range tab.todos {
		switch todo.State {
		case stateDone:
			done++
		case "active":
			active = index
		}
	}
	lines := []string{m.sideHead("Todos", fmt.Sprintf("%d/%d", done, len(tab.todos)), width)}
	if short {
		return lines
	}
	start, end := paletteWindow(len(tab.todos), active, sidebarTodoRows)
	for _, todo := range tab.todos[start:end] {
		lines = append(lines, m.todoRow(todo, width))
	}
	return appendHidden(m, lines, len(tab.todos)-(end-start), width)
}

func (m *model) todoRow(todo contracts.TodoItem, width int) string {
	text := oneLine(todo.Text)
	switch todo.State {
	case stateDone:
		return sideRow(m.st.green.Render(glyphDone)+" "+m.st.dim.Render(text), width)
	case "active":
		return sideRow(m.spinFrame()+" "+m.st.text.Bold(true).Render(text), width)
	}
	return sideRow(m.st.dim.Render(glyphOpen)+" "+m.st.text.Render(text), width)
}

func (m *model) agentSection(tab *tab, width int, now time.Time, short bool) []string {
	agents := append(append([]liveAgent(nil), tab.agents...), tab.agentsDone...)
	if len(agents) == 0 {
		return nil
	}
	lines := []string{m.sideHead("Sub-agents", fmt.Sprintf("%d active", len(tab.agents)), width)}
	if short {
		return lines
	}
	shown := min(len(agents), sidebarAgentRows)
	for _, agent := range agents[:shown] {
		lines = append(lines, m.agentRow(agent, width, now))
	}
	return appendHidden(m, lines, len(agents)-shown, width)
}

func (m *model) agentRow(agent liveAgent, width int, now time.Time) string {
	glyph, style := m.agentGlyph(agent.State)
	name := oneLine(agent.Name)
	if name == "" {
		name = agent.ID
	}
	end := now
	if !agent.ended.IsZero() {
		end = agent.ended
	}
	right := elapsed(end.Sub(agent.since))
	if kind := oneLine(agent.Kind); kind != "" {
		right = kind + " " + right
	}
	left := sidebarIndent + style.Render(glyph) + " " + m.st.text.Render(name)
	return justifyRow(left, m.st.dim.Render(right), width)
}

func (m *model) agentGlyph(state string) (string, lipgloss.Style) {
	switch state {
	case stateDone:
		return glyphDone, m.st.green
	case stateFailed, stateError:
		return glyphFailed, m.st.red
	}
	return m.spinFrame(), m.st.spinner
}

func (m *model) filesSection(tab *tab, dir string, width int, short bool) []string {
	if len(tab.files) == 0 {
		return nil
	}
	stats := m.gitStateOf(dir).changes
	added, removed, known := 0, 0, 0
	for _, file := range tab.files {
		if stat, ok := fileStat(stats, file, dir); ok {
			added += stat.added
			removed += stat.removed
			known++
		}
	}
	extra := fmt.Sprintf("%d files", len(tab.files))
	if known > 0 {
		extra = fmt.Sprintf("+%d %s%d", added, glyphMinus, removed)
	}
	head := m.sideHead("Files", extra, width)
	if short {
		return []string{head}
	}
	lines := []string{head}
	shown := min(len(tab.files), sidebarFileRows)
	for _, file := range tab.files[:shown] {
		left := sidebarIndent + m.st.warn.Render("M") + " " + m.st.text.Render(relativeTo(dir, file))
		right := ""
		if stat, ok := fileStat(stats, file, dir); ok {
			right = m.st.green.Render(fmt.Sprintf("+%d", stat.added)) + " " + m.st.red.Render(glyphMinus+strconv.Itoa(stat.removed))
		}
		lines = append(lines, justifyRow(left, right, width))
	}
	return appendHidden(m, lines, len(tab.files)-shown, width)
}

func (m *model) contextSection(tab *tab, info contracts.SessionInfo, width int, short bool) []string {
	used := m.contextTokens(tab)
	if used <= 0 {
		head := m.sideHead("Context", "", width)
		if short {
			return []string{head}
		}
		return []string{head, sideRow(m.st.dim.Render("no measurement yet"), width)}
	}
	limit := contextLimit(info.Vendor)
	ratio := contextRatio(used, limit)
	style := palierStyle(ratio, m.st)
	percent := fmt.Sprintf("%d%%", int(ratio*percentScale))
	if short {
		return []string{m.sideHead("Context", percent, width)}
	}
	gaugeLine := sidebarIndent + gauge(ratio, style, m.st.dim) + " " + style.Render(percent)
	detail := fmt.Sprintf("%s / %s · compacts at %d%%", formatTokens(used), formatLimit(limit), int(compactAt*percentScale))
	return []string{
		m.sideHead("Context", "", width),
		truncate(gaugeLine, width),
		sideRow(m.st.dim.Render(detail), width),
	}
}

func (m *model) sideHead(title, extra string, width int) string {
	head := " " + m.st.text.Bold(true).Render(title)
	used := 1 + lipgloss.Width(title)
	if extra != "" {
		head += " " + m.st.dim.Render(extra)
		used += 1 + lipgloss.Width(extra)
	}
	fill := max(1, width-used-1)
	return truncate(head+" "+m.st.dim.Render(strings.Repeat(ruleChar, fill)), width)
}

func (tb *tab) noteToolFile(text string) {
	family, rest := toolVerb(text)
	if family != familyWrite {
		return
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return
	}
	for _, file := range tb.files {
		if file == fields[0] {
			return
		}
	}
	tb.files = append(tb.files, fields[0])
}

func appendHidden(m *model, lines []string, hidden, width int) []string {
	if hidden > 0 {
		lines = append(lines, truncate(sidebarIndent+m.st.dim.Render("· +"+strconv.Itoa(hidden)), width))
	}
	return lines
}

func sideRow(text string, width int) string {
	return truncate(sidebarIndent+text, width)
}

func justifyRow(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left+" "+right, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func padCells(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-lipgloss.Width(text)))
}

func fitColumn(lines []string, height, width int) []string {
	out := make([]string, height)
	for index := range out {
		line := ""
		if index < len(lines) {
			line = lines[index]
		}
		out[index] = padCells(truncate(line, width), width)
	}
	return out
}

func shortPath(dir string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(dir, home+"/") {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}
