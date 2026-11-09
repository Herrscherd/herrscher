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
	argUsageVend  = "--vendor"
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
	vendorParam  = "Vendor"
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
	vendor   string
	vendors  []string
	ready    bool
	failed   bool
	snapshot usage.Snapshot
}

type usageMsg struct {
	rng      usage.Range
	account  usage.AccountKey
	vendor   string
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

func usageArgs(rng usage.Range, account usage.AccountKey, vendor string) []string {
	args := []string{verbUsage, argUsageRange, rng.String()}
	if account != (usage.AccountKey{}) {
		args = append(args, argUsageAcct, account.String())
	}
	if vendor != "" {
		args = append(args, argUsageVend, vendor)
	}
	return args
}

func (m *model) usageRefresh() tea.Cmd {
	if m.tm == nil {
		return nil
	}
	backend := m.tm
	rng, account, vendor := m.usage.rng, m.usage.account, m.usage.vendor
	args := usageArgs(rng, account, vendor)
	return func() tea.Msg {
		out, err := backend.Dispatch(args)
		if err != nil {
			return usageMsg{rng: rng, account: account, vendor: vendor, err: err}
		}
		snapshot, err := usage.Decode([]byte(out))
		return usageMsg{rng: rng, account: account, vendor: vendor, snapshot: snapshot, err: err}
	}
}

func (m *model) handleUsageMsg(msg usageMsg) {
	if msg.rng != m.usage.rng || msg.account != m.usage.account || msg.vendor != m.usage.vendor {
		return
	}
	m.usage.failed = msg.err != nil
	if msg.err == nil {
		m.usage.snapshot = msg.snapshot
		m.usage.ready = true
		m.usage.vendors = knownVendors(m.usage.vendors, msg.snapshot)
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
		bind[keyRun](actUsageVendor, cmdKey((*model).nextUsageVendor)),
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
	return m.reloadUsage()
}

func (m *model) nextUsageAccount() tea.Cmd {
	accounts := []usage.AccountKey{{}}
	add := func(vendor, account string) {
		key := usage.AccountKey{Vendor: vendor, Account: account}
		if (m.usage.vendor == "" || vendor == m.usage.vendor) && !slices.Contains(accounts, key) {
			accounts = append(accounts, key)
		}
	}
	for _, entry := range m.usage.snapshot.Limits {
		add(entry.Vendor, entry.Account)
	}
	for _, share := range m.usage.snapshot.Spend.Accounts {
		add(share.Vendor, share.Account)
	}
	index := slices.Index(accounts, m.usage.account)
	m.usage.account = accounts[(index+1)%len(accounts)]
	return m.reloadUsage()
}

func (m *model) nextUsageVendor() tea.Cmd {
	vendors := append([]string{""}, m.usage.vendors...)
	index := slices.Index(vendors, m.usage.vendor)
	m.usage.vendor = vendors[(index+1)%len(vendors)]
	if m.usage.vendor != "" && m.usage.account.Vendor != m.usage.vendor {
		m.usage.account = usage.AccountKey{}
	}
	return m.reloadUsage()
}

func (m *model) reloadUsage() tea.Cmd {
	m.usage.ready = false
	m.usage.failed = false
	return m.usageRefresh()
}

func knownVendors(known []string, snapshot usage.Snapshot) []string {
	vendors := slices.Clone(known)
	add := func(vendor string) {
		if vendor != "" && !slices.Contains(vendors, vendor) {
			vendors = append(vendors, vendor)
		}
	}
	for index := range snapshot.Limits {
		add(snapshot.Limits[index].Vendor)
	}
	for index := range snapshot.Spend.Accounts {
		add(snapshot.Spend.Accounts[index].Vendor)
	}
	return vendors
}

func (m *model) usageOverlayView() string {
	if !m.usage.open {
		return ""
	}
	keys := joinNonEmpty(
		m.labelledHint(actUsageTab, m.tr.t("tui.usage.hint.tab", nil), ctxUsage),
		m.labelledHint(actUsageRangeNext, m.tr.t("tui.usage.hint.range", nil), ctxUsage),
		m.labelledHint(actUsageAccount, m.tr.t("tui.usage.hint.account", nil), ctxUsage),
		m.labelledHint(actUsageVendor, m.tr.t("tui.usage.hint.vendor", nil), ctxUsage),
		m.labelledHint(actUsageRefresh, m.tr.t("tui.usage.hint.refresh", nil), ctxUsage),
		m.labelledHint(actUsageClose, m.tr.t("tui.common.close", nil), ctxUsage),
	)
	body := append([]string{""}, m.usageBody()...)
	return m.renderFrame(overlayFrame{
		title:  m.st.strong.Render(m.tr.t("tui.usage.title", nil)) + "  " + m.usageFilters(),
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

func (m *model) usageFilters() string {
	account, vendor := m.tr.t("tui.usage.filter.all", nil), m.tr.t("tui.usage.filter.all", nil)
	if m.usage.account != (usage.AccountKey{}) {
		account = m.usage.account.String()
	}
	if m.usage.vendor != "" {
		vendor = m.usage.vendor
	}
	return m.st.dim.Render(m.tr.t("tui.usage.filter.account", map[string]any{accountParam: m.st.accent.Render(account)})) + "  " +
		m.st.dim.Render(m.tr.t("tui.usage.filter.vendor", map[string]any{vendorParam: m.st.accent.Render(vendor)}))
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
		return usage.CostPanel(m.usage.snapshot, m.tr.lang, time.Local, m.reportPalette(), m.frameInner())
	case usageTabTokens:
		return usage.TokenLines(m.usage.snapshot, m.tr.lang, m.reportPalette())
	case usageTabSession, usageTabLimits:
	}
	return usage.LimitCards(m.usage.snapshot, m.tr.lang, m.reportPalette(), m.cardEdges(), m.frameInner())
}
