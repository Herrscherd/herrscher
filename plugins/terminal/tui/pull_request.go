package tui

import contracts "github.com/Herrscherd/herrscher-contracts"

func (m *model) pullRequestText(pullRequest contracts.SessionPullRequest) string {
	if pullRequest == (contracts.SessionPullRequest{}) {
		return ""
	}
	return m.tr.t("tui.pr.label", map[string]any{"Number": pullRequest.Number, "State": m.tr.t(pullRequestStateKey(pullRequest), nil)})
}

func pullRequestStateKey(pullRequest contracts.SessionPullRequest) string {
	if pullRequest.Draft && pullRequest.State == contracts.PullRequestOpen {
		return "tui.pr.state.draft"
	}
	switch pullRequest.State {
	case contracts.PullRequestOpen:
		return "tui.pr.state.open"
	case contracts.PullRequestClosed:
		return "tui.pr.state.closed"
	case contracts.PullRequestMerged:
		return "tui.pr.state.merged"
	}
	return "tui.pr.state.unknown"
}
