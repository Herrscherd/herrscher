package state

import contracts "github.com/Herrscherd/herrscher-contracts"

// PullRequestRecord is the pull request of a session as the host last read it from the forge.
// Number zero means the session has none. State is zero while it is not known yet, which is
// the case of a session recorded before its state was stored.
type PullRequestRecord struct {
	Number       int
	State        contracts.PullRequestState
	Draft        bool
	URL          string
	Revision     contracts.Revision
	HeadSHA      string
	Merge        contracts.MergeState
	Checks       contracts.ChecksState
	LatestReview contracts.ReviewID
}

// NewPullRequestRecord returns the record of pull request number as the forge last reported it.
func NewPullRequestRecord(number int, pull contracts.PullRequest) PullRequestRecord {
	return PullRequestRecord{
		Number:       number,
		State:        pull.State,
		Draft:        pull.Draft,
		URL:          pull.URL,
		Revision:     pull.Revision,
		HeadSHA:      pull.HeadSHA.String(),
		Merge:        pull.Merge,
		Checks:       pull.Checks,
		LatestReview: pull.LatestReview,
	}
}

// Known reports whether the record names a pull request with a positive number and a state
// the forge reports. Only a known record can be shown as a [contracts.SessionPullRequest].
func (r PullRequestRecord) Known() bool {
	if r.Number <= 0 {
		return false
	}
	switch r.State {
	case contracts.PullRequestOpen, contracts.PullRequestClosed, contracts.PullRequestMerged:
		return true
	}
	return false
}

// PullRequestRecord returns the pull request recorded on the session, with a zero Number when
// it has none.
func (s Session) PullRequestRecord() PullRequestRecord {
	return PullRequestRecord{
		Number:       s.PullRequest,
		State:        s.PullRequestState,
		Draft:        s.PullRequestDraft,
		URL:          s.PullRequestURL,
		Revision:     s.PullRequestRevision,
		HeadSHA:      s.PullRequestHeadSHA,
		Merge:        s.PullRequestMerge,
		Checks:       s.PullRequestChecks,
		LatestReview: s.PullRequestLatestReview,
	}
}

func setSessionPullRequest(session *Session, record PullRequestRecord) {
	session.PullRequest = record.Number
	session.PullRequestState = record.State
	session.PullRequestDraft = record.Draft
	session.PullRequestURL = record.URL
	session.PullRequestRevision = record.Revision
	session.PullRequestHeadSHA = record.HeadSHA
	session.PullRequestMerge = record.Merge
	session.PullRequestChecks = record.Checks
	session.PullRequestLatestReview = record.LatestReview
}
