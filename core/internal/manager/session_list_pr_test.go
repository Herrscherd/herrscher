package manager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionListShowsTheStateOfThePullRequest(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{
		{Name: "alpha", PullRequest: 42, PullRequestState: contracts.PullRequestOpen, PullRequestDraft: true},
		{Name: "beta", PullRequest: 7, PullRequestState: contracts.PullRequestMerged},
		{Name: "gamma", PullRequest: 9},
		{Name: "delta"},
	}
	out, err := cli.Plain(h.sessionListRun(context.Background(), contracts.Input{}))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")[1:]
	wantSuffix := map[string]string{
		"alpha": h.text(clitext.SessionListPullRequest, clitext.Params{clitext.ParamNumber: 42, clitext.ParamState: h.text(clitext.SessionPRStateDraft, nil)}),
		"beta":  h.text(clitext.SessionListPullRequest, clitext.Params{clitext.ParamNumber: 7, clitext.ParamState: h.text(clitext.SessionPRStateMerged, nil)}),
		"gamma": h.text(clitext.SessionListPullRequest, clitext.Params{clitext.ParamNumber: 9, clitext.ParamState: h.text(clitext.SessionPRStateUnknown, nil)}),
	}
	for _, line := range lines {
		for name, suffix := range wantSuffix {
			if strings.Contains(line, "• "+name+" ") && !strings.HasSuffix(line, suffix) {
				t.Fatalf("list line %q for %s does not end with %q", line, name, suffix)
			}
		}
		if strings.Contains(line, "• delta ") && strings.Contains(line, "PR #") {
			t.Fatalf("list line %q shows a pull request for a session without one", line)
		}
	}
	if len(lines) != 4 {
		t.Fatalf("list has %d lines, want 4: %q", len(lines), out)
	}
}

func TestSessionListJSONCarriesThePullRequestWithStableStates(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{
		{Name: "alpha", PullRequest: 42, PullRequestState: contracts.PullRequestOpen, PullRequestDraft: true, PullRequestURL: "https://github.com/acme/widget/pull/42"},
		{Name: "beta", PullRequest: 7},
		{Name: "gamma"},
		{Name: "delta", PullRequest: 5, PullRequestState: contracts.PullRequestClosed},
	}
	out, err := cli.Plain(h.sessionListRun(context.Background(), contracts.Input{JSON: true}))
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	alpha, ok := got[0]["pullRequest"].(map[string]any)
	if !ok {
		t.Fatalf("alpha has no pullRequest object: %v", got[0])
	}
	if alpha["number"] != float64(42) || alpha["state"] != "open" || alpha["draft"] != true || alpha["url"] != "https://github.com/acme/widget/pull/42" {
		t.Fatalf("alpha pullRequest = %v, want number 42, state open, draft, url", alpha)
	}
	beta, ok := got[1]["pullRequest"].(map[string]any)
	if !ok || beta["state"] != "unknown" {
		t.Fatalf("beta pullRequest = %v, want state unknown", got[1]["pullRequest"])
	}
	if _, present := got[2]["pullRequest"]; present {
		t.Fatalf("session without a pull request must omit pullRequest, got %v", got[2])
	}
	closed, ok := got[3]["pullRequest"].(map[string]any)
	if !ok || closed["state"] != "closed" {
		t.Fatalf("delta pullRequest = %v, want state closed", got[3]["pullRequest"])
	}
}
