package ciloop

import (
	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/prwatch"
)

// MaxAttempts is the number of fix attempts allowed per pull request and head commit.
const MaxAttempts Attempts = 3

// LogTailBytes bounds the tail of a failing job log sent to a session.
const LogTailBytes int = 16 << 10

// Attempts counts the fix attempts already made on a pull request.
type Attempts int

// Action is the decision the CI loop takes for a pull request snapshot.
type Action int

// Decision values returned by Decide.
const (
	ActionWait Action = iota + 1
	ActionIdle
	ActionHuman
	ActionFix
	ActionStop
)

// Snapshot is the pull request state the CI loop decides on.
type Snapshot struct {
	State    contracts.PullRequestState
	Draft    bool
	Checks   contracts.ChecksState
	Attempts Attempts
	Reported bool
}

// Decide returns the CI loop action for a pull request snapshot.
func Decide(snapshot Snapshot) Action {
	if prwatch.Ended(snapshot.State) {
		return ActionIdle
	}
	if snapshot.Draft {
		return ActionHuman
	}
	switch snapshot.Checks {
	case contracts.ChecksFailed:
		return decideFailure(snapshot)
	case contracts.ChecksActionRequired:
		return ActionHuman
	case contracts.ChecksPassed:
		return ActionIdle
	case contracts.ChecksUnknown, contracts.ChecksNone, contracts.ChecksPending:
		return ActionWait
	}
	return ActionWait
}

func decideFailure(snapshot Snapshot) Action {
	if snapshot.Reported {
		return ActionWait
	}
	if snapshot.Attempts >= MaxAttempts {
		return ActionStop
	}
	return ActionFix
}
