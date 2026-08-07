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
		// The lines are already painted, so a raw Contains would also match the
		// escape sequences: "m" is in every SGR, and a search for it would report
		// every line in the transcript as a hit.
		if strings.Contains(strings.ToLower(visibleText(ln)), q) {
			out = append(out, i)
		}
	}
	return out
}

// escapeEnd returns the index just past the escape sequence starting at i, or -1
// when nothing starts there. It has to know the shapes because the transcript
// carries more than SGR: an OSC 8 hyperlink, a kitty APC image and a sixel DCS
// band all live on these lines, and their payloads are bytes, not text.
func escapeEnd(s string, i int) int {
	if i >= len(s) || s[i] != 0x1b {
		return -1
	}
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[': // CSI: parameters until a final byte in 0x40..0x7e
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
		return len(s)
	case ']', 'P', '_', '^', 'X': // OSC, DCS, APC, PM, SOS: until ST or BEL
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	}
	return i + 2 // a two-byte escape
}

// visibleText is s with its escape sequences removed: what the operator can
// actually read on the row, which is the only thing a search over it should see.
func visibleText(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	forEachTextRun(s, func(esc bool, chunk string) {
		if !esc {
			b.WriteString(chunk)
		}
	})
	return b.String()
}

// forEachTextRun walks s as alternating escape sequences and plain text, calling
// fn for each run in order. Every caller here needs the same split and none of
// them may get it slightly differently.
func forEachTextRun(s string, fn func(esc bool, chunk string)) {
	for i := 0; i < len(s); {
		if end := escapeEnd(s, i); end > 0 {
			fn(true, s[i:end])
			i = end
			continue
		}
		j := i
		for j < len(s) && s[j] != 0x1b {
			j++
		}
		fn(false, s[i:j])
		i = j
	}
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

// markLine wraps each match in one line, looking only at the parts of it the
// operator can read. Marking the escape sequences too is not a cosmetic slip: the
// mark is inserted mid-sequence, which splits an SGR (or an image payload) in two
// and spills the remainder onto the screen as text. A match that straddles a
// style change is missed instead, which costs a highlight rather than the row.
func markLine(line, lowerQuery string) string {
	var b strings.Builder
	forEachTextRun(line, func(esc bool, chunk string) {
		if esc {
			b.WriteString(chunk)
			return
		}
		b.WriteString(markRun(chunk, lowerQuery))
	})
	return b.String()
}

// markRun marks the matches in one run of plain text. It searches the lowercased
// copy but cuts the original, so the text keeps its case — the offsets are the
// same in both because lowering ASCII does not change lengths.
func markRun(line, lowerQuery string) string {
	lower := strings.ToLower(line)
	if len(lower) != len(line) {
		// A handful of runes lower to a different byte length (İ is the usual one),
		// and every offset below would then point into the wrong string — far enough
		// to slice past the end. Such a run keeps its text and loses its highlight.
		return line
	}
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
