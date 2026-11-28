package host

import (
	"maps"
	"slices"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/internal/issuetrigger"
	"github.com/Herrscherd/herrscher/core/internal/state"
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
	delimiters := strings.NewReplacer("<issue>", "&lt;issue&gt;", "</issue>", "&lt;/issue&gt;")
	return func(_ contracts.ForgeRepoRef, issue contracts.Issue) string {
		return messages.text(lang(), "serve.issue_task", map[string]any{
			"Number": issue.Ref.Number(),
			"Title":  delimiters.Replace(escapeLine(issue.Title)),
			"URL":    escapeLine(issue.URL),
			"Body":   delimiters.Replace(escapeLines(issue.Body)),
		})
	}
}

func newIssueScheduler(messages *hostMessages, options Options, triggers map[string]config.IssueTriggerConfig, store *state.State, runningHub *hub, sessions *sessionTable, clock func() time.Time, tick time.Duration) (*scheduler, error) {
	targets, err := issueTriggerTargets(triggers)
	if err != nil {
		return nil, wrapHost(messages, err, "serve.issue_trigger", map[string]any{detailParam: err.Error()})
	}
	poller := issuetrigger.NewPoller(store, GitHubOpener(options.Forge), runningHub, sessions.Seed, issueTriggerTask(messages, options.languageSource()), clock)
	return newScheduler(store, store, runningHub, sessions.Seed, clock, tick).withIssues(poller, targets), nil
}
