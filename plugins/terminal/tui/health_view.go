package tui

import (
	"bytes"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const (
	healthColumnGap    = "   "
	healthRemedyIndent = "    "
	verbHealth         = "health"
	argHealthDeep      = "--deep"
	healthInterval     = 15 * time.Second
)

type healthMsg struct {
	snapshot health.Snapshot
	deep     bool
	err      error
}

type healthPanel struct {
	open      bool
	detail    bool
	deepNext  bool
	shownDeep bool
	busy      bool
	ready     bool
	failed    bool
	at        time.Time
	snapshot  health.Snapshot
}

func (m *model) openHealth() tea.Cmd {
	m.health.open = true
	m.health.detail = false
	m.relayout()
	return m.healthRefresh()
}

func (m *model) closeHealth() {
	m.health.open = false
	m.relayout()
}

func (m *model) toggleHealthDetail() {
	m.health.detail = !m.health.detail
}

func (m *model) toggleHealthDeep() tea.Cmd {
	m.health.deepNext = true
	return m.healthRefresh()
}

func (m *model) healthRefresh() tea.Cmd {
	if m.health.busy || m.tm == nil {
		return nil
	}
	m.health.busy = true
	backend := m.tm
	deep := m.health.deepNext
	m.health.deepNext = false
	args := []string{verbHealth}
	if deep {
		args = append(args, argHealthDeep)
	}
	return func() tea.Msg {
		out, err := backend.Dispatch(args)
		if err != nil {
			return healthMsg{deep: deep, err: err}
		}
		snapshot, err := health.Decode([]byte(out))
		return healthMsg{snapshot: snapshot, deep: deep, err: err}
	}
}

func (m *model) healthTick() tea.Cmd {
	if m.health.busy || time.Since(m.health.at) < healthInterval {
		return nil
	}
	return m.healthRefresh()
}

func (m *model) handleHealthMsg(msg healthMsg) {
	m.health.busy = false
	m.health.at = time.Now()
	m.health.failed = msg.err != nil
	if msg.err == nil {
		m.health.snapshot = msg.snapshot
		m.health.shownDeep = msg.deep
		m.health.ready = true
	}
}

func (m *model) handleHealthKey(msg tea.KeyMsg) tea.Cmd {
	chosen := m.stackAction(msg, ctxHealth)
	if run, ok := m.tables().health[chosen]; ok {
		return run(m, msg)
	}
	return nil
}

func healthRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actHealthRefresh, cmdKey((*model).healthRefresh)),
		bind[keyRun](actHealthDeep, cmdKey((*model).toggleHealthDeep)),
		bind[keyRun](actHealthDetail, stepKey((*model).toggleHealthDetail)),
		bind[keyRun](actHealthClose, stepKey((*model).closeHealth)),
	)
}

func (m *model) healthOverlayView() string {
	if !m.health.open {
		return ""
	}
	title := m.st.strong.Render(m.tr.t("tui.health.title", nil))
	if m.health.shownDeep {
		title += m.st.dim.Render(" · " + m.tr.t("tui.health.deep_mode", nil))
	}
	keys := joinNonEmpty(
		m.labelledHint(actHealthRefresh, m.tr.t("tui.health.refresh", nil), ctxHealth),
		m.labelledHint(actHealthDeep, m.tr.t("tui.health.deep", nil), ctxHealth),
		m.labelledHint(actHealthDetail, m.tr.t("tui.health.detail", nil), ctxHealth),
		m.labelledHint(actHealthClose, m.tr.t("tui.common.close", nil), ctxHealth),
	)
	return m.renderFrame(overlayFrame{title: title, right: m.st.dim.Render(keys), body: m.healthBody()})
}

func (m *model) healthProbed() string {
	ago := max(time.Since(m.health.snapshot.ProbedAt)/time.Second, 0)
	next := max((healthInterval-time.Since(m.health.at))/time.Second, 0)
	return m.tr.t("tui.health.probed", map[string]any{"Ago": int(ago), "Next": int(next)})
}

func (m *model) healthBody() []string {
	switch {
	case m.health.failed:
		return []string{"", m.st.red.Render(m.tr.t("tui.health.unreachable", nil)), ""}
	case !m.health.ready:
		return []string{"", m.st.dim.Render(m.tr.t("tui.health.waiting", nil)), ""}
	}
	pal := m.reportPalette()
	view, err := m.health.snapshot.View(m.tr.lang)
	if err != nil {
		return []string{m.st.red.Render(m.tr.t("tui.health.render_failed", nil))}
	}
	lines := []string{"", spreadLine(healthCounts(view, pal), m.st.dim.Render(m.healthProbed()), m.frameInner()), ""}
	if m.health.detail {
		var out bytes.Buffer
		if err := m.health.snapshot.Details(&out, problemChecks(m.health.snapshot.Checks), m.tr.lang, pal); err != nil {
			return []string{m.st.red.Render(m.tr.t("tui.health.render_failed", nil))}
		}
		return append(lines, strings.Split(strings.TrimRight(out.String(), "\n"), "\n")...)
	}
	lines = append(lines, healthGrid(view.Sections, pal, m.frameInner())...)
	if bands := healthBands(view.Sections, pal); len(bands) > 0 {
		lines = append(append(lines, ""), bands...)
	}
	return lines
}

