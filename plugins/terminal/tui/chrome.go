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
	for _, s := range m.tm.Sessions() {
		if s.ChannelID == m.active {
			m.infoCache, m.infoKnown = s, true
			break
		}
	}
	m.infoChannel, m.infoAt = m.active, time.Now()
	return m.infoCache, m.infoKnown
}

func (m *model) emptyState(width int) string {
	lines := []string{accentStyle.Render(brand), ""}
	lines = append(lines, m.sessionFacts()...)
	lines = append(lines, m.otherSessions()...)
	lines = append(lines, "", shortcutRow())
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if ln == "" && len(out) > 0 && out[len(out)-1] == "" {
			continue
		}
		out = append(out, truncate(ln, width))
	}
	return strings.Join(out, "\n")
}

const factWidth = 10

func fact(label, value string) string {
	return dimStyle.Render("  "+label+strings.Repeat(" ", max(1, factWidth-lipgloss.Width(label)))) +
		textStyle.Render(value)
}

func (m *model) sessionFacts() []string {
	s, ok := m.activeInfo()
	if !ok {
		return nil
	}
	var out []string
	if s.Name != "" {
		out = append(out, fact("session", s.Name))
	}
	if where := joinNonEmpty(s.Project, s.Vendor); where != "" {
		out = append(out, fact("repo", where))
	}
	if label := modelLabel(s); label != "" {
		out = append(out, fact("modèle", label))
	}
	if tb := m.tabs[m.active]; tb != nil && !tb.openedAt.IsZero() {
		out = append(out, fact("open", formatDuration(time.Since(tb.openedAt))))
	}
	return out
}

const maxOtherSessions = 5

func (m *model) otherSessions() []string {
	var rows []string
	for _, ch := range m.order {
		if len(rows) >= maxOtherSessions {
			break
		}
		if ch == m.active {
			continue
		}
		tb := m.tabs[ch]
		if tb == nil {
			continue
		}
		mark := "  "
		if tb.unread {
			mark = " " + glyphUnread
		}
		rows = append(rows, dimStyle.Render("  "+mark+" "+tb.label))
	}
	if len(rows) == 0 {
		return nil
	}
	return append([]string{"", dimStyle.Render("  autres sessions")}, rows...)
}

func shortcutRow() string {
	return dimStyle.Render("  /  commandes   @  fichiers   Tab  changer de session   échap  interrompre")
}

func joinNonEmpty(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

func truncate(s string, width int) string {
	if width < 1 || lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}
