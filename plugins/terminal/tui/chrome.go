package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const infoTTL = 500 * time.Millisecond

func (m *model) activeInfo() (contracts.SessionInfo, bool) {
	if m.tm == nil {
		return contracts.SessionInfo{}, false
	}
	if m.infoChannel == m.active && time.Since(m.infoAt) < infoTTL {
		return m.infoCache, m.infoKnown
	}
	m.infoCache, m.infoKnown = contracts.SessionInfo{}, false
	sessions := m.tm.Sessions()
	for index := range sessions {
		if sessions[index].ChannelID == m.active {
			m.infoCache, m.infoKnown = sessions[index], true
			break
		}
	}
	m.infoChannel, m.infoAt = m.active, time.Now()
	return m.infoCache, m.infoKnown
}

func (m *model) emptyState(width int) string {
	lines := []string{m.st.accent.Render(m.st.glyphs.brand), ""}
	lines = append(lines, m.sessionFacts()...)
	lines = append(lines, m.otherSessions()...)
	if m.tabs[m.active] == nil {
		lines = append(lines, "", m.st.text.Render("  "+m.tr.t("tui.chrome.start_hint", nil)))
	}
	lines = append(lines, "", m.shortcutRow())
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" && len(out) > 0 && out[len(out)-1] == "" {
			continue
		}
		out = append(out, truncate(line, width))
	}
	return strings.Join(out, "\n")
}

const factWidth = 10

func fact(label, value string, styles uiStyles) string {
	return styles.dim.Render("  "+label+strings.Repeat(" ", max(1, factWidth-lipgloss.Width(label)))) +
		styles.text.Render(value)
}

func (m *model) sessionFacts() []string {
	info, ok := m.activeInfo()
	if !ok {
		return nil
	}
	var out []string
	if info.Name != "" {
		out = append(out, fact(m.tr.t("tui.chrome.fact_session", nil), info.Name, m.st))
	}
	if where := joinNonEmpty(info.Project, info.Vendor); where != "" {
		out = append(out, fact(m.tr.t("tui.chrome.fact_repo", nil), where, m.st))
	}
	if label := modelLabel(info); label != "" {
		out = append(out, fact(m.tr.t("tui.chrome.fact_model", nil), label, m.st))
	}
	if tb := m.tabs[m.active]; tb != nil && !tb.openedAt.IsZero() {
		out = append(out, fact(m.tr.t("tui.chrome.fact_open", nil), formatDuration(time.Since(tb.openedAt)), m.st))
	}
	return out
}

const maxOtherSessions = 5

func (m *model) otherSessions() []string {
	var rows []string
	for _, channel := range m.order {
		if len(rows) >= maxOtherSessions {
			break
		}
		if channel == m.active {
			continue
		}
		tab := m.tabs[channel]
		if tab == nil {
			continue
		}
		mark := "  "
		if tab.unread {
			mark = " " + m.st.glyphs.unread
		}
		rows = append(rows, m.st.dim.Render("  "+mark+" "+tab.label))
	}
	if len(rows) == 0 {
		return nil
	}
	return append([]string{"", m.st.dim.Render("  " + m.tr.t("tui.chrome.other_sessions", nil))}, rows...)
}

func (m *model) shortcutRow() string {
	return m.st.dim.Render("  " + joinNonEmpty(
		m.tr.t("tui.chrome.hint_commands", nil),
		m.tr.t("tui.chrome.hint_files", nil),
		m.labelledHint(actTabsNext, m.tr.t("tui.chrome.switch_session", nil), chatKeyStack()...),
		m.labelledHint(actCancel, m.tr.t("tui.common.interrupt", nil), chatKeyStack()...),
	))
}

func joinNonEmpty(parts ...string) string {
	var kept []string
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

func truncate(text string, width int) string {
	if width < 1 || lipgloss.Width(text) <= width {
		return text
	}
	return ansi.Truncate(text, width, "")
}

func truncateLeft(text string, width int) string {
	if width < 2 || lipgloss.Width(text) <= width {
		return truncate(text, width)
	}
	return ansi.TruncateLeft(text, lipgloss.Width(text)-width+1, "…")
}
