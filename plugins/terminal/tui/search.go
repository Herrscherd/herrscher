package tui

import (
	"fmt"
	"strings"
)

const (
	oscIntroLen     = 2
	stTerminatorLen = 2
	foldedHeadWidth = 60
)

func searchHits(lines []string, query string) []int {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	needle := strings.ToLower(query)
	var out []int
	for index, ln := range lines {
		if strings.Contains(strings.ToLower(visibleText(ln)), needle) {
			out = append(out, index)
		}
	}
	return out
}

func escapeEnd(text string, index int) int {
	if index >= len(text) || text[index] != 0x1b {
		return -1
	}
	if index+1 >= len(text) {
		return len(text)
	}
	switch text[index+1] {
	case '[':
		return csiEnd(text, index)
	case ']', 'P', '_', '^', 'X':
		return stringEnd(text, index)
	}
	return index + oscIntroLen
}

func csiEnd(text string, i int) int {
	for next := i + oscIntroLen; next < len(text); next++ {
		if text[next] >= 0x40 && text[next] <= 0x7e {
			return next + 1
		}
	}
	return len(text)
}

func stringEnd(text string, i int) int {
	for next := i + oscIntroLen; next < len(text); next++ {
		if text[next] == ctrlBEL {
			return next + 1
		}
		if text[next] == 0x1b && next+1 < len(text) && text[next+1] == '\\' {
			return next + stTerminatorLen
		}
	}
	return len(text)
}

func visibleText(text string) string {
	if !strings.Contains(text, "\x1b") {
		return text
	}
	var block strings.Builder
	forEachTextRun(text, func(esc bool, chunk string) {
		if !esc {
			block.WriteString(chunk)
		}
	})
	return block.String()
}

func forEachTextRun(text string, emit func(esc bool, chunk string)) {
	for index := 0; index < len(text); {
		if end := escapeEnd(text, index); end > 0 {
			emit(true, text[index:end])
			index = end
			continue
		}
		next := index
		for next < len(text) && text[next] != 0x1b {
			next++
		}
		emit(false, text[index:next])
		index = next
	}
}

func highlightMatches(lines []string, query string) []string {
	if strings.TrimSpace(query) == "" {
		return lines
	}
	needle := strings.ToLower(query)
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = markLine(ln, needle)
	}
	return out
}

func markLine(line, lowerQuery string) string {
	var builder strings.Builder
	forEachTextRun(line, func(esc bool, chunk string) {
		if esc {
			builder.WriteString(chunk)
			return
		}
		builder.WriteString(markRun(chunk, lowerQuery))
	})
	return builder.String()
}

func markRun(line, lowerQuery string) string {
	lower := strings.ToLower(line)
	if len(lower) != len(line) {
		return line
	}
	var builder strings.Builder
	for {
		index := strings.Index(lower, lowerQuery)
		if index < 0 {
			builder.WriteString(line)
			return builder.String()
		}
		next := index + len(lowerQuery)
		builder.WriteString(line[:index])
		builder.WriteString(ansiReverse + line[index:next] + ansiReverseOff)
		line, lower = line[next:], lower[next:]
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

func (m *model) typeSearch(query string) {
	m.searchQuery = query
	m.searchHits = searchHits(m.transcriptLines(), query)
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
	return m.st.dim.Render("recherche : ") + m.st.text.Render(m.searchQuery+"▏") +
		m.st.dim.Render("  "+pos+"  ctrl+n/ctrl+p · échap")
}

func (m *model) jumpTurn(delta int) {
	tab := m.tabs[m.active]
	if tab == nil {
		return
	}
	bounds := m.layoutTranscript(tab, m.vp.Width).turns
	if len(bounds) == 0 {
		return
	}
	cur := m.vp.YOffset
	if delta > 0 {
		for _, bounds := range bounds {
			if bounds > cur {
				m.vp.SetYOffset(bounds)
				return
			}
		}
		m.vp.SetYOffset(bounds[len(bounds)-1])
		return
	}
	for index := len(bounds) - 1; index >= 0; index-- {
		if bounds[index] < cur {
			m.vp.SetYOffset(bounds[index])
			return
		}
	}
	m.vp.SetYOffset(bounds[0])
}

func foldTurns(lines []string, bounds []int, keep int, styles uiStyles) []string {
	if keep < 1 {
		keep = 1
	}
	if len(bounds) <= keep {
		return lines
	}
	cut := bounds[len(bounds)-keep]
	var out []string
	for index := range bounds {
		start := bounds[index]
		if start >= cut {
			return append(out, lines[start:]...)
		}
		if index == 0 && start > 0 {
			out = append(out, lines[:start]...)
		}
		end := bounds[index+1]
		out = append(out, foldedTurnLine(lines[start], end-start, styles))
	}
	return out
}

func foldedTurnLine(head string, lines int, st uiStyles) string {
	return truncate(head, foldedHeadWidth) + st.dim.Render(fmt.Sprintf(" %s %d lines", glyphFold, lines))
}

const turnFoldKeep = 2

func (m *model) toggleTurnFold() {
	m.foldTurnsOn = !m.foldTurnsOn
	m.syncViewport()
}
