package tui

import (
	"bytes"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

const (
	verbHealth     = "health"
	argHealthDeep  = "--deep"
	healthInterval = 15 * time.Second
)

type healthMsg struct {
	snapshot health.Snapshot
	deep     bool
	err      error
}

type healthPanel struct {
	open     bool
	detail   bool
	deep     bool
	busy     bool
	ready    bool
	failed   bool
	at       time.Time
	snapshot health.Snapshot
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
	m.health.deep = !m.health.deep
	return m.healthRefresh()
}

func (m *model) healthRefresh() tea.Cmd {
	if m.health.busy || m.tm == nil {
		return nil
	}
	m.health.busy = true
	backend := m.tm
	deep := m.health.deep
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
	width := m.width
	if width <= 0 {
		width = defaultWidth
	}
	title := m.tr.t("tui.health.title", nil)
	if m.health.deep {
		title += "  " + m.tr.t("tui.health.deep_mode", nil)
	}
	lines := []string{m.st.dim.Render(title)}
	switch {
	case m.health.failed:
		lines = append(lines, m.st.red.Render(m.tr.t("tui.health.unreachable", nil)))
	case !m.health.ready:
		lines = append(lines, m.st.dim.Render(m.tr.t("tui.health.waiting", nil)))
	}
	lines = append(lines, m.healthBody()...)
	lines = append(lines, m.st.dim.Render(joinNonEmpty(
		m.labelledHint(actHealthRefresh, m.tr.t("tui.health.refresh", nil), ctxHealth),
		m.labelledHint(actHealthDeep, m.tr.t("tui.health.deep", nil), ctxHealth),
		m.labelledHint(actHealthDetail, m.tr.t("tui.health.detail", nil), ctxHealth),
		m.labelledHint(actHealthClose, m.tr.t("tui.common.close", nil), ctxHealth),
	)))
	for i := range lines {
		lines[i] = cutText(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func (m *model) healthBody() []string {
	if !m.health.ready {
		return nil
	}
	var out bytes.Buffer
	var err error
	if m.health.detail {
		err = m.health.snapshot.Details(&out, problemChecks(m.health.snapshot.Checks), m.tr.lang)
	} else {
		err = m.health.snapshot.Table(&out, m.tr.lang)
	}
	if err != nil {
		return []string{m.st.red.Render(m.tr.t("tui.health.render_failed", nil))}
	}
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
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
