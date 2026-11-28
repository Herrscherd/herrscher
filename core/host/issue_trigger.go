package host

import (
	"maps"
	"slices"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/internal/issuetrigger"
)

func issueTriggerTargets(triggers map[string]config.IssueTriggerConfig) ([]issuetrigger.Target, error) {
	targets := make([]issuetrigger.Target, 0, len(triggers))
	for _, key := range slices.Sorted(maps.Keys(triggers)) {
		trigger := triggers[key]
		repo, err := contracts.ParseRepoRef(key)
		if err != nil {
			return nil, err
		}
		period, err := trigger.PollPeriod()
		if err != nil {
			return nil, err
		}
		filters, err := issueTriggerFilters(trigger)
		if err != nil {
			return nil, err
		}
		targets = append(targets, issuetrigger.Target{Repo: repo, Agent: trigger.Agent, Period: period, Filters: filters})
	}
	return targets, nil
}

func issueTriggerFilters(trigger config.IssueTriggerConfig) ([]issuetrigger.Filter, error) {
	var filters []issuetrigger.Filter
	for _, label := range trigger.Labels {
		filters = append(filters, issuetrigger.LabelFilter(strings.TrimSpace(label)))
	}
	for _, mention := range trigger.Mentions {
		login, err := contracts.ParseForgeLogin(strings.TrimSpace(mention))
		if err != nil {
			return nil, err
		}
		filters = append(filters, issuetrigger.MentionFilter(login))
	}
	return filters, nil
}

func issueTriggerTask(messages *hostMessages, lang languageSource) func(contracts.ForgeRepoRef, contracts.Issue) string {
	return func(_ contracts.ForgeRepoRef, issue contracts.Issue) string {
		return messages.text(lang(), "serve.issue_task", map[string]any{
			"Number": issue.Ref.Number(),
			"Title":  escapeLine(issue.Title),
			"URL":    escapeLine(issue.URL),
			"Body":   escapeLines(issue.Body),
		})
	}
}
