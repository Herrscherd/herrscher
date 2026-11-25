package clitext

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestPullRequestStateKeyGivesEachStateItsOwnLabel(t *testing.T) {
	cases := []struct {
		name  string
		state contracts.PullRequestState
		draft bool
		key   Key
	}{
		{name: "open", state: contracts.PullRequestOpen, key: SessionPRStateOpen},
		{name: "open draft", state: contracts.PullRequestOpen, draft: true, key: SessionPRStateDraft},
		{name: "closed", state: contracts.PullRequestClosed, key: SessionPRStateClosed},
		{name: "merged", state: contracts.PullRequestMerged, key: SessionPRStateMerged},
		{name: "unknown", state: contracts.PullRequestState(99), key: SessionPRStateUnknown},
	}
	seen := make(map[Key]string)
	for _, tc := range cases {
		key := PullRequestStateKey(tc.state, tc.draft)
		if key != tc.key {
			t.Errorf("%s maps to %q, want %q", tc.name, key, tc.key)
		}
		if other, dup := seen[key]; dup {
			t.Errorf("%s and %s share the label key %q", tc.name, other, key)
		}
		seen[key] = tc.name
	}
}

func TestPullRequestStateIDNamesEachKnownStateAndMapsAnyOtherToUnknown(t *testing.T) {
	cases := []struct {
		name  string
		state contracts.PullRequestState
		want  string
	}{
		{name: "open", state: contracts.PullRequestOpen, want: "open"},
		{name: "closed", state: contracts.PullRequestClosed, want: "closed"},
		{name: "merged", state: contracts.PullRequestMerged, want: "merged"},
		{name: "unknown value", state: contracts.PullRequestState(99), want: "unknown"},
	}
	for _, tc := range cases {
		if got := PullRequestStateID(tc.state); got != tc.want {
			t.Errorf("%s JSON id = %q, want %q", tc.name, got, tc.want)
		}
	}
}
