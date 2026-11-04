package host

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifySessionPrefersWaitingThenWorkingThenArchived(t *testing.T) {
	cases := []struct {
		name                               string
		waiting, working, archived, review bool
		want                               string
	}{
		{"waiting beats everything", true, true, true, true, overviewWaiting},
		{"working beats archived", false, true, true, true, overviewWorking},
		{"archived when quiet", false, false, true, true, overviewArchived},
		{"unread finished answer is review", false, false, false, true, overviewReview},
		{"idle otherwise", false, false, false, false, overviewIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySession(tc.waiting, tc.working, tc.archived, tc.review); got != tc.want {
				t.Fatalf("classifySession = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTallyCountsCostAndLastEntryIncrementally(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeTranscript(t, path, `{"ts":"2026-01-01T10:00:00Z","role":"user","text":"salut"}`+"\n"+
		`{"ts":"2026-01-01T10:00:05Z","role":"assistant","text":"bonjour","cost":0.25}`+"\n")
	cache := newTallyCache()

	first := cache.tally(path)
	if first.cost != 0.25 || first.lastTs != "2026-01-01T10:00:05Z" || first.lastText != "bonjour" {
		t.Fatalf("first tally = %+v", first)
	}

	appendTranscript(t, path, `{"ts":"2026-01-01T10:01:00Z","role":"assistant","text":"fini","cost":0.5}`+"\n")
	second := cache.tally(path)
	if second.cost != 0.75 || second.lastTs != "2026-01-01T10:01:00Z" || second.lastText != "fini" {
		t.Fatalf("second tally = %+v", second)
	}
}

func TestTallyIgnoresPartialLineUntilComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeTranscript(t, path, `{"ts":"2026-01-01T10:00:00Z","role":"assistant","text":"a","cost":1`)
	cache := newTallyCache()
	if got := cache.tally(path); got.cost != 0 {
		t.Fatalf("partial line counted: %+v", got)
	}
	appendTranscript(t, path, `}`+"\n")
	if got := cache.tally(path); got.cost != 1 {
		t.Fatalf("completed line not counted: %+v", got)
	}
}

func TestTallyOfMissingTranscriptIsEmpty(t *testing.T) {
	cache := newTallyCache()
	if got := cache.tally(filepath.Join(t.TempDir(), "absent.jsonl")); got != (transcriptTally{}) {
		t.Fatalf("missing transcript tally = %+v", got)
	}
}

func writeTranscript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendTranscript(t *testing.T, path, body string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(body); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFinishedAnswerNeedsReviewUntilOperatorSeesIt(t *testing.T) {
	floor := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	seen := newSeenTable(floor)
	const done = "2026-01-01T10:30:00Z"
	if !needsReview(done, seen.since("alpha")) {
		t.Fatal("an answer finished after the daemon started must need review")
	}
	seen.mark("alpha", time.Date(2026, 1, 1, 10, 31, 0, 0, time.UTC))
	if needsReview(done, seen.since("alpha")) {
		t.Fatal("an answer read after it finished must not need review")
	}
}

func TestAnswerFinishedBeforeDaemonStartIsNotReview(t *testing.T) {
	floor := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if needsReview("2026-01-01T09:00:00Z", newSeenTable(floor).since("alpha")) {
		t.Fatal("an answer from before the daemon started must not need review")
	}
}
