package host

import (
	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func (p sessionPRs) stateWord(language i18n.Language, state contracts.PullRequestState) (string, error) {
	switch state {
	case contracts.PullRequestOpen:
		return p.messages.Text(language, clitext.SessionPRStateOpen, nil)
	case contracts.PullRequestClosed:
		return p.messages.Text(language, clitext.SessionPRStateClosed, nil)
	case contracts.PullRequestMerged:
		return p.messages.Text(language, clitext.SessionPRStateMerged, nil)
	}
	return p.unknownWord(language, state.String())
}

func (p sessionPRs) mergeWord(language i18n.Language, merge contracts.MergeState) (string, error) {
	switch merge {
	case contracts.MergeStateUnknown:
		return p.messages.Text(language, clitext.SessionPRMergeUnknown, nil)
	case contracts.MergeStateClean:
		return p.messages.Text(language, clitext.SessionPRMergeClean, nil)
	case contracts.MergeStateConflicting:
		return p.messages.Text(language, clitext.SessionPRMergeConflicting, nil)
	case contracts.MergeStateBlocked:
		return p.messages.Text(language, clitext.SessionPRMergeBlocked, nil)
	case contracts.MergeStateBehind:
		return p.messages.Text(language, clitext.SessionPRMergeBehind, nil)
	case contracts.MergeStateDraft:
		return p.messages.Text(language, clitext.SessionPRMergeDraft, nil)
	}
	return p.unknownWord(language, merge.String())
}

func (p sessionPRs) checkWord(language i18n.Language, check contracts.Check) (string, error) {
	if conclusion, done := check.Concluded(); done {
		return p.conclusionWord(language, conclusion)
	}
	return p.checkStatusWord(language, check.Status)
}

func (p sessionPRs) checkStatusWord(language i18n.Language, status contracts.CheckStatus) (string, error) {
	switch status {
	case contracts.CheckQueued:
		return p.messages.Text(language, clitext.SessionPRCheckQueued, nil)
	case contracts.CheckInProgress:
		return p.messages.Text(language, clitext.SessionPRCheckInProgress, nil)
	case contracts.CheckCompleted:
		return p.messages.Text(language, clitext.SessionPRCheckCompleted, nil)
	}
	return p.unknownWord(language, status.String())
}

func (p sessionPRs) conclusionWord(language i18n.Language, conclusion contracts.CheckConclusion) (string, error) {
	switch conclusion {
	case contracts.ConclusionNone:
		return p.messages.Text(language, clitext.SessionPRConclusionNone, nil)
	case contracts.ConclusionSuccess:
		return p.messages.Text(language, clitext.SessionPRConclusionSuccess, nil)
	case contracts.ConclusionFailure:
		return p.messages.Text(language, clitext.SessionPRConclusionFailure, nil)
	case contracts.ConclusionNeutral:
		return p.messages.Text(language, clitext.SessionPRConclusionNeutral, nil)
	case contracts.ConclusionCancelled:
		return p.messages.Text(language, clitext.SessionPRConclusionCancel, nil)
	case contracts.ConclusionSkipped:
		return p.messages.Text(language, clitext.SessionPRConclusionSkipped, nil)
	case contracts.ConclusionTimedOut:
		return p.messages.Text(language, clitext.SessionPRConclusionTimeout, nil)
	case contracts.ConclusionActionRequired:
		return p.messages.Text(language, clitext.SessionPRConclusionAction, nil)
	case contracts.ConclusionOther:
		return p.messages.Text(language, clitext.SessionPRConclusionOther, nil)
	}
	return p.unknownWord(language, conclusion.String())
}

func (p sessionPRs) unknownWord(language i18n.Language, value string) (string, error) {
	return p.messages.Text(language, clitext.SessionPRUnknownValue, clitext.Params{clitext.ParamValue: value})
}
