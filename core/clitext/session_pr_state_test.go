package clitext

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestPullRequestStateKeyIsDistinctPerState(t *testing.T) {
	cases := []struct {
		name   string
		state  contracts.PullRequestState
		draft  bool
		key    Key
		jsonID string
	}{
		{name: "open", state: contracts.PullRequestOpen, key: SessionPRStateOpen, jsonID: "open"},
		{name: "open draft", state: contracts.PullRequestOpen, draft: true, key: SessionPRStateDraft, jsonID: "open"},
		{name: "closed", state: contracts.PullRequestClosed, key: SessionPRStateClosed, jsonID: "closed"},
		{name: "merged", state: contracts.PullRequestMerged, key: SessionPRStateMerged, jsonID: "merged"},
		{name: "unknown", state: contracts.PullRequestState(99), key: SessionPRStateUnknown, jsonID: "PullRequestState(99)"},
	}
	keys := make(map[Key]string)
	ids := make(map[string]string)
	for _, tc := range cases {
		key := PullRequestStateKey(tc.state, tc.draft)
		if key != tc.key {
			t.Errorf("%s maps to %q, want %q", tc.name, key, tc.key)
		}
		if other, seen := keys[key]; seen {
			t.Errorf("%s and %s share the label key %q", tc.name, other, key)
		}
		keys[key] = tc.name
		if got := tc.state.String(); got != tc.jsonID {
			t.Errorf("%s JSON id = %q, want %q", tc.name, got, tc.jsonID)
		}
		if tc.draft {
			continue
		}
		if other, seen := ids[tc.jsonID]; seen {
			t.Errorf("%s and %s share the JSON id %q", tc.name, other, tc.jsonID)
		}
		ids[tc.jsonID] = tc.name
	}
}
