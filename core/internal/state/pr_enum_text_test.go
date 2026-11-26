package state

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestPullRequestEnumsRoundTripEveryValue(t *testing.T) {
	for _, state := range contracts.PullRequestStates() {
		for merge := contracts.MergeStateUnknown; knownMergeState(merge); merge++ {
			for _, checks := range []contracts.ChecksState{contracts.ChecksUnknown, contracts.ChecksNone, contracts.ChecksPending, contracts.ChecksPassed, contracts.ChecksFailed, contracts.ChecksActionRequired} {
				session := Session{Name: "main", PullRequest: 7, PullRequestState: state, PullRequestMerge: merge, PullRequestChecks: checks}
				buf, err := json.Marshal(session)
				must(t, err)
				var back Session
				must(t, json.Unmarshal(buf, &back))
				if back.PullRequestState != state || back.PullRequestMerge != merge || back.PullRequestChecks != checks {
					t.Fatalf("round trip of %s/%s/%s through %s gave %s/%s/%s", state, merge, checks, buf, back.PullRequestState, back.PullRequestMerge, back.PullRequestChecks)
				}
			}
		}
	}
}

func TestPullRequestEnumsAreStoredAsText(t *testing.T) {
	session := Session{Name: "main", PullRequest: 7, PullRequestState: contracts.PullRequestMerged, PullRequestMerge: contracts.MergeStateBlocked, PullRequestChecks: contracts.ChecksActionRequired}
	buf, err := json.Marshal(session)
	must(t, err)
	for _, want := range []string{`"pullRequestState":"merged"`, `"pullRequestMerge":"blocked"`, `"pullRequestChecks":"action_required"`} {
		if !strings.Contains(string(buf), want) {
			t.Fatalf("stored session %s lacks %s", buf, want)
		}
	}
}

func TestSessionWithoutPullRequestEnumsReadsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(path, []byte(`{"sessions":[{"name":"main","pullRequest":42}]}`), 0o600))

	s, err := LoadState(path)
	must(t, err)
	got, ok := s.FindSession("main")
	if !ok {
		t.Fatal("legacy session did not load")
	}
	if got.PullRequestState != 0 || got.PullRequestMerge != contracts.MergeStateUnknown || got.PullRequestChecks != contracts.ChecksUnknown {
		t.Fatalf("absent enums read as %v/%v/%v, want the unknown zero values", got.PullRequestState, got.PullRequestMerge, got.PullRequestChecks)
	}
}

func TestUnrecognisedEnumTextReadsUnknownAndWarns(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	path := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(path, []byte(`{"sessions":[{"name":"main","pullRequest":42,"pullRequestState":"reopened","pullRequestMerge":"sideways","pullRequestChecks":"maybe","pullRequestUrl":"u"}]}`), 0o600))

	s, err := LoadState(path)
	must(t, err)
	got, ok := s.FindSession("main")
	if !ok {
		t.Fatal("session with unknown enum text did not load")
	}
	if got.PullRequest != 42 || got.PullRequestURL != "u" {
		t.Fatalf("session fields beside the unknown text = %d %q, want 42 and u", got.PullRequest, got.PullRequestURL)
	}
	if got.PullRequestState != 0 || got.PullRequestMerge != contracts.MergeStateUnknown || got.PullRequestChecks != contracts.ChecksUnknown {
		t.Fatalf("unknown text read as %v/%v/%v, want the unknown zero values", got.PullRequestState, got.PullRequestMerge, got.PullRequestChecks)
	}
	for _, value := range []string{"reopened", "sideways", "maybe"} {
		if !strings.Contains(logs.String(), value) {
			t.Fatalf("log %q does not name the unknown value %s", logs.String(), value)
		}
	}
}

func TestSessionWrittenByMasterWithIntegersLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(path, []byte(`{"sessions":[{"name":"main","pullRequest":7,"pullRequestState":2,"pullRequestMerge":1,"pullRequestChecks":3,"pullRequestUrl":"u"}]}`), 0o600))

	s, err := LoadState(path)
	must(t, err)
	got, ok := s.FindSession("main")
	if !ok {
		t.Fatal("session written with integers did not load")
	}
	if got.PullRequestState != contracts.PullRequestClosed || got.PullRequestMerge != contracts.MergeStateClean || got.PullRequestChecks != contracts.ChecksPassed {
		t.Fatalf("integers 2/1/3 read as %v/%v/%v, want closed/clean/passed", got.PullRequestState, got.PullRequestMerge, got.PullRequestChecks)
	}
	if got.PullRequest != 7 || got.PullRequestURL != "u" {
		t.Fatalf("session fields beside the integers = %d %q, want 7 and u", got.PullRequest, got.PullRequestURL)
	}
}

func TestOutOfRangeIntegersReadUnknownAndWarn(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	path := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(path, []byte(`{"sessions":[{"name":"main","pullRequest":7,"pullRequestState":9,"pullRequestMerge":42,"pullRequestChecks":-1}]}`), 0o600))

	s, err := LoadState(path)
	must(t, err)
	got, ok := s.FindSession("main")
	if !ok {
		t.Fatal("session with out-of-range integers did not load")
	}
	if got.PullRequestState != 0 || got.PullRequestMerge != contracts.MergeStateUnknown || got.PullRequestChecks != contracts.ChecksUnknown {
		t.Fatalf("out-of-range integers read as %v/%v/%v, want the unknown zero values", got.PullRequestState, got.PullRequestMerge, got.PullRequestChecks)
	}
	for _, value := range []string{"9", "42", "-1"} {
		if !strings.Contains(logs.String(), "value="+value) {
			t.Fatalf("log %q does not name the out-of-range value %s", logs.String(), value)
		}
	}
}
