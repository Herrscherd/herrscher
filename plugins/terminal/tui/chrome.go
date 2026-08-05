package tui

import (
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/charmbracelet/lipgloss"
)

// The chrome is the frame around the transcript: a banner naming the terminal and
// its open sessions, a rule separating the flow from the composer, and the empty
// state a fresh tab shows instead of a blank screen. None of it carries content —
// it says where you are, which the terminal previously left you to guess.

// brand is the banner's fixed left mark.
const brand = "──◂ HERRSCHER ▸──"

// bannerRow renders the brand and the tab strip on one line, clipped to width.
// Tabs live here rather than in a picker because they are the terminal's one real
// advantage: several sessions at once, each with its own transcript.
func (m *model) bannerRow() string {
	left := accentStyle.Render(brand)
	strip := m.tabStrip()
	if strip == "" {
		return truncate(left, m.innerWidth())
	}
	gap := m.innerWidth() - lipgloss.Width(left) - lipgloss.Width(strip)
	if gap < 2 {
		// No room for both: the tabs win — which session you are typing into
		// matters more than the name of the program you already launched.
		return truncate(strip, m.innerWidth())
	}
	return left + strings.Repeat(" ", gap) + strip
}

// tabStrip renders one mark per open session: the active one accented and led by
// a chevron, the others dim, an unread one carrying its pip.
func (m *model) tabStrip() string {
	var out []string
	for _, ch := range m.order {
		tb := m.tabs[ch]
		if tb == nil {
			continue
		}
		label := tb.label
		if tb.unread {
			label = glyphUnread + label
		}
		if ch == m.active {
			out = append(out, accentStyle.Render(glyphCursor+" "+label))
			continue
		}
		out = append(out, dimStyle.Render(label))
	}
	return strings.Join(out, "  ")
}

// separatorRow rules off the transcript from the composer, so a long answer does
// not run visually into what you are typing.
func (m *model) separatorRow() string {
	return dimStyle.Render(strings.Repeat("─", m.innerWidth()))
}

// activeInfo is the hub's record for the active tab, if the hub still knows it.
func (m *model) activeInfo() (contracts.SessionInfo, bool) {
	if m.tm == nil {
		return contracts.SessionInfo{}, false
	}
	for _, s := range m.tm.Sessions() {
		if s.ChannelID == m.active {
			return s, true
		}
	}
	return contracts.SessionInfo{}, false
}

// emptyState is what a tab with no transcript shows. It replaces a black screen
// with the two things you need on arrival: which session this is, and what you
// can press. It renders inside the viewport, so it costs no chrome height.
func (m *model) emptyState(width int) string {
	lines := []string{accentStyle.Render(brand)}
	if s, ok := m.activeInfo(); ok {
		var facts []string
		for _, f := range []string{s.Name, s.Project, s.Vendor} {
			if f != "" {
				facts = append(facts, f)
			}
		}
		if len(facts) > 0 {
			lines = append(lines, dimStyle.Render(strings.Join(facts, " · ")))
		}
	}
	lines = append(lines, "", dimStyle.Render("type a message · / commands · @ files · Tab switches session"))
	for i, ln := range lines {
		lines[i] = truncate(ln, width)
	}
	return strings.Join(lines, "\n")
}
