package tui

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestPullRequestTextNamesTheNumberAndState(t *testing.T) {
	m := newTestModel()
	cases := []struct {
		name string
		pr   contracts.SessionPullRequest
		want string
	}{
		{"open", contracts.SessionPullRequest{Number: 42, State: contracts.PullRequestOpen}, "#42 open"},
		{"draft", contracts.SessionPullRequest{Number: 42, State: contracts.PullRequestOpen, Draft: true}, "#42 draft"},
		{"merged", contracts.SessionPullRequest{Number: 7, State: contracts.PullRequestMerged}, "#7 merged"},
		{"closed", contracts.SessionPullRequest{Number: 3, State: contracts.PullRequestClosed}, "#3 closed"},
		{"absent", contracts.SessionPullRequest{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.pullRequestText(tc.pr); got != tc.want {
				t.Fatalf("pullRequestText(%+v) = %q, want %q", tc.pr, got, tc.want)
			}
		})
	}
}

func TestPickerRowShowsThePullRequest(t *testing.T) {
	m := sizedTestModel(t, 100)
	m.resumeRows = []contracts.SessionInfo{{Name: "alpha", PullRequest: contracts.SessionPullRequest{Number: 42, State: contracts.PullRequestOpen, Draft: true}}}

	if out := m.resumeView(); !strings.Contains(out, "#42 draft") {
		t.Fatalf("picker row %q does not show the pull request #42 draft", out)
	}
}

func TestSidebarSessionShowsThePullRequest(t *testing.T) {
	m := sizedTestModel(t, 100)
	info := contracts.SessionInfo{Name: "alpha", PullRequest: contracts.SessionPullRequest{Number: 42, State: contracts.PullRequestMerged}}

	out := strings.Join(m.sessionSection(&tab{label: "alpha"}, info, "", 60), "\n")
	if !strings.Contains(out, "#42 merged") {
		t.Fatalf("sidebar %q does not show the pull request #42 merged", out)
	}
}
