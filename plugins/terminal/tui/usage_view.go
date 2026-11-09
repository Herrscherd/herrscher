package tui

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/usage"
)

const (
	verbUsage     = "usage"
	argUsageRange = "--range"
	argUsageAcct  = "--account"
)

type usageTab int

const (
	usageTabSession usageTab = iota + 1
	usageTabCost
	usageTabTokens
	usageTabLimits
)

func (t usageTab) next() usageTab {
	if t >= usageTabLimits {
		return usageTabSession
	}
	return t + 1
}

const (
	rangeParam   = "Range"
	accountParam = "Account"
)

func (t usageTab) key() string {
	switch t {
	case usageTabSession:
		return "tui.usage.tab.session"
	case usageTabCost:
		return "tui.usage.tab.cost"
	case usageTabTokens:
		return "tui.usage.tab.tokens"
	case usageTabLimits:
		return "tui.usage.tab.limits"
	}
	return "tui.usage.tab.session"
}

type usagePanel struct {
	open     bool
	tab      usageTab
	rng      usage.Range
	account  usage.AccountKey
	ready    bool
	failed   bool
	snapshot usage.Snapshot
}

type usageMsg struct {
	rng      usage.Range
	account  usage.AccountKey
	snapshot usage.Snapshot
	err      error
}

func (m *model) openUsage() tea.Cmd {
	m.usage.open = true
	if m.usage.tab == 0 {
		m.usage.tab = usageTabSession
	}
	if m.usage.rng == 0 {
		m.usage.rng = usage.DefaultRange()
	}
	m.relayout()
	return m.usageRefresh()
}

func (m *model) closeUsage() {
	m.usage.open = false
	m.relayout()
}

func usageArgs(rng usage.Range, account usage.AccountKey) []string {
	args := []string{verbUsage, argUsageRange, rng.String()}
	if account != (usage.AccountKey{}) {
		args = append(args, argUsageAcct, account.String())
	}
	return args
}

func (m *model) usageRefresh() tea.Cmd {
	if m.tm == nil {
		return nil
	}
	backend := m.tm
	rng, account := m.usage.rng, m.usage.account
	args := usageArgs(rng, account)
	return func() tea.Msg {
		out, err := backend.Dispatch(args)
		if err != nil {
			return usageMsg{rng: rng, account: account, err: err}
		}
		snapshot, err := usage.Decode([]byte(out))
		return usageMsg{rng: rng, account: account, snapshot: snapshot, err: err}
	}
}

func (m *model) handleUsageMsg(msg usageMsg) {
	if msg.rng != m.usage.rng || msg.account != m.usage.account {
		return
	}
	m.usage.failed = msg.err != nil
	if msg.err == nil {
		m.usage.snapshot = msg.snapshot
		m.usage.ready = true
	}
}

func (m *model) handleUsageKey(msg tea.KeyMsg) tea.Cmd {
	chosen := m.stackAction(msg, ctxUsage)
	if run, ok := m.tables().usage[chosen]; ok {
		return run(m, msg)
	}
	return nil
}

func usageRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actUsageRefresh, cmdKey((*model).usageRefresh)),
		bind[keyRun](actUsageTab, stepKey((*model).nextUsageTab)),
		bind[keyRun](actUsageRangePrevious, cmdKey((*model).previousUsageRange)),
		bind[keyRun](actUsageRangeNext, cmdKey((*model).nextUsageRange)),
		bind[keyRun](actUsageAccount, cmdKey((*model).nextUsageAccount)),
		bind[keyRun](actUsageClose, stepKey((*model).closeUsage)),
	)
}

func (m *model) nextUsageTab() {
	m.usage.tab = m.usage.tab.next()
}

func (m *model) previousUsageRange() tea.Cmd {
	return m.stepUsageRange(-1)
}

func (m *model) nextUsageRange() tea.Cmd {
	return m.stepUsageRange(1)
}

func (m *model) stepUsageRange(delta int) tea.Cmd {
	ranges := usage.Ranges()
	index := slices.Index(ranges, m.usage.rng)
	m.usage.rng = ranges[(index+delta+len(ranges))%len(ranges)]
	m.usage.ready = false
	m.usage.failed = false
	return m.usageRefresh()
}