func healthCounts(view health.View, pal termpaint.Palette) string {
	counts := make([]string, 0, len(view.Counts))
	for _, count := range view.Counts {
		counts = append(counts, termpaint.Paint(health.StateStyle(count.State, pal), count.Text))
	}
	return strings.Join(counts, healthColumnGap)
}

func healthGrid(sections []health.Section, pal termpaint.Palette, width int) []string {
	columns := make([][]string, 0, len(sections))
	for _, section := range sections {
		columns = append(columns, healthColumn(section, pal))
	}
	var lines []string
	for start := 0; start < len(columns); {
		end := start + 1
		used := columnWidth(columns[start])
		for end < len(columns) && used+len(healthColumnGap)+columnWidth(columns[end]) <= width {
			used += len(healthColumnGap) + columnWidth(columns[end])
			end++
		}
		if start > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, besideEachOther(columns[start:end])...)
		start = end
	}
	return lines
}

func healthColumn(section health.Section, pal termpaint.Palette) []string {
	cells := &termpaint.Table{Right: map[int]bool{1: true}}
	for index := range section.Rows {
		row := section.Rows[index]
		label := health.RowLabel(row, pal)
		if row.State == contracts.HealthOff {
			label = termpaint.Paint(pal.Dim, health.RowLabel(row, termpaint.Palette{}))
		}
		cells.Add(termpaint.Paint(health.StateStyle(row.State, pal), row.Glyph)+" "+label, healthCellNote(row, pal))
	}
	return append([]string{termpaint.Paint(pal.Group, strings.ToUpper(section.Title))}, cells.Lines()...)
}

func healthCellNote(row health.Row, pal termpaint.Palette) string {
	switch row.State {
	case contracts.HealthOff, contracts.HealthDegraded:
		return termpaint.Paint(health.StateStyle(row.State, pal), row.StateText)
	case contracts.HealthOK, contracts.HealthDown:
		return row.Latency
	}
	return row.Latency
}

func columnWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, termpaint.Width(line))
	}
	return width
}

func besideEachOther(columns [][]string) []string {
	height := 0
	for _, column := range columns {
		height = max(height, len(column))
	}
	lines := make([]string, height)
	for index := range lines {
		var line strings.Builder
		for position, column := range columns {
			cell := ""
			if index < len(column) {
				cell = column[index]
			}
			if position < len(columns)-1 {
				cell = termpaint.PadRight(cell, columnWidth(column)) + healthColumnGap
			}
			line.WriteString(cell)
		}
		lines[index] = strings.TrimRight(line.String(), " ")
	}
	return lines
}

func healthBands(sections []health.Section, pal termpaint.Palette) []string {
	var bands []string
	for _, section := range sections {
		for index := range section.Rows {
			row := section.Rows[index]
			if row.State != contracts.HealthDown && row.State != contracts.HealthDegraded {
				continue
			}
			band := termpaint.Paint(pal.Inverse, " "+row.Glyph+" "+health.RowLabel(row, termpaint.Palette{})+" ")
			if cause := joinNonEmpty(row.Cause, row.Facts); cause != "" {
				band += " " + termpaint.Paint(health.StateStyle(row.State, pal), cause)
			}
			bands = append(bands, band)
			if row.Remedy != "" {
				bands = append(bands, healthRemedyIndent+termpaint.Paint(pal.Dim, "→ ")+row.Remedy)
			}
		}
	}
	return bands
}

func (m *model) healthPart() (barPart, bool) {
	if m.health.failed {
		return single(m.st.red.Render(m.st.glyphs.bolt)+" "+m.st.dim.Render(m.tr.t("tui.health.unreachable", nil)), rankExtra), true
	}
	if !m.health.ready {
		return barPart{}, false
	}
	summary := health.Summarize(m.health.snapshot.Checks)
	dot := m.st.green
	switch summary.ExitCode() {
	case health.ExitHealthy:
		dot = m.st.green
	case health.ExitDegraded:
		dot = m.st.warn
	case health.ExitDown:
		dot = m.st.red
	}
	count := summary.Count(contracts.HealthOK)
	label := m.tr.t("tui.health.bar", map[string]any{"Count": count, "Total": summary.Watched()})
	return single(dot.Render(m.st.glyphs.bolt)+" "+m.st.dim.Render(label), rankExtra), true
}

func problemChecks(checks []health.Check) []health.Check {
	var problems []health.Check
	for index := range checks {
		check := checks[index]
		if state := health.Settled(check.Report).State; state == contracts.HealthDegraded || state == contracts.HealthDown {
			problems = append(problems, check)
		}
	}
	if len(problems) == 0 {
		return checks
	}
	return problems
}
