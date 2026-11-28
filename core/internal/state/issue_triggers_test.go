package state

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestAdvanceIssueWatchOnlyMovesFromTheExpectedWatch(t *testing.T) {
	store := NewState(filepath.Join(t.TempDir(), "state.json"))
	start := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	first := IssueWatch{Since: start}
	moved, err := store.AdvanceIssueWatch("github.com/a/b", "label:ready", IssueWatch{}, first)
	if err != nil || !moved {
		t.Fatalf("first advance = %v, %v; want it to apply", moved, err)
	}
	stale, err := store.AdvanceIssueWatch("github.com/a/b", "label:ready", IssueWatch{}, IssueWatch{Since: start.Add(time.Hour)})
	if err != nil || stale {
		t.Fatalf("advance from a stale watch = %v, %v; want refused", stale, err)
	}
	if got := store.IssueTriggerRepo("github.com/a/b").Watches["label:ready"]; !got.Since.Equal(start) {
		t.Fatalf("watch = %+v, want the first one kept", got)
	}
}

func TestClaimIssueTakesEachNumberOnceAndBoundsTheList(t *testing.T) {
	store := NewState(filepath.Join(t.TempDir(), "state.json"))
	claimed, err := store.ClaimIssue("github.com/a/b", 1, "issue-a-b-1")
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v", claimed, err)
	}
	again, err := store.ClaimIssue("github.com/a/b", 1, "issue-a-b-1")
	if err != nil || again {
		t.Fatalf("second claim = %v, %v; want refused", again, err)
	}
	for number := 2; number <= maxTakenIssues+10; number++ {
		if _, err := store.ClaimIssue("github.com/a/b", number, fmt.Sprintf("issue-a-b-%d", number)); err != nil {
			t.Fatal(err)
		}
	}
	record := store.IssueTriggerRepo("github.com/a/b")
	if len(record.Taken) != maxTakenIssues || record.Taken[len(record.Taken)-1].Number != maxTakenIssues+10 {
		t.Fatalf("taken = %d entries, want the newest %d", len(record.Taken), maxTakenIssues)
	}
}

func TestReleaseIssueLetsTheNextReadTakeItAgain(t *testing.T) {
	store := NewState(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.ClaimIssue("github.com/a/b", 5, "issue-a-b-5"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseIssue("github.com/a/b", 5); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimIssue("github.com/a/b", 5, "issue-a-b-5")
	if err != nil || !claimed {
		t.Fatalf("claim after release = %v, %v", claimed, err)
	}
}
