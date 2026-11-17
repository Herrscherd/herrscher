package vendortest

import (
	"errors"
	"testing"
)

func TestDecideReadsOnlyAnExplicitDecision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
		want   verdict
		err    error
	}{
		{"allow", `{"hookSpecificOutput":{"permissionDecision":"allow"}}`, verdict{allowed: true}, nil},
		{"deny keeps the reason", `{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"no"}}`, verdict{reason: "no"}, nil},
		{"no decision", `{}`, verdict{}, errNoDecision},
		{"not json", `nope`, verdict{}, errHookAnswer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decide([]byte(tc.answer))
			if !errors.Is(err, tc.err) || got != tc.want {
				t.Fatalf("got %+v and %v, want %+v and %v", got, err, tc.want, tc.err)
			}
		})
	}
}

func TestExpectationJudgesTheToolBeforeIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		decided *verdict
		want    string
		err     error
	}{
		{"no tool before it", nil, wordAllowed, errExpectNoTool},
		{"allowed as expected", &verdict{allowed: true}, wordAllowed, nil},
		{"denied as expected", &verdict{reason: "no"}, wordDenied, nil},
		{"allowed where denied was expected", &verdict{allowed: true}, wordDenied, errExpectedDeny},
		{"denied where allowed was expected", &verdict{reason: "no"}, wordAllowed, errExpectedAllow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := expectation(tc.decided, tc.want); !errors.Is(err, tc.err) {
				t.Fatalf("got %v, want %v", err, tc.err)
			}
		})
	}
}
