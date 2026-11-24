package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const lmeFixture = `[
 {"question_id":"q1","question_type":"single-session-user","question":"Where do I live?","answer":"Lyon","haystack_session_ids":["s1","s2"],"haystack_sessions":[[{"role":"user","content":"I live in Lyon."}],[{"role":"user","content":"I like tea."}]],"answer_session_ids":["s1"]},
 {"question_id":"q2","question_type":"multi-session","question":"How many trips?","answer":3,"haystack_session_ids":["s1","s2"],"haystack_sessions":[[{"role":"user","content":"Trip one."}],[{"role":"user","content":"Trip two."}]],"answer_session_ids":["s1","s2"]},
 {"question_id":"q3","question_type":"other-type","question":"Ignored?","answer":"no","haystack_session_ids":["s1"],"haystack_sessions":[[{"role":"user","content":"x"}]],"answer_session_ids":["s1"]},
 {"question_id":"q4","question_type":"single-session-user","question":"No evidence?","answer":"no","haystack_session_ids":["s1"],"haystack_sessions":[[{"role":"user","content":"x"}]],"answer_session_ids":[]},
 {"question_id":"q5_abs","question_type":"multi-session","question":"Unknown thing?","answer":"","haystack_session_ids":["s1"],"haystack_sessions":[[{"role":"user","content":"x"}]],"answer_session_ids":["s1"]},
 {"question_id":"q6","question_type":"temporal-reasoning","question":"Which came first?","answer":"alpha","haystack_session_ids":["s1","s2","s3","s4","s5"],"haystack_sessions":[[{"role":"user","content":"1"}],[{"role":"user","content":"2"}],[{"role":"user","content":"3"}],[{"role":"user","content":"4"}],[{"role":"user","content":"5"}]],"answer_session_ids":["s1","s2","s3","s4","s5"]}
]`

func TestSampleItemsKeepsOnlyEligibleItems(t *testing.T) {
	items, eligible, err := sampleItems(strings.NewReader(lmeFixture), 100, 1, 6)
	if err != nil {
		t.Fatal(err)
	}
	if eligible != 4 {
		t.Errorf("eligible %d, want 4 (q3 has an unknown type, q4 no evidence)", eligible)
	}
	var ids []string
	for _, item := range items {
		ids = append(ids, item.QuestionID)
	}
	slices.Sort(ids)
	if want := []string{"q1", "q2", "q5_abs", "q6"}; !slices.Equal(ids, want) {
		t.Errorf("sampled %v, want %v", ids, want)
	}
}

func TestSampleItemsHonoursMaxTurns(t *testing.T) {
	_, eligible, err := sampleItems(strings.NewReader(lmeFixture), 100, 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if eligible != 3 {
		t.Errorf("eligible %d with max turns 5, want 3 (q6 needs six turns)", eligible)
	}
}

func TestSampleItemsIsSeeded(t *testing.T) {
	first, _, err := sampleItems(strings.NewReader(lmeFixture), 2, 3, 6)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := sampleItems(strings.NewReader(lmeFixture), 2, 3, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("sampled %d items, want 2", len(first))
	}
	for i := range first {
		if first[i].QuestionID != second[i].QuestionID {
			t.Errorf("same seed sampled %q then %q", first[i].QuestionID, second[i].QuestionID)
		}
	}
}

func TestLoadLongMemEvalBuildsTasksPerCapability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "longmemeval.json")
	if err := os.WriteFile(path, []byte(lmeFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	tasks, err := loadLongMemEval(path, 100, 1, 6)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]task)
	for _, built := range tasks {
		byID[built.id] = built
	}
	if got := byID["q1"]; got.group != clitext.MembenchCapExtraction || !slices.Equal(got.want, []string{"lyon"}) || len(got.steps) != 1 {
		t.Errorf("q1 = group %s, want %v, %d steps; want extraction, lyon, one step", got.group, got.want, len(got.steps))
	}
	if got := byID["q2"]; got.group != clitext.MembenchCapMultisession || !slices.Equal(got.want, []string{"3"}) || len(got.steps) != 2 {
		t.Errorf("q2 = group %s, want %v, %d steps; want multisession, 3, two steps", got.group, got.want, len(got.steps))
	}
	if got := byID["q5_abs"]; got.group != clitext.MembenchCapAbstention || !got.abstain {
		t.Errorf("q5_abs = group %s, abstain %v; want abstention with abstain set", got.group, got.abstain)
	}
	if got := byID["q6"]; got.group != clitext.MembenchCapTemporal || len(got.steps) != 5 {
		t.Errorf("q6 = group %s, %d steps; want temporal with five steps", got.group, len(got.steps))
	}
}

func TestLoadLongMemEvalRejectsBadShapes(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"not an array": `{}`,
		"no eligible":  `[]`,
	}
	for name, content := range cases {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := loadLongMemEval(path, 10, 1, 6)
		known, ok := errors.AsType[messageError](err)
		if !ok || known.key != clitext.MembenchErrDatasetShape {
			t.Errorf("%s: error %v, want a dataset shape error", name, err)
		}
	}
}
