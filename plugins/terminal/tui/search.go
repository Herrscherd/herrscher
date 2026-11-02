package tui

import (
	"fmt"
	"strings"
)

func searchHits(lines []string, query string) []int {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	q := strings.ToLower(query)
	var out []int
	for i, ln := range lines {
		if strings.Contains(strings.ToLower(visibleText(ln)), q) {
			out = append(out, i)
		}
	}
	return out
}

func escapeEnd(s string, i int) int {
	if i >= len(s) || s[i] != 0x1b {
		return -1
	}
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[':
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
		return len(s)
	case ']', 'P', '_', '^', 'X':
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
	return i + 2
}

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

func markRun(line, lowerQuery string) string {
	lower := strings.ToLower(line)
	if len(lower) != len(line) {
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

func (m *model) typeSearch(q string) {
	m.searchQuery = q
	m.searchHits = searchHits(m.transcriptLines(), q)
	m.searchIdx = 0
	m.syncViewport()
	m.scrollToHit()
}

func (m *model) searchStep(delta int) {
	if len(m.searchHits) == 0 {
		return
	}
	n := len(m.searchHits)
	m.searchIdx = ((m.searchIdx+delta)%n + n) % n
	m.scrollToHit()
}

func (m *model) scrollToHit() {
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchHits) {
		return
	}
	m.vp.SetYOffset(max(0, m.searchHits[m.searchIdx]-m.vp.Height/3))
}

func (m *model) transcriptLines() []string {
	return strings.Split(m.baseContent(), "\n")
}

func (m *model) searchView() string {
	pos := "aucune correspondance"
	if len(m.searchHits) > 0 {
		pos = fmt.Sprintf("%d/%d", m.searchIdx+1, len(m.searchHits))
	}
	return dimStyle.Render("recherche : ") + textStyle.Render(m.searchQuery+"▏") +
		dimStyle.Render("  "+pos+"  ctrl+n/ctrl+p · échap")
}

func turnBoundaries(lines []string) []int {
	var out []int
	for i, ln := range lines {
		if strings.Contains(ln, glyphCursor) || strings.Contains(ln, agentTitle) {
			out = append(out, i)
		}
	}
	return out
}

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

func foldTurns(lines []string, keep int) []string {
	if keep < 1 {
		keep = 1
	}
	bounds := turnBoundaries(lines)
	if len(bounds) <= keep {
		return lines
	}
	cut := bounds[len(bounds)-keep]
	var out []string
	for i := range bounds {
		start := bounds[i]
		if start >= cut {
			return append(out, lines[start:]...)
		}
		if i == 0 && start > 0 {
			out = append(out, lines[:start]...)
		}
		end := bounds[i+1]
		out = append(out, foldedTurnLine(lines[start], end-start))
	}
	return out
}

func foldedTurnLine(head string, lines int) string {
	return truncate(head, 60) + dimStyle.Render(fmt.Sprintf(" %s %d lines", glyphFold, lines))
}

const turnFoldKeep = 2

func (m *model) toggleTurnFold() {
	m.foldTurnsOn = !m.foldTurnsOn
	m.syncViewport()
}
