package host

import (
	"encoding/json"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const prTestURL = "https://github.com/acme/widget/pull/42"

func TestPROpenRecordsTheWholePullRequest(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	pullRequest := prNumbered(t)
	pullRequest.Draft = true
	pullRequest.URL = prTestURL
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullRequest: pullRequest}))

	if _, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre"); err != nil {
		t.Fatalf("open: %v", err)
	}
	sess, _ := st.FindSession("alpha")
	want := state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen, Draft: true, URL: prTestURL}
	if got := sess.PullRequestRecord(); got != want {
		t.Fatalf("recorded pull request = %+v, want %+v", got, want)
	}
}

func TestPRStatusRefreshesTheRecordedState(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen, Draft: true, URL: "https://github.com/acme/widget/pull/1"}); err != nil {
		t.Fatal(err)
	}
	merged := prNumbered(t)
	merged.State = contracts.PullRequestMerged
	merged.URL = prTestURL
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullRequest: merged}))

	if _, err := runPR(t, reg, verbPRStatus, "--name", "alpha"); err != nil {
		t.Fatalf("status: %v", err)
	}
	sess, _ := st.FindSession("alpha")
	want := state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestMerged, Draft: false, URL: prTestURL}
	if got := sess.PullRequestRecord(); got != want {
		t.Fatalf("refreshed pull request = %+v, want %+v", got, want)
	}
}

func TestSessionInfoCarriesAKnownPullRequest(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen, Draft: true, URL: prTestURL}); err != nil {
		t.Fatal(err)
	}

	infos := sessionInfos(st, t.TempDir(), compactPolicy(0), modelTermsFor(nil))
	want := contracts.SessionPullRequest{Number: prTestNumber, State: contracts.PullRequestOpen, Draft: true, URL: prTestURL}
	if len(infos) != 1 || infos[0].PullRequest != want {
		t.Fatalf("session info pull request = %+v, want %+v", infos, want)
	}
	if !hasJSONKey(t, infos[0], "PullRequest") {
		t.Fatal("encoded session info omits PullRequest although the session has one")
	}
}

func TestSessionInfoOmitsAPullRequestWhoseStateIsUnknown(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "alpha", PullRequest: prTestNumber}); err != nil {
		t.Fatal(err)
	}

	infos := sessionInfos(st, t.TempDir(), compactPolicy(0), modelTermsFor(nil))
	if len(infos) != 1 || infos[0].PullRequest != (contracts.SessionPullRequest{}) {
		t.Fatalf("session info pull request = %+v, want none for an unknown state", infos)
	}
	if hasJSONKey(t, infos[0], "PullRequest") {
		t.Fatal("encoded session info carries a pull request whose state is unknown")
	}
	if _, err := json.Marshal(infos[0]); err != nil {
		t.Fatalf("session info with a legacy pull request does not encode: %v", err)
	}
}

func hasJSONKey(t *testing.T, value any, key string) bool {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	_, ok := fields[key]
	return ok
}
