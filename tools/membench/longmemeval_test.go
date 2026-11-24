package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func lmeFixture(count int) string {
	var items []string
	for i := range count {
		items = append(items, fmt.Sprintf(`{"question_id":"q%03d","question_type":"multi-session","question":"What?","answer":"ans%d","haystack_session_ids":["s1","s2"],"haystack_sessions":[[{"role":"user","content":"hi"}],[{"role":"user","content":"yo"}]],"answer_session_ids":["s1"]}`, i, i))
	}
	return "[" + strings.Join(items, ",") + "]"
}

func sampledIDs(t *testing.T, fixture string, samples int, seed uint64) []string {
	t.Helper()
	items, _, _, err := sampleItems(strings.NewReader(fixture), samples, seed, 6)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(items))
	for index := range items {
		ids = append(ids, items[index].QuestionID)
	}
	return ids
}

func TestSampleIsDeterministicAndBounded(t *testing.T) {
	fixture := lmeFixture(50)
	first := sampledIDs(t, fixture, 5, 7)
	again := sampledIDs(t, fixture, 5, 7)
	if len(first) != 5 || strings.Join(first, ",") != strings.Join(again, ",") {
		t.Errorf("sample %v then %v, want five items, the same both times", first, again)
	}
	other := sampledIDs(t, fixture, 5, 8)
	if strings.Join(first, ",") == strings.Join(other, ",") {
		t.Errorf("seeds 7 and 8 sampled the same items %v", first)
	}
}

func TestSampleSkipsIneligibleItems(t *testing.T) {
	fixture := `[{"question_id":"keep","question_type":"multi-session","question":"q","answer":"a","haystack_session_ids":["s1"],"haystack_sessions":[[{"role":"user","content":"x"}]],"answer_session_ids":["s1"]},` +
		`{"question_id":"no-evidence","question_type":"multi-session","question":"q","answer":"a","haystack_session_ids":["s1"],"haystack_sessions":[[]],"answer_session_ids":[]},` +
		`{"question_id":"unknown-type","question_type":"other","question":"q","answer":"a","haystack_session_ids":["s1"],"haystack_sessions":[[]],"answer_session_ids":["s1"]}]`
	ids := sampledIDs(t, fixture, 10, 1)
	if strings.Join(ids, ",") != "keep" {
		t.Errorf("sampled %v, want only the eligible item", ids)
	}
}

func TestItemTaskAsksTheQuestionAfterItsEvidence(t *testing.T) {
	item := lmeItem{
		QuestionID:   "q1",
		QuestionType: "knowledge-update",
		Question:     "Where now?",
		Answer:       "Lisbon",
		SessionIDs:   []string{"s1", "s2"},
		Sessions:     [][]lmeTurn{{{Role: "user", Content: "I live in Porto."}}, {{Role: "user", Content: "I moved to Lisbon."}}},
		Evidence:     []string{"s2"},
	}
	built := itemTask(item)
	if len(built.steps) != 1 || !strings.HasPrefix(built.steps[0].prompt, rememberPrefix) {
		t.Fatalf("steps %+v, want one remember step for the evidence session", built.steps)
	}
	if built.ask.prompt != "Where now?" || len(built.want) != 1 || built.want[0] != "Lisbon" {
		t.Errorf("ask %+v, want %q", built.ask, "Where now?")
	}
}

func TestSampleCountsItemsTooLongForAnArgument(t *testing.T) {
	long := strings.Repeat("y", maxPromptBytes)
	fixture := fmt.Sprintf(`[{"question_id":"keep","question_type":"multi-session","question":"q","answer":"a","haystack_session_ids":["s1"],"haystack_sessions":[[{"role":"user","content":"x"}]],"answer_session_ids":["s1"]},`+
		`{"question_id":"too-long","question_type":"multi-session","question":"q","answer":"a","haystack_session_ids":["s1"],"haystack_sessions":[[{"role":"user","content":"%s"}]],"answer_session_ids":["s1"]}]`, long)
	items, _, skipped, err := sampleItems(strings.NewReader(fixture), 10, 1, 6)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || len(items) != 1 || items[0].QuestionID != "keep" {
		t.Errorf("items %d, skipped %d; want the eligible item kept and one skipped", len(items), skipped)
	}
}

func TestDatasetErrorsReadInFrench(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "empty", body: "[]", want: "jeu LongMemEval invalide : aucun item n'a de sessions de preuve, de type de question connu et de réponse"},
		{name: "malformed", body: `[{"question_id":`, want: "jeu LongMemEval invalide : JSON mal formé à l'octet 1"},
		{name: "delimiter", body: "{}", want: "jeu LongMemEval invalide : « [ » attendu à l'octet 1"},
	}
	msgs := loadCatalogue()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dataset.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := loadLongMemEval(path, 10, 1, 6)
			if got := failureText(msgs, i18n.French, err); got != tc.want {
				t.Errorf("french text %q, want %q", got, tc.want)
			}
		})
	}
}
