package manager

import (
	"errors"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

var (
	// ErrCloneUnsupported is returned when a clone is requested on a host that cannot clone.
	ErrCloneUnsupported = errors.New("clone is not supported on host")
	// ErrConflictingChannel is returned when channel_id and under are both given.
	ErrConflictingChannel = errors.New("pass either channel_id or under, not both: one adopts a conversation that exists, the other creates one")
	// ErrConflictingPlacement is returned when terminal_only and under are both given.
	ErrConflictingPlacement = errors.New("terminal_only and under contradict each other: one keeps the session in the terminal, the other puts it in a gateway container")
	// ErrExplicitCmdRefused is returned when a session names a cmd under the gateway-only route policy.
	ErrExplicitCmdRefused = &sentinelError{text: "an explicit cmd is refused under the gateway-only route policy: it bypasses the model catalog; select a model instead", code: contracts.CodeExplicitCmdRefused}
	// ErrMissingChannel is returned when a command omits the channel it targets.
	ErrMissingChannel = errors.New("missing channel")
	// ErrMissingName is returned when a command omits the session name.
	ErrMissingName = errors.New("missing name")
	// ErrMissingPath is returned when a command omits the path it operates on.
	ErrMissingPath = errors.New("missing path")
	// ErrMissingSoul is returned when an agent is created without a soul.
	ErrMissingSoul = errors.New("missing soul")
	// ErrMissingTo is returned when a rename omits the new name.
	ErrMissingTo = errors.New("missing to")
	// ErrModelRequired is returned when a session under the gateway-only route policy names no model.
	ErrModelRequired = &sentinelError{text: "a model is required under the gateway-only route policy: without one the session would spawn on this machine's own vendor login", code: contracts.CodeModelRequired}
	// ErrNoHome is returned when a session is created before a home channel is set.
	ErrNoHome = &sentinelError{text: "no home set; run `set home` first", code: contracts.CodeNoHome}
	// ErrNoHostRegistered is returned when a remote host is requested but none is registered.
	ErrNoHostRegistered = &sentinelError{text: "no host is registered", code: contracts.CodeNoHostRegistered}
	// ErrServiceControlUnavailable is returned when the host cannot control its own service.
	ErrServiceControlUnavailable = &sentinelError{text: "service control unavailable", code: contracts.CodeServiceControlUnavailable}
	// ErrSessionLimit is returned when creating a session would exceed the maximum number of sessions.
	ErrSessionLimit = errors.New("session limit reached")
)

// ChannelKindError reports a channel whose kind does not fit the requested placement.
type ChannelKindError struct {
	Channel string
	Kind    string
	Need    string
	Home    bool
}

func (e *ChannelKindError) Error() string {
	return fmt.Sprintf("channel %s is %q: %s", e.Channel, e.Kind, e.Need)
}

// Notice returns the notice that renders ChannelKindError in the caller's language.
func (e *ChannelKindError) Notice() contracts.Notice {
	code := contracts.CodeChannelKindUnder
	if e.Home {
		code = contracts.CodeChannelKindHome
	}
	return contracts.Notice{Code: code, Args: []contracts.Arg{contracts.Ref("Channel", e.Channel), contracts.Text("Kind", e.Kind)}}
}

// InvalidValueError reports a command argument whose value is malformed or out of range.
type InvalidValueError struct {
	Field string
	Value string
	Hint  string
}

func (e *InvalidValueError) Error() string {
	if e.Hint == "" {
		return fmt.Sprintf("invalid %s %q", e.Field, e.Value)
	}
	return fmt.Sprintf("invalid %s %q: %s", e.Field, e.Value, e.Hint)
}

// Notice returns the notice that renders InvalidValueError in the caller's language.
func (e *InvalidValueError) Notice() contracts.Notice {
	args := []contracts.Arg{contracts.Text("Field", e.Field), contracts.Text("Value", e.Value)}
	if e.Hint == "" {
		return contracts.Notice{Code: contracts.CodeInvalidValue, Args: args}
	}
	return contracts.Notice{Code: contracts.CodeInvalidValueHint, Args: append(args, contracts.Text("Hint", e.Hint))}
}

// SessionNotFoundError reports a reference to a session that does not exist.
type SessionNotFoundError struct {
	Name string
}

func (e *SessionNotFoundError) Error() string {
	return fmt.Sprintf("session %q not found", e.Name)
}

// SessionExistsError reports an attempt to create a session whose name is already taken.
type SessionExistsError struct {
	Name string
}

func (e *SessionExistsError) Error() string {
	return fmt.Sprintf("session %q already exists", e.Name)
}

// SharedAgentError reports an agent requested for a session that runs in the main checkout.
type SharedAgentError struct {
	Agent string
}

func (e *SharedAgentError) Error() string {
	return fmt.Sprintf("session create with agent %q cannot be shared: an agent needs a place of its own, and shared:true runs in the main checkout", e.Agent)
}

// Notice returns the notice that renders SharedAgentError in the caller's language.
func (e *SharedAgentError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeSharedAgent, Args: []contracts.Arg{contracts.Text("Agent", e.Agent)}}
}