func (m *model) nextUsageAccount() tea.Cmd {
	accounts := []usage.AccountKey{{}}
	for _, entry := range m.usage.snapshot.Limits {
		accounts = append(accounts, usage.AccountKey{Vendor: entry.Vendor, Account: entry.Account})
	}
	index := slices.Index(accounts, m.usage.account)
	m.usage.account = accounts[(index+1)%len(accounts)]
	m.usage.ready = false
	m.usage.failed = false
	return m.usageRefresh()
}

func (m *model) usageOverlayView() string {
	if !m.usage.open {
		return ""
	}
	keys := joinNonEmpty(
		m.labelledHint(actUsageTab, m.tr.t("tui.usage.hint.tab", nil), ctxUsage),
		m.labelledHint(actUsageRangeNext, m.tr.t("tui.usage.hint.range", nil), ctxUsage),
		m.labelledHint(actUsageAccount, m.tr.t("tui.usage.hint.account", nil), ctxUsage),
		m.labelledHint(actUsageRefresh, m.tr.t("tui.usage.hint.refresh", nil), ctxUsage),
		m.labelledHint(actUsageClose, m.tr.t("tui.common.close", nil), ctxUsage),
	)
	body := append([]string{""}, m.usageBody()...)
	return m.renderFrame(overlayFrame{
		title:  m.st.strong.Render(m.tr.t("tui.usage.title", nil)) + m.st.dim.Render(" · ") + m.st.accent.Render(m.usageAccountLabel()),
		right:  joinWith("  ", m.usageRangeChips(), m.usageTabsLine()),
		footer: m.st.dim.Render(keys),
		body:   body,
	})
}

func (m *model) usageTabsLine() string {
	tabs := []usageTab{usageTabSession, usageTabCost, usageTabTokens, usageTabLimits}
	labels := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		labels = append(labels, m.chip(m.tr.t(tab.key(), nil), tab == m.usage.tab))
	}
	return strings.Join(labels, "")
}

func (m *model) usageRangeChips() string {
	if m.usage.tab != usageTabCost && m.usage.tab != usageTabTokens {
		return ""
	}
	ranges := usage.Ranges()
	chips := make([]string, 0, len(ranges))
	for _, rng := range ranges {
		chips = append(chips, m.chip(rng.String(), rng == m.usage.rng))
	}
	return strings.Join(chips, "")
}

func (m *model) chip(label string, active bool) string {
	if active {
		return m.st.overlaySel.Render(" " + label + " ")
	}
	return m.st.dim.Render(" " + label + " ")
}

func (m *model) usageAccountLabel() string {
	if m.usage.account == (usage.AccountKey{}) {
		return m.tr.t("tui.usage.account.all", nil)
	}
	return m.tr.t("tui.usage.account.one", map[string]any{accountParam: m.usage.account.String()})
}

func (m *model) usageBody() []string {
	if m.usage.tab == usageTabSession {
		return m.usageSessionBody()
	}
	if m.usage.failed {
		return []string{m.st.red.Render(m.tr.t("tui.usage.unreachable", nil))}
	}
	if !m.usage.ready {
		return []string{m.st.dim.Render(m.tr.t("tui.usage.waiting", nil))}
	}
	lines, err := m.usageLines()
	if err != nil {
		return []string{m.st.red.Render(m.tr.t("tui.usage.render_failed", nil))}
	}
	return lines
}

func (m *model) usageSessionBody() []string {
	current := m.tabs[m.active]
	if current == nil {
		return nil
	}
	return strings.Split(strings.TrimRight(m.usageReport(current), "\n"), "\n")
}

func (m *model) usageLines() ([]string, error) {
	switch m.usage.tab {
	case usageTabCost:
		return usage.CostLines(m.usage.snapshot, m.tr.lang, time.Local, m.reportPalette())
	case usageTabTokens:
		return usage.TokenLines(m.usage.snapshot, m.tr.lang, m.reportPalette())
	case usageTabSession, usageTabLimits:
	}
	return usage.LimitLines(m.usage.snapshot, m.tr.lang, m.reportPalette())
}
