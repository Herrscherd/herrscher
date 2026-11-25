package clitext

import contracts "github.com/Herrscherd/herrscher-contracts"

// PullRequestStateKey names how a pull request stands for the operator: an open draft reads as a draft, and a state no forge reports reads as unknown.
func PullRequestStateKey(state contracts.PullRequestState, draft bool) Key {
	if draft && state == contracts.PullRequestOpen {
		return SessionPRStateDraft
	}
	switch state {
	case contracts.PullRequestOpen:
		return SessionPRStateOpen
	case contracts.PullRequestClosed:
		return SessionPRStateClosed
	case contracts.PullRequestMerged:
		return SessionPRStateMerged
	}
	return SessionPRStateUnknown
}
