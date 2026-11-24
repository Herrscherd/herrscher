package main

import (
	"testing"
)

func TestScoreRejectsStaleAndInventedAnswers(t *testing.T) {
	byID := make(map[string]task)
	for _, candidate := range pairTasks() {
		byID[candidate.id] = candidate
	}
	cases := []struct {
		id          string
		answer      string
		success     bool
		falseRecall bool
	}{
		{"same-agent", "The relay listens on port 7312.", true, false},
		{"same-agent", fakeNoAnswer, false, false},
		{"stale", "The gate code is 9056.", true, false},
		{"stale", "The gate code is 4481, then 9056.", false, true},
		{"stale", "It was 4481.", false, true},
		{"never-given", fakeNoAnswer, true, false},
		{"never-given", "The vault holds nothing else.", false, false},
		{"never-given", "The password is hunter2, I do not know anything else.", false, true},
		{"never-given", "The password is hunter2.", false, true},
	}
	for _, tc := range cases {
		success, falseRecall := score(byID[tc.id], tc.answer)
		if success != tc.success || falseRecall != tc.falseRecall {
			t.Errorf("%s with %q: success %v, false recall %v; want %v, %v", tc.id, tc.answer, success, falseRecall, tc.success, tc.falseRecall)
		}
	}
}
