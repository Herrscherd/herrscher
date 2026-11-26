package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var errUnknownMergeState = errors.New("unknown merge state")

func knownPullRequestState(state contracts.PullRequestState) bool {
	switch state {
	case contracts.PullRequestOpen, contracts.PullRequestClosed, contracts.PullRequestMerged:
		return true
	}
	return false
}

func knownMergeState(state contracts.MergeState) bool {
	switch state {
	case contracts.MergeStateUnknown, contracts.MergeStateClean, contracts.MergeStateConflicting,
		contracts.MergeStateBlocked, contracts.MergeStateBehind, contracts.MergeStateDraft:
		return true
	}
	return false
}

func knownChecksState(state contracts.ChecksState) bool {
	switch state {
	case contracts.ChecksUnknown, contracts.ChecksNone, contracts.ChecksPending,
		contracts.ChecksPassed, contracts.ChecksFailed, contracts.ChecksActionRequired:
		return true
	}
	return false
}

func parseMergeState(text string) (contracts.MergeState, error) {
	for state := contracts.MergeStateUnknown; knownMergeState(state); state++ {
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

func decodeEnum[Enum enumText](data []byte, parseText func(string) (Enum, error), known func(Enum) bool) Enum {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		value, parseErr := parseText(text)
		if parseErr != nil {
			return unrecognisedEnum[Enum](text)
		}
		return value
	}
	var number int
	if err := json.Unmarshal(data, &number); err == nil && known(Enum(number)) {
		return Enum(number)
	}
	return unrecognisedEnum[Enum](string(data))
}

func unrecognisedEnum[Enum enumText](value string) Enum {
	slog.Warn("state: pull request value not recognised, read as unknown", "value", value)
	var unknown Enum
	return unknown
}

type persistedPullRequestState contracts.PullRequestState

func (s persistedPullRequestState) MarshalText() ([]byte, error) {
	return encodeEnum(contracts.PullRequestState(s), contracts.ParsePullRequestState)
}

func (s *persistedPullRequestState) UnmarshalJSON(data []byte) error {
	*s = persistedPullRequestState(decodeEnum(data, contracts.ParsePullRequestState, knownPullRequestState))
	return nil
}

type persistedMergeState contracts.MergeState

func (s persistedMergeState) MarshalText() ([]byte, error) {
	return encodeEnum(contracts.MergeState(s), parseMergeState)
}

func (s *persistedMergeState) UnmarshalJSON(data []byte) error {
	*s = persistedMergeState(decodeEnum(data, parseMergeState, knownMergeState))
	return nil
}

type persistedChecksState contracts.ChecksState

func (s persistedChecksState) MarshalText() ([]byte, error) {
	return encodeEnum(contracts.ChecksState(s), contracts.ParseChecksState)
}

func (s *persistedChecksState) UnmarshalJSON(data []byte) error {
	*s = persistedChecksState(decodeEnum(data, contracts.ParseChecksState, knownChecksState))
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

// UnmarshalJSON reads the pull request enums from text or from the integers older builds wrote,
// unrecognised values giving the unknown value.
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
