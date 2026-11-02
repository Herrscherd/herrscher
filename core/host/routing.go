package host

import (
	"strings"
	"unicode"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func tokenizeTask(task string) map[string]bool {
	set := map[string]bool{}
	for _, tok := range strings.FieldsFunc(strings.ToLower(task), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		set[tok] = true
	}
	return set
}

func pickAgent(roster []agent.Agent, task string) (string, bool) {
	tokens := tokenizeTask(task)
	best := ""
	bestScore := 0
	for _, a := range roster {
		score := 0
		for _, tag := range a.Tags {
			if tokens[strings.ToLower(tag)] {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = a.Name
		}
	}
	if bestScore == 0 {
		return "", false
	}
	return best, true
}
