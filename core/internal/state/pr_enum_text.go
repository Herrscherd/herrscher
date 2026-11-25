package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var errUnknownMergeState = errors.New("unknown merge state")

func mergeStates() []contracts.MergeState {
	return []contracts.MergeState{
		contracts.MergeStateUnknown,
		contracts.MergeStateClean,
		contracts.MergeStateConflicting,
		contracts.MergeStateBlocked,
		contracts.MergeStateBehind,
		contracts.MergeStateDraft,
	}
}

func parseMergeState(text string) (contracts.MergeState, error) {
	for _, state := range mergeStates() {
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

func encodeEnum[Enum enumText](value Enum, parse func(string) (Enum, error)) ([]byte, error) {
	name := value.String()
	if _, err := parse(name); err != nil {
		return nil, fmt.Errorf("state: %s has no stored name: %w", name, err)
	}
	return []byte(name), nil
}

func decodeEnum[Enum enumText](text []byte, parse func(string) (Enum, error)) Enum {
	value, err := parse(string(text))
	if err != nil {
		slog.Warn("state: pull request value not recognised, read as unknown", "value", string(text))
		var unknown Enum
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

// MarshalJSON stores the pull request enums as text.
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

// UnmarshalJSON reads the pull request enums from text, unrecognised text giving the unknown value.
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
