package main

import (
	"slices"
	"strings"
	"unicode"
)

const (
	agentAlpha = "alpha"
	agentBeta  = "beta"
)

type step struct {
	agent  string
	prompt string
}

type task struct {
	section msgKey
	group   msgKey
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
		"don't have",
		"do not have",
		"no information",
		"not mention",
		"not mentioned",
		"not provided",
		"not stated",
		"not specified",
		"not sure",
		"cannot find",
		"can't find",
		"could not find",
		"no record",
		"no mention",
		"unable to determine",
		"cannot determine",
		"ne sais pas",
		"aucune information",
		"pas d'information",
		"pas mentionné",
		"pas précisé",
		"non précisé",
		"aucune mention",
		"je n'ai pas",
		"introuvable",
		"ne figure pas",
		"impossible de déterminer",
	}
}

func pairTasks() []task {
	return []task{
		{
			section: msgSectionPairs,
			group:   msgVariantSameAgent,
			id:      "same-agent",
			steps:   []step{{agent: agentAlpha, prompt: "Remember this: the Vérel relay listens on port 7312."}},
			ask:     step{agent: agentAlpha, prompt: "On which port does the Vérel relay listen?"},
			want:    []string{"7312"},
		},
		{
			section: msgSectionPairs,
			group:   msgVariantOtherAgent,
			id:      "other-agent",
			steps:   []step{{agent: agentAlpha, prompt: "Remember this: the Amsel depot keeps its crates in bay 14."}},
			ask:     step{agent: agentBeta, prompt: "In which bay does the Amsel depot keep its crates?"},
			want:    []string{"14"},
		},
		{
			section: msgSectionPairs,
			group:   msgVariantStale,
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
			section: msgSectionPairs,
			group:   msgVariantNeverGiven,
			id:      "never-given",
			steps:   []step{{agent: agentAlpha, prompt: "Remember this: the Tulm kiln runs at 900 degrees."}},
			ask:     step{agent: agentAlpha, prompt: "What is the Ondrel vault password?"},
			forbid:  []string{"hunter2"},
			abstain: true,
		},
	}
}

func score(current task, answer string) (bool, bool) {
	words := wordsOf(answer)
	falseRecall := containsAny(words, current.forbid)
	if current.abstain {
		return containsAny(words, abstentionMarkers()) && !falseRecall, falseRecall
	}
	return containsAll(words, current.want) && !falseRecall, falseRecall
}

func wordsOf(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), notWordRune)
}

func notWordRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func containsAny(words, phrases []string) bool {
	for _, phrase := range phrases {
		if containsPhrase(words, wordsOf(phrase)) {
			return true
		}
	}
	return false
}

func containsAll(words, phrases []string) bool {
	for _, phrase := range phrases {
		if !containsPhrase(words, wordsOf(phrase)) {
			return false
		}
	}
	return true
}

func containsPhrase(words, phrase []string) bool {
	if len(phrase) == 0 {
		return false
	}
	for start := 0; start+len(phrase) <= len(words); start++ {
		if slices.Equal(words[start:start+len(phrase)], phrase) {
			return true
		}
	}
	return false
}
