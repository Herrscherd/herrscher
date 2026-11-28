package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// DefaultIssuePeriod is how often a repository with issue triggers is read when its period is unset.
const DefaultIssuePeriod = 10 * time.Minute

const minIssuePeriod = time.Minute

var (
	// ErrIssueTriggerRepo reports a repository key that is not host/namespace/name.
	ErrIssueTriggerRepo = errors.New("issue trigger repository is not host/namespace/name")
	// ErrIssueTriggerEmpty reports a trigger that lists neither a label nor a mention.
	ErrIssueTriggerEmpty = errors.New("issue trigger lists no label and no mention")
	// ErrIssueTriggerLabel reports a label that is empty once trimmed.
	ErrIssueTriggerLabel = errors.New("issue trigger label is empty")
	// ErrIssueTriggerMention reports a mention that is not a forge login.
	ErrIssueTriggerMention = errors.New("issue trigger mention is not a forge login")
	// ErrIssueTriggerPeriod reports a period that is not a duration of at least one minute.
	ErrIssueTriggerPeriod = errors.New("issue trigger period is not a duration of at least one minute")
)

// IssueTriggerConfig opens a session for each issue of one repository that carries
// one of Labels or that mentions one of Mentions. Period is how often the repository
// is read, DefaultIssuePeriod when empty. Agent is the agent the sessions run as.
type IssueTriggerConfig struct {
	Labels   []string `json:"labels,omitempty"`
	Mentions []string `json:"mentions,omitempty"`
	Period   string   `json:"period,omitempty"`
	Agent    string   `json:"agent,omitempty"`
}

// PollPeriod returns how often the repository is read.
func (c IssueTriggerConfig) PollPeriod() (time.Duration, error) {
	if strings.TrimSpace(c.Period) == "" {
		return DefaultIssuePeriod, nil
	}
	period, err := time.ParseDuration(strings.TrimSpace(c.Period))
	if err != nil || period < minIssuePeriod {
		return 0, ErrIssueTriggerPeriod
	}
	return period, nil
}

func validateIssueTriggers(triggers map[string]IssueTriggerConfig) error {
	for repo, trigger := range triggers {
		if _, err := contracts.ParseRepoRef(repo); err != nil {
			return fmt.Errorf("%w: %q", ErrIssueTriggerRepo, repo)
		}
		if len(trigger.Labels) == 0 && len(trigger.Mentions) == 0 {
			return fmt.Errorf("%w: %q", ErrIssueTriggerEmpty, repo)
		}
		for _, label := range trigger.Labels {
			if strings.TrimSpace(label) == "" {
				return fmt.Errorf("%w: %q", ErrIssueTriggerLabel, repo)
			}
		}
		for _, mention := range trigger.Mentions {
			if _, err := contracts.ParseForgeLogin(strings.TrimSpace(mention)); err != nil {
				return fmt.Errorf("%w: %q", ErrIssueTriggerMention, repo)
			}
		}
		if _, err := trigger.PollPeriod(); err != nil {
			return fmt.Errorf("%w: %q", err, repo)
		}
	}
	return nil
}
