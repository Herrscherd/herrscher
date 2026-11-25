package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestLegacyPullRequestNumberLoadsWithoutAState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(path, []byte(`{"sessions":[{"name":"main","pullRequest":42}]}`), 0o600))

	s, err := LoadState(path)
	must(t, err)
	got, ok := s.FindSession("main")
	if !ok {
		t.Fatal("legacy session did not load")
	}
	record := got.PullRequestRecord()
	if record.Number != 42 {
		t.Fatalf("legacy pull request number = %d, want 42", record.Number)
	}
	if record.Known() {
		t.Fatalf("legacy record %+v reports a known state, want unknown", record)
	}
}

func TestPullRequestRecordIsKnownOnlyWithANumberAndAForgeState(t *testing.T) {
	cases := []struct {
		name   string
		record PullRequestRecord
		want   bool
	}{
		{"no pull request", PullRequestRecord{}, false},
		{"negative number", PullRequestRecord{Number: -1, State: contracts.PullRequestOpen}, false},
		{"number without state", PullRequestRecord{Number: 42}, false},
		{"number with unnamed state", PullRequestRecord{Number: 42, State: contracts.PullRequestState(99)}, false},
		{"open", PullRequestRecord{Number: 42, State: contracts.PullRequestOpen}, true},
		{"closed", PullRequestRecord{Number: 42, State: contracts.PullRequestClosed}, true},
		{"merged", PullRequestRecord{Number: 42, State: contracts.PullRequestMerged}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.record.Known(); got != tc.want {
				t.Fatalf("Known() = %t for %+v, want %t", got, tc.record, tc.want)
			}
		})
	}
}

func TestRefreshPullRequestReplacesStateDraftAndURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main"}))
	must(t, s.RecordPullRequest("main", PullRequestRecord{Number: 7, State: contracts.PullRequestOpen, Draft: true, URL: "https://example.test/pull/7"}))

	refreshed := PullRequestRecord{Number: 7, State: contracts.PullRequestMerged, Draft: false, URL: "https://example.test/pull/7/merged"}
	must(t, s.RefreshPullRequest("main", refreshed))

	reloaded, err := LoadState(path)
	must(t, err)
	got, _ := reloaded.FindSession("main")
	if got := got.PullRequestRecord(); got != refreshed {
		t.Fatalf("reloaded record = %+v, want %+v", got, refreshed)
	}
}

func TestRefreshPullRequestRefusesAnotherNumber(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main"}))
	must(t, s.RecordPullRequest("main", PullRequestRecord{Number: 7, State: contracts.PullRequestOpen}))

	err := s.RefreshPullRequest("main", PullRequestRecord{Number: 8, State: contracts.PullRequestClosed})
	if !errors.Is(err, ErrPullRequestMismatch) {
		t.Fatalf("RefreshPullRequest error = %v, want ErrPullRequestMismatch", err)
	}
	if got, _ := s.FindSession("main"); got.PullRequestState != contracts.PullRequestOpen || got.PullRequest != 7 {
		t.Fatalf("record after a refused refresh = number %d state %v, want 7 open", got.PullRequest, got.PullRequestState)
	}
}

func TestRefreshPullRequestRollsBackWhenSaveFails(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main"}))
	original := PullRequestRecord{Number: 7, State: contracts.PullRequestOpen, Draft: true, URL: "https://example.test/pull/7"}
	must(t, s.RecordPullRequest("main", original))
	s.write = failEveryWrite

	err := s.RefreshPullRequest("main", PullRequestRecord{Number: 7, State: contracts.PullRequestMerged, URL: "https://example.test/pull/7/merged"})
	if !errors.Is(err, errInjectedSave) {
		t.Fatalf("RefreshPullRequest error = %v, want the save failure", err)
	}
	if got, _ := s.FindSession("main"); got.PullRequestRecord() != original {
		t.Fatalf("memory after a failed refresh = %+v, want %+v", got.PullRequestRecord(), original)
	}
}
