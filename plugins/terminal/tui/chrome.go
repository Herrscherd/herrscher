package tui

import (
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The chrome is the frame around the transcript: the rail naming the terminal and
// its open sessions, the composer box, the status line under it, and the empty
// state a fresh tab shows instead of a blank screen. None of it carries content —
// it says where you are, which the terminal previously left you to guess.

const infoTTL = 500 * time.Millisecond

// activeInfo is the hub's record for the active tab, if the hub still knows it.
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
	return dimStyle.Render("  "+label+strings.Repeat(" ", max(1, factWidth-lipgloss.Width(label)))) +
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
	if label := modelLabel(s); label != "" {
		out = append(out, fact("modèle", label))
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
	return append([]string{"", dimStyle.Render("  autres sessions")}, rows...)
}

// shortcutRow is the two things a reader needs before typing: how to reach the
// commands, and how to get out of a turn.
func shortcutRow() string {
	return dimStyle.Render("  /  commandes   @  fichiers   Tab  changer de session   échap  interrompre")
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
