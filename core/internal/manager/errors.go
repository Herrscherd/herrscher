package manager

import (
	"errors"
	"fmt"
)

var (
	// ErrCloneUnsupported is returned when a clone is requested on a host that cannot clone.
	ErrCloneUnsupported = errors.New("clone is not supported on host")
	// ErrConflictingChannel is returned when channel_id and under are both given.
	ErrConflictingChannel = errors.New("pass either channel_id or under, not both: one adopts a conversation that exists, the other creates one")
	// ErrConflictingPlacement is returned when terminal_only and under are both given.
	ErrConflictingPlacement = errors.New("terminal_only and under contradict each other: one keeps the session in the terminal, the other puts it in a gateway container")
	// ErrExplicitCmdRefused is returned when a session names a cmd under the gateway-only route policy.
	ErrExplicitCmdRefused = errors.New("an explicit cmd is refused under the gateway-only route policy: it bypasses the model catalog; select a model instead")
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
	ErrModelRequired = errors.New("a model is required under the gateway-only route policy: without one the session would spawn on this machine's own vendor login")
	// ErrNoHome is returned when a session is created before a home channel is set.
	ErrNoHome = errors.New("no home set; run `set home` first")
	// ErrNoHostRegistered is returned when a remote host is requested but none is registered.
	ErrNoHostRegistered = errors.New("no host is registered")
	// ErrServiceControlUnavailable is returned when the host cannot control its own service.
	ErrServiceControlUnavailable = errors.New("service control unavailable")
	// ErrSessionLimit is returned when creating a session would exceed the maximum number of sessions.
	ErrSessionLimit = errors.New("session limit reached")
)

// ChannelKindError reports a channel whose kind does not fit the requested placement.
type ChannelKindError struct {
	Channel string
	Kind    string
	Need    string
}

func (e *ChannelKindError) Error() string {
	return fmt.Sprintf("channel %s is %q: %s", e.Channel, e.Kind, e.Need)
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
	return fmt.Sprintf("unknown approval mode %q: ask, bypass or strict", e.Mode)
}

// UnsupportedHomeTypeError reports a home channel kind the manager cannot open sessions in.
type UnsupportedHomeTypeError struct {
	Type string
}

func (e *UnsupportedHomeTypeError) Error() string {
	return fmt.Sprintf("home type %q unsupported", e.Type)
}
