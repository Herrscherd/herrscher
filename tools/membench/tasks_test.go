package main

import "testing"

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
		{"same-agent", "I don't know.", false, false},
		{"stale", "The gate code is 9056.", true, false},
		{"stale", "The gate code is 4481, then 9056.", false, true},
		{"stale", "It was 4481.", false, true},
		{"never-given", "I don't know.", true, false},
		{"never-given", "The vault holds nothing else.", false, false},
		{"never-given", "The password is hunter2, I do not know anything else.", false, true},
		{"never-given", "The password is hunter2.", false, true},
		{"never-given", "The notes do not mention a vault password.", true, false},
		{"never-given", "I'm not sure, nothing about it was stated.", true, false},
		{"never-given", "I could not find that in my records.", true, false},
		{"never-given", "Je ne sais pas, ce n'est pas mentionné.", true, false},
		{"never-given", "Aucune mention de ce mot de passe, introuvable.", true, false},
		{"never-given", "Le mot de passe est hunter2.", false, true},
	}
	for _, tc := range cases {
		success, falseRecall := score(byID[tc.id], tc.answer)
		if success != tc.success || falseRecall != tc.falseRecall {
			t.Errorf("%s with %q: success %v, false recall %v; want %v, %v", tc.id, tc.answer, success, falseRecall, tc.success, tc.falseRecall)
		}
	}
}

func TestScoreMatchesWholeWordsOnly(t *testing.T) {
	same := pairTasks()[0]
	if ok, _ := score(same, "The relay listens on port 17312."); ok {
		t.Error("a longer number containing the expected value counted as a success")
	}
	stale := pairTasks()[2]
	if _, recall := score(stale, "The code is 44812."); recall {
		t.Error("a longer number containing the forbidden value counted as false recall")
	}
}
