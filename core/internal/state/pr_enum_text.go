package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var errUnknownMergeState = errors.New("unknown merge state")

var mergeStates = []contracts.MergeState{
	contracts.MergeStateUnknown,
	contracts.MergeStateClean,
	contracts.MergeStateConflicting,
	contracts.MergeStateBlocked,
	contracts.MergeStateBehind,
	contracts.MergeStateDraft,
}

func parseMergeState(text string) (contracts.MergeState, error) {
	for _, state := range mergeStates {
		if state.String() == text {
			return state, nil
		}
	}
	return contracts.MergeStateUnknown, errUnknownMergeState
}

type enumText interface {
	~int
	String() string
}

func encodeEnum[T enumText](value T, parse func(string) (T, error)) ([]byte, error) {
	name := value.String()
	if _, err := parse(name); err != nil {
		return nil, fmt.Errorf("state: %s has no stored name: %w", name, err)
	}
	return []byte(name), nil
}

func decodeEnum[T enumText](text []byte, parse func(string) (T, error)) T {
	value, err := parse(string(text))
	if err != nil {
		slog.Warn("state: pull request value not recognised, read as unknown", "value", string(text))
		var unknown T
		return unknown
	}
	return value
}

type persistedPullRequestState contracts.PullRequestState

func (s persistedPullRequestState) MarshalText() ([]byte, error) {
	return encodeEnum(contracts.PullRequestState(s), contracts.ParsePullRequestState)
}

func (s *persistedPullRequestState) UnmarshalText(text []byte) error {
	*s = persistedPullRequestState(decodeEnum(text, contracts.ParsePullRequestState))
	return nil
}

type persistedMergeState contracts.MergeState

func (s persistedMergeState) MarshalText() ([]byte, error) {
	return encodeEnum(contracts.MergeState(s), parseMergeState)
}

func (s *persistedMergeState) UnmarshalText(text []byte) error {
	*s = persistedMergeState(decodeEnum(text, parseMergeState))
	return nil
}

type persistedChecksState contracts.ChecksState

func (s persistedChecksState) MarshalText() ([]byte, error) {
	return encodeEnum(contracts.ChecksState(s), contracts.ParseChecksState)
}

func (s *persistedChecksState) UnmarshalText(text []byte) error {
	*s = persistedChecksState(decodeEnum(text, contracts.ParseChecksState))
	return nil
}

func (s Session) MarshalJSON() ([]byte, error) {
	type plain Session
	return json.Marshal(struct {
		plain
		PullRequestState  persistedPullRequestState `json:"pullRequestState,omitempty"`
		PullRequestMerge  persistedMergeState       `json:"pullRequestMerge,omitempty"`
		PullRequestChecks persistedChecksState      `json:"pullRequestChecks,omitempty"`
	}{
		plain:             plain(s),
		PullRequestState:  persistedPullRequestState(s.PullRequestState),
		PullRequestMerge:  persistedMergeState(s.PullRequestMerge),
		PullRequestChecks: persistedChecksState(s.PullRequestChecks),
	})
}

func (s *Session) UnmarshalJSON(data []byte) error {
	type plain Session
	decoded := struct {
		*plain
		PullRequestState  persistedPullRequestState `json:"pullRequestState,omitempty"`
		PullRequestMerge  persistedMergeState       `json:"pullRequestMerge,omitempty"`
		PullRequestChecks persistedChecksState      `json:"pullRequestChecks,omitempty"`
	}{
		plain:             (*plain)(s),
		PullRequestState:  persistedPullRequestState(s.PullRequestState),
		PullRequestMerge:  persistedMergeState(s.PullRequestMerge),
		PullRequestChecks: persistedChecksState(s.PullRequestChecks),
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	s.PullRequestState = contracts.PullRequestState(decoded.PullRequestState)
	s.PullRequestMerge = contracts.MergeState(decoded.PullRequestMerge)
	s.PullRequestChecks = contracts.ChecksState(decoded.PullRequestChecks)
	return nil
}
