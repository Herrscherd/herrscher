package state

import (
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestClaimPullRequestEndWaitsForAnEndedPullRequest(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main"}))
	must(t, s.RecordPullRequest("main", PullRequestRecord{Number: 7, State: contracts.PullRequestOpen}))

	claimed, err := s.ClaimPullRequestEnd("main")
	must(t, err)
	if claimed {
		t.Fatal("ClaimPullRequestEnd on an open pull request claimed the end, want none")
	}
}

func TestClaimPullRequestEndAnnouncesOncePerPullRequestAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main"}))
	must(t, s.RecordPullRequest("main", PullRequestRecord{Number: 7, State: contracts.PullRequestMerged}))

	first, err := s.ClaimPullRequestEnd("main")
	must(t, err)
	if !first {
		t.Fatal("first ClaimPullRequestEnd on a merged pull request = false, want true")
	}
	again, err := s.ClaimPullRequestEnd("main")
	must(t, err)
	if again {
		t.Fatal("second ClaimPullRequestEnd in the same process = true, want false")
	}

	reloaded, err := LoadState(path)
	must(t, err)
	afterRestart, err := reloaded.ClaimPullRequestEnd("main")
	must(t, err)
	if afterRestart {
		t.Fatal("ClaimPullRequestEnd after a restart = true, want the claim kept in the file")
	}
}

func TestClaimPullRequestEndRollsBackWhenSaveFails(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main"}))
	must(t, s.RecordPullRequest("main", PullRequestRecord{Number: 7, State: contracts.PullRequestMerged}))
	s.write = failEveryWrite

	if _, err := s.ClaimPullRequestEnd("main"); err == nil {
		t.Fatal("ClaimPullRequestEnd with a failing save returned nil, want the save failure")
	}
	if got, _ := s.FindSession("main"); got.PullRequestEndNotified != 0 {
		t.Fatalf("memory after a failed claim holds %d, want no claim", got.PullRequestEndNotified)
	}
}
