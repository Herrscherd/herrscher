package main

import (
	"fmt"
	"strings"
	"testing"
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
	items, _, err := sampleItems(strings.NewReader(fixture), samples, seed, 6)
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
