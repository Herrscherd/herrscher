package tui

import (
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	if gap >= 2 {
		return left + strings.Repeat(" ", gap) + strip
	}
	// No room for both: the tabs win — which session you are typing into matters
	// more than the name of the program you already launched. And if even the
	// strip does not fit, clipping it would cut from the right and could drop the
	// active tab entirely, so fall back to the active tab alone.
	if lipgloss.Width(strip) <= m.innerWidth() {
		return strip
	}
	return truncate(m.activeMark(), m.innerWidth())
}

// activeMark is the active tab's mark alone — the last thing the banner gives up.
func (m *model) activeMark() string {
	tb := m.tabs[m.active]
	if tb == nil {
		return ""
	}
	return accentStyle.Render(glyphCursor + " " + tb.label)
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
	lines := []string{accentStyle.Render(brand), ""}
	lines = append(lines, m.sessionFacts()...)
	lines = append(lines, m.otherSessions()...)
	lines = append(lines, "", shortcutRow())
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		// Each section brings its own leading blank so it can be dropped whole. Two
		// of them meeting — a hub that does not know this tab yet, so no facts
		// between the brand and the session list — must still read as one gap.
		if ln == "" && len(out) > 0 && out[len(out)-1] == "" {
			continue
		}
		out = append(out, truncate(ln, width))
	}
	return strings.Join(out, "\n")
}

// factWidth pads a fact's label so the values line up in a column. A pair of
// ragged lines reads as prose; an aligned pair reads as a record, which is what
// this is.
const factWidth = 10

// fact renders one labelled line of the arrival record.
func fact(label, value string) string {
	return dimStyle.Render("  "+label+strings.Repeat(" ", max(1, factWidth-len(label)))) +
		textStyle.Render(value)
}

// sessionFacts is what this tab is: its session, where it works, and since when.
// A hub that does not know the tab yields nothing rather than a row of blanks.
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
	if tb := m.tabs[m.active]; tb != nil && !tb.openedAt.IsZero() {
		out = append(out, fact("open", formatDuration(time.Since(tb.openedAt))))
	}
	return out
}

// maxOtherSessions bounds the list of other open sessions. The point of the list
// is that you can see there are others and switch to one, not that you can read
// them all: past a handful it stops being a glance and starts being a page.
const maxOtherSessions = 5

// otherSessions lists the tabs this one is not, so a fresh tab says what else is
// running rather than implying nothing is. It renders nothing at all when this
// is the only session — an empty heading is worse than no heading.
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
	return append([]string{"", dimStyle.Render("  other sessions")}, rows...)
}

// shortcutRow is the two things a reader needs before typing: how to reach the
// commands, and how to get out of a turn.
func shortcutRow() string {
	return dimStyle.Render("  /  commands   @  files   Tab  switch session   esc  interrupt")
}

// joinNonEmpty joins the parts that have something to say with a separating dot.
func joinNonEmpty(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

// truncate clips s to width columns. Callers pass already-styled text, so the
// clip must be aware of escape sequences and of wide glyphs alike — cutting mid
// escape would leak the sequence onto the screen as text.
func truncate(s string, width int) string {
	if width < 1 || lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}
