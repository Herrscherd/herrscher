package state

import (
	"errors"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestRecordPullRequestRollsBackWhenSaveFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main"}))
	s.write = failEveryWrite

	if err := s.RecordPullRequest("main", PullRequestRecord{Number: 7, State: contracts.PullRequestOpen}); !errors.Is(err, errInjectedSave) {
		t.Fatalf("RecordPullRequest error = %v, want the save failure", err)
	}
	if got, _ := s.FindSession("main"); got.PullRequest != 0 {
		t.Fatalf("memory after a failed RecordPullRequest holds pull request %d, want none", got.PullRequest)
	}
	reloaded, err := LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.PullRequest != 0 {
		t.Fatalf("file after a failed RecordPullRequest holds pull request %d, want none", got.PullRequest)
	}
}

func TestRecordPullRequestRefusesASecondNumber(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main"}))
	must(t, s.RecordPullRequest("main", PullRequestRecord{Number: 7, State: contracts.PullRequestOpen}))

	if err := s.RecordPullRequest("main", PullRequestRecord{Number: 8, State: contracts.PullRequestOpen}); !errors.Is(err, ErrPullRequestRecorded) {
		t.Fatalf("second RecordPullRequest error = %v, want ErrPullRequestRecorded", err)
	}
	if got, _ := s.FindSession("main"); got.PullRequest != 7 {
		t.Fatalf("pull request = %d, want 7 kept", got.PullRequest)
	}
}

func TestReservePullRequestRefusesASecondReservationUntilRelease(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main"}))

	must(t, s.ReservePullRequest("main"))
	if err := s.ReservePullRequest("main"); !errors.Is(err, ErrPullRequestOpening) {
		t.Fatalf("second ReservePullRequest error = %v, want ErrPullRequestOpening", err)
	}
	must(t, s.ReservePullRequest("other"))
	s.ReleasePullRequest("main")
	if err := s.ReservePullRequest("main"); err != nil {
		t.Fatalf("ReservePullRequest after release error = %v, want nil", err)
	}
}
