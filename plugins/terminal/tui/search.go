package tui

import (
	"fmt"
	"strings"
)

// A day-long session scrolls past the thing the operator remembers reading. Until
// now the only way back was PgUp and patience. Search and turn navigation are
// both answers to the same question — where was that — and both work on the
// rendered lines, because what the operator remembers is what they saw.

// searchHits returns the indexes of the lines containing query, case-insensitively.
// An empty query matches nothing rather than everything: a search that highlights
// the whole transcript has told the reader as little as one that highlights none
// of it, and it does so more expensively.
func searchHits(lines []string, query string) []int {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	q := strings.ToLower(query)
	var out []int
	for i, ln := range lines {
		if strings.Contains(strings.ToLower(ln), q) {
			out = append(out, i)
		}
	}
	return out
}

// highlightMatches marks every occurrence of query in lines. Reverse video again:
// the transcript underneath is already styled, and a colour would compete with
// whatever it landed on rather than stand out from it.
func highlightMatches(lines []string, query string) []string {
	if strings.TrimSpace(query) == "" {
		return lines
	}
	q := strings.ToLower(query)
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = markLine(ln, q)
	}
	return out
}

// markLine wraps each match in one line. It searches the lowercased copy but cuts
// the original, so the text keeps its case and its escapes stay intact — the
// offsets are the same in both because lowering ASCII does not change lengths.
func markLine(line, lowerQuery string) string {
	lower := strings.ToLower(line)
	var b strings.Builder
	for {
		i := strings.Index(lower, lowerQuery)
		if i < 0 {
			b.WriteString(line)
			return b.String()
		}
		j := i + len(lowerQuery)
		b.WriteString(line[:i])
		b.WriteString(ansiReverse + line[i:j] + ansiReverseOff)
		line, lower = line[j:], lower[j:]
	}
}

// openSearch opens the overlay, remembering where the operator was. A search is a
// detour, and the way back is the point.
func (m *model) openSearch() {
	if m.searchOpen {
		return
	}
	m.searchOpen = true
	m.searchQuery = ""
	m.searchHits = nil
	m.searchIdx = 0
	m.searchReturn = m.vp.YOffset
	m.applySize()
	m.syncViewport()
}

// closeSearch drops the query and puts the viewport back where it started.
func (m *model) closeSearch() {
	if !m.searchOpen {
		return
	}
	m.searchOpen = false
	m.searchQuery = ""
	m.searchHits = nil
	m.searchIdx = 0
	m.applySize()
	m.syncViewport()
	m.vp.SetYOffset(m.searchReturn)
}

// typeSearch sets the query and re-runs it, landing on the first hit. Searching
// as the query is typed is the whole ergonomics of this: the operator stops
// typing when they can see the line.
func (m *model) typeSearch(q string) {
	m.searchQuery = q
	m.searchHits = searchHits(m.transcriptLines(), q)
	m.searchIdx = 0
	m.syncViewport()
	m.scrollToHit()
}

// searchStep moves to the next or previous hit, wrapping in both directions. A
// search that stops at the end makes the operator remember which end they are at.
func (m *model) searchStep(delta int) {
	if len(m.searchHits) == 0 {
		return
	}
	n := len(m.searchHits)
	m.searchIdx = ((m.searchIdx+delta)%n + n) % n
	m.scrollToHit()
}

// scrollToHit puts the current hit on screen, a third of the way down, so the
// lines around it — which are what make it mean anything — come with it.
func (m *model) scrollToHit() {
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchHits) {
		return
	}
	m.vp.SetYOffset(max(0, m.searchHits[m.searchIdx]-m.vp.Height/3))
}

// transcriptLines is the active tab's rendered transcript, split as the viewport
// holds it. Everything in this file counts in these lines, so an offset computed
// here is an offset the viewport understands.
func (m *model) transcriptLines() []string {
	return strings.Split(m.baseContent(), "\n")
}

// searchView is the overlay row: the query, the position in the hits, and the
// keys. One row, because it sits between the transcript and the composer and
// every row it takes is a row of conversation.
func (m *model) searchView() string {
	pos := "no match"
	if len(m.searchHits) > 0 {
		pos = fmt.Sprintf("%d/%d", m.searchIdx+1, len(m.searchHits))
	}
	return dimStyle.Render("search: ") + textStyle.Render(m.searchQuery+"▏") +
		dimStyle.Render("  "+pos+"  ctrl+n/ctrl+p · esc")
}

// turnBoundaries returns the line indexes where a turn begins: the operator's own
// prompt glyph, or the rule that opens an answer. Those two marks are what a
// reader scans for when scrolling back, so they are what a jump lands on.
func turnBoundaries(lines []string) []int {
	var out []int
	for i, ln := range lines {
		if strings.Contains(ln, glyphCursor) || strings.Contains(ln, agentTitle) {
			out = append(out, i)
		}
	}
	return out
}

// jumpTurn moves the viewport to the next or previous turn boundary. It stops at
// the ends rather than wrapping: unlike a search, scrolling has a top and a
// bottom the operator can feel, and jumping from one to the other would be
// disorienting rather than useful.
func (m *model) jumpTurn(delta int) {
	bounds := turnBoundaries(m.transcriptLines())
	if len(bounds) == 0 {
		return
	}
	cur := m.vp.YOffset
	if delta > 0 {
		for _, b := range bounds {
			if b > cur {
				m.vp.SetYOffset(b)
				return
			}
		}
		m.vp.SetYOffset(bounds[len(bounds)-1])
		return
	}
	for i := len(bounds) - 1; i >= 0; i-- {
		if bounds[i] < cur {
			m.vp.SetYOffset(bounds[i])
			return
		}
	}
	m.vp.SetYOffset(bounds[0])
}

// foldTurns collapses every turn but the last keep of them to its opening line
// plus a count. It returns new lines and never touches the ones it was given, so
// unfolding is not a reconstruction — it is simply not calling this.
func foldTurns(lines []string, keep int) []string {
	if keep < 1 {
		keep = 1 // a fold that collapses the current exchange too has nothing to read
	}
	bounds := turnBoundaries(lines)
	if len(bounds) <= keep {
		return lines
	}
	cut := bounds[len(bounds)-keep] // the last keep turns, from here down, stay expanded
	var out []string
	for i := 0; i < len(bounds); i++ {
		start := bounds[i]
		if start >= cut {
			return append(out, lines[start:]...)
		}
		if i == 0 && start > 0 {
			out = append(out, lines[:start]...) // anything above the first turn
		}
		end := bounds[i+1]
		out = append(out, foldedTurnLine(lines[start], end-start))
	}
	return out
}

// foldedTurnLine is what a collapsed turn shows: its own first line, clipped, and
// how much is behind it. The first line is the turn's own words, which is the
// only summary that is never wrong.
func foldedTurnLine(head string, lines int) string {
	return truncate(head, 60) + dimStyle.Render(fmt.Sprintf(" %s %d lines", glyphFold, lines))
}

// turnFoldKeep is how many turns stay open when old ones are folded: the current
// exchange and the one it answers.
const turnFoldKeep = 2

// toggleTurnFold flips the collapsed view of the history above the current
// exchange.
func (m *model) toggleTurnFold() {
	m.foldTurnsOn = !m.foldTurnsOn
	m.syncViewport()
}
