package tui

import (
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

const (
	fuzzyMatchScore     = 4
	fuzzyWordStartBonus = 6
	fuzzyConsecutive    = 5
	fuzzyGapPenalty     = 1
)

type fuzzyHit struct {
	score     int
	positions []int
}

func fuzzyMatch(query, target string) (fuzzyHit, bool) {
	needle := []rune(strings.ToLower(query))
	haystack := []rune(target)
	hit := fuzzyHit{}
	if len(needle) == 0 {
		return hit, true
	}
	next := 0
	previous := -2
	for at, r := range haystack {
		if next == len(needle) {
			break
		}
		if unicode.ToLower(r) != needle[next] {
			continue
		}
		hit.score += fuzzyMatchScore
		if at == previous+1 {
			hit.score += fuzzyConsecutive
		} else if previous >= 0 {
			hit.score -= fuzzyGapPenalty * (at - previous - 1)
		}
		if at == 0 || !unicode.IsLetter(haystack[at-1]) && !unicode.IsDigit(haystack[at-1]) {
			hit.score += fuzzyWordStartBonus
		}
		hit.positions = append(hit.positions, at)
		previous = at
		next++
	}
	if next != len(needle) {
		return fuzzyHit{}, false
	}
	return hit, true
}

func rankFuzzy(query string, items []string) []string {
	type scored struct {
		text string
		hit  fuzzyHit
	}
	var found []scored
	for _, item := range items {
		if hit, ok := fuzzyMatch(query, item); ok {
			found = append(found, scored{text: item, hit: hit})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].hit.score > found[j].hit.score })
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.text)
	}
	return out
}

func highlightFuzzy(text string, positions []int, plain, hot lipgloss.Style) string {
	marked := make(map[int]bool, len(positions))
	for _, at := range positions {
		marked[at] = true
	}
	var out strings.Builder
	var run []rune
	var runHot bool
	flush := func() {
		if len(run) == 0 {
			return
		}
		style := plain
		if runHot {
			style = hot
		}
		out.WriteString(style.Render(string(run)))
		run = run[:0]
	}
	for at, r := range []rune(text) {
		if marked[at] != runHot {
			flush()
			runHot = marked[at]
		}
		run = append(run, r)
	}
	flush()
	return out.String()
}

func fuzzyFilter[T any](items []T, query string, text func(T) string) []T {
	needle := strings.ToLower(strings.TrimSpace(query))
	out := make([]T, 0, len(items))
	for _, item := range items {
		if fuzzySubsequence(needle, text(item)) {
			out = append(out, item)
		}
	}
	return out
}

func fuzzySubsequence(needle, haystack string) bool {
	runes := []rune(strings.ToLower(haystack))
	pos := 0
	for _, want := range needle {
		found := -1
		for index := pos; index < len(runes); index++ {
			if runes[index] == want {
				found = index
				break
			}
		}
		if found < 0 {
			return false
		}
		pos = found + 1
	}
	return true
}