// UnknownAgentError reports a reference to an agent that is not registered.
type UnknownAgentError struct {
	Name string
}

func (e *UnknownAgentError) Error() string {
	return fmt.Sprintf("unknown agent %q: create it with `agent create %s`", e.Name, e.Name)
}

// UnknownApprovalModeError reports an approval mode outside ask, bypass and strict.
type UnknownApprovalModeError struct {
	Mode string
}

func (e *UnknownApprovalModeError) Error() string {
	return fmt.Sprintf("unknown approval mode %q: ask, accept-edits, plan, auto, bypass or strict", e.Mode)
}

// Notice returns the notice that renders UnknownApprovalModeError in the caller's language.
func (e *UnknownApprovalModeError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeUnknownApprovalMode, Args: []contracts.Arg{contracts.Text("Mode", e.Mode)}}
}

// UnsupportedHomeTypeError reports a home channel kind the manager cannot open sessions in.
type UnsupportedHomeTypeError struct {
	Type string
}

func (e *UnsupportedHomeTypeError) Error() string {
	return fmt.Sprintf("home type %q unsupported", e.Type)
}

// Notice returns the notice that renders UnsupportedHomeTypeError in the caller's language.
func (e *UnsupportedHomeTypeError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeUnsupportedHomeType, Args: []contracts.Arg{contracts.Text("Type", e.Type)}}
}

// SessionLimitError reports that creating a session would exceed the maximum number of sessions.
type SessionLimitError struct {
	Max int
}

func (e *SessionLimitError) Error() string {
	return fmt.Sprintf("%v (%d): close a session before creating another", ErrSessionLimit, e.Max)
}

func (e *SessionLimitError) Unwrap() error {
	return ErrSessionLimit
}

// Notice returns the notice that renders SessionLimitError in the caller's language.
func (e *SessionLimitError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeSessionLimit, Args: []contracts.Arg{contracts.Count(e.Max)}}
}

type localizedError struct {
	english string
	text    string
	causes  []error
}

func localize(lang i18n.Language, english string, key clitext.Key, params clitext.Params, causes ...error) error {
	text, err := clitext.Text(lang, key, params)
	if err != nil {
		text = english
	}
	return &localizedError{english: english, text: text, causes: causes}
}

func restartLostError(lang i18n.Language, name string, restartErr, rollbackErr error) error {
	english := fmt.Sprintf("backend restart: %v; rollback failed, session %s out of service: %v", restartErr, name, rollbackErr)
	params := clitext.Params{clitext.ParamName: name, clitext.ParamErr: restartErr.Error(), clitext.ParamRollbackErr: rollbackErr.Error()}
	return localize(lang, english, clitext.SessionSwitchRestartLost, params, restartErr, rollbackErr)
}

func newDirtyWorktreeError(name string, cause error) error {
	return &dirtyWorktreeError{name: name, cause: cause}
}

type dirtyWorktreeError struct {
	name  string
	cause error
}

func (e *dirtyWorktreeError) Error() string {
	return fmt.Sprintf("%v; commit, or close with force:true to discard (branch session/%s is kept)", e.cause, e.name)
}

func (e *dirtyWorktreeError) Unwrap() error {
	return e.cause
}

func (e *dirtyWorktreeError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeSessionCloseDirty, Args: []contracts.Arg{contracts.Text("Name", e.name)}}
}

func (e *localizedError) Error() string {
	return e.english
}

func (e *localizedError) Unwrap() []error {
	return e.causes
}

func (e *localizedError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeUnexpected, Args: []contracts.Arg{contracts.Text("Detail", e.text)}}
}

type sentinelError struct {
	text string
	code contracts.Code
}

func (e *sentinelError) Error() string {
	return e.text
}

func (e *sentinelError) Notice() contracts.Notice {
	return contracts.Notice{Code: e.code}
}

// Notice returns the shared notice naming the missing session.
func (e *SessionNotFoundError) Notice() contracts.Notice {
	return sessionNotice(contracts.CodeSessionNotFound, e.Name)
}

// Notice returns the shared notice naming the existing session.
func (e *SessionExistsError) Notice() contracts.Notice {
	return sessionNotice(contracts.CodeSessionExists, e.Name)
}

// Notice returns the shared notice naming the unknown agent.
func (e *UnknownAgentError) Notice() contracts.Notice {
	return sessionNotice(contracts.CodeUnknownAgent, e.Name)
}

func sessionNotice(code contracts.Code, name string) contracts.Notice {
	return contracts.Notice{Code: code, Args: []contracts.Arg{contracts.Text("Name", name)}}
}
