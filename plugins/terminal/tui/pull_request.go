package tui

import (
	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func (m *model) pullRequestText(pullRequest contracts.SessionPullRequest) string {
	if pullRequest == (contracts.SessionPullRequest{}) {
		return ""
	}
	state := clitext.PullRequestStateKey(pullRequest.State, pullRequest.Draft)
	return m.tr.t("tui.pr.label", map[string]any{"Number": pullRequest.Number, "State": m.tr.shared(state)})
}
