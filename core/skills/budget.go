package skills

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher/core/contextwindow"
)

const (
	minWordRunes = 3
	nameWeight   = 2
)

// Budget caps the skill text injected each turn, in estimated tokens. A zero or
// negative field leaves that block unbounded.
type Budget struct {
	Menu       int
	Expansions int
}

// Notices renders the sentences that tell the model what a budget hid, so the
// hidden skill is named and can still be read in full. The caller supplies the
// localized text.
type Notices interface {
	MenuMore(hidden int) string
	ExpansionCut(name, path string) string
}

func (e *Engine) selectMenu(message string, notes Notices) (map[string]bool, int) {
	all := make(map[string]bool, len(e.order))
	for _, name := range e.order {
		all[name] = true
	}
	if e.budget.Menu <= 0 || contextwindow.EstimateTokens(e.renderMenu(all, 0, notes)) <= e.budget.Menu {
		return all, 0
	}
	shown := make(map[string]bool, len(e.order))
	for _, name := range e.order {
		if e.active[name] {
			shown[name] = true
		}
	}
	for _, name := range e.rankedInactive(message) {
		shown[name] = true
		hidden := len(e.order) - len(shown)
		if contextwindow.EstimateTokens(e.renderMenu(shown, hidden, notes)) > e.budget.Menu {
			delete(shown, name)
		}
	}
	return shown, len(e.order) - len(shown)
}

func (e *Engine) rankedInactive(message string) []string {
	words := wordSet(message)
	scores := make(map[string]int, len(e.order))
	var names []string
	for _, name := range e.order {
		if e.active[name] {
			continue
		}
		names = append(names, name)
		scores[name] = relevance(words, e.byName[name])
	}
	slices.SortStableFunc(names, func(first, second string) int {
		if c := cmp.Compare(scores[second], scores[first]); c != 0 {
			return c
		}
		return cmp.Compare(first, second)
	})
	return names
}

func relevance(words map[string]bool, skill Skill) int {
	score := 0
	for word := range wordSet(skill.Name) {
		if words[word] {
			score += nameWeight
		}
	}
	for word := range wordSet(skill.Description) {
		if words[word] {
			score++
		}
	}
	return score
}

func wordSet(text string) map[string]bool {
	words := make(map[string]bool)
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, field := range fields {
		if utf8.RuneCountInString(field) >= minWordRunes {
			words[field] = true
		}
	}
	return words
}

func fitTokens(body string, tokens int) string {
	if tokens <= 0 {
		return ""
	}
	limit := contextwindow.BytesForTokens(tokens)
	if len(body) <= limit {
		return body
	}
	kept := body[:limit]
	if line := strings.LastIndexByte(kept, '\n'); line >= 0 {
		kept = kept[:line]
	}
	for kept != "" && !utf8.RuneStart(body[len(kept)]) {
		kept = kept[:len(kept)-1]
	}
	return kept
}
