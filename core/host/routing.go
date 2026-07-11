package host

import (
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// tokenizeTask lowercases task and splits it into a set of alphanumeric tokens —
// the vocabulary a task's wording offers for matching against agent tags.
func tokenizeTask(task string) map[string]bool {
	set := map[string]bool{}
	for _, tok := range strings.FieldsFunc(strings.ToLower(task), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		set[tok] = true
	}
	return set
}

// pickAgent scores each agent's tags against the task's token set and returns the
// highest-scoring agent. Score = number of the agent's tags present as a token in
// the task. The roster is expected sorted by name (Store.List), so the first agent
// reaching the max score wins — ties break to the lexicographically smallest name,
// deterministically. ok=false when every score is 0 (no agent matches): the host
// refuses rather than falling back to a default, which would be a hidden judgment.
// Pure and LLM-free — this is the whole of the routing "decision" (Model O).
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
