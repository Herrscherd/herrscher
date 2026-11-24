package main

import (
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	agentAlpha            = "alpha"
	agentBeta             = "beta"
	typographicApostrophe = "’"
)

type step struct {
	agent  string
	prompt string
}

type task struct {
	section clitext.Key
	group   clitext.Key
	id      string
	steps   []step
	ask     step
	want    []string
	forbid  []string
	abstain bool
}

func abstentionMarkers() []string {
	return []string{
		"don't know",
		"do not know",
		"not know",
		"no information",
		"not mentioned",
		"not provided",
		"ne sais pas",
		"aucune information",
		"pas d'information",
	}
}

func pairTasks() []task {
	return []task{
		{
			section: clitext.MembenchSectionPairs,
			group:   clitext.MembenchVariantSameAgent,
			id:      "same-agent",
			steps:   []step{{agent: agentAlpha, prompt: "Remember this: the Vérel relay listens on port 7312."}},
			ask:     step{agent: agentAlpha, prompt: "On which port does the Vérel relay listen?"},
			want:    []string{"7312"},
		},
		{
			section: clitext.MembenchSectionPairs,
			group:   clitext.MembenchVariantOtherAgent,
			id:      "other-agent",
			steps:   []step{{agent: agentAlpha, prompt: "Remember this: the Amsel depot keeps its crates in bay 14."}},
			ask:     step{agent: agentBeta, prompt: "In which bay does the Amsel depot keep its crates?"},
			want:    []string{"14"},
		},
		{
			section: clitext.MembenchSectionPairs,
			group:   clitext.MembenchVariantStale,
			id:      "stale",
			steps: []step{
				{agent: agentAlpha, prompt: "Remember this: the Quorr gate code is 4481."},
				{agent: agentAlpha, prompt: "Correction: the Quorr gate code is 9056."},
			},
			ask:    step{agent: agentAlpha, prompt: "What is the Quorr gate code?"},
			want:   []string{"9056"},
			forbid: []string{"4481"},
		},
		{
			section: clitext.MembenchSectionPairs,
			group:   clitext.MembenchVariantNeverGiven,
			id:      "never-given",
			steps:   []step{{agent: agentAlpha, prompt: "Remember this: the Tulm kiln runs at 900 degrees."}},
			ask:     step{agent: agentAlpha, prompt: "What is the Ondrel vault password?"},
			forbid:  []string{"hunter2"},
			abstain: true,
		},
	}
}

func score(current task, answer string) (bool, bool) {
	lower := strings.ToLower(strings.ReplaceAll(answer, typographicApostrophe, "'"))
	falseRecall := containsAny(lower, current.forbid)
	if current.abstain {
		return containsAny(lower, abstentionMarkers()) && !falseRecall, falseRecall
	}
	return containsAll(lower, current.want) && !falseRecall, falseRecall
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func containsAll(text string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, strings.ToLower(needle)) {
			return false
		}
	}
	return true
}
