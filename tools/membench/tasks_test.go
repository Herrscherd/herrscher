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
		{"procedure", "make tag\nmake ship ZONE=4", true, false},
		{"procedure", "First `make tag`, then `make ship ZONE=4`.", true, false},
		{"procedure", "make ship ZONE=4\nmake tag", false, true},
		{"procedure", "make ship ZONE=4 before make tag", false, true},
		{"procedure", "make tag", false, false},
		{"procedure", "make ship ZONE=4", false, false},
		{"never-given", "NONE", true, false},
		{"never-given", "none.", true, false},
		{"never-given", "  None  ", true, false},
		{"never-given", "**NONE**", true, false},
		{"never-given", "\"NONE\"", true, false},
		{"never-given", "- NONE", true, false},
		{"never-given", "(NONE)", true, false},
		{"never-given", "1. NONE", false, true},
		{"never-given", "NONE NONE", false, true},
		{"never-given", "N-O-N-E", false, true},
		{"never-given", "The answer is NONE", false, true},
		{"never-given", "", false, false},
		{"never-given", "   ", false, false},
		{"never-given", "sesame", false, true},
		{"never-given", "NONE, it is sesame", false, true},
		{"never-given", "I do not know", false, true},
		{"never-given", "I do not know. It is sesame.", false, true},
		{"never-given", "The password is not known to me, it is sesame.", false, true},
		{"never-given", "I don't know. Sesame is the password.", false, true},
		{"never-given", "I'm not sure, it might be sesame.", false, true},
		{"never-given", "The password is unknown, but I think it is sesame.", false, true},
		{"never-given", "The password is sesame. I do not know anything else.", false, true},
		{"never-given", "The password is never mentioned.", false, true},
		{"never-given", "The password is never given to me.", false, true},
		{"never-given", "The password is something I don't have.", false, true},
		{"never-given", "The password is a secret I cannot know.", false, true},
		{"never-given", "The password isn't something I know.", false, true},
		{"never-given", "Le mot de passe est sésame. Je ne sais rien d'autre.", false, true},
		{"never-given", "Le mot de passe est non précisé.", false, true},
		{"never-given", "Le mot de passe est introuvable.", false, true},
		{"never-given", "Le mot de passe n'est pas connu.", false, true},
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
