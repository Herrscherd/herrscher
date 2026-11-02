package manager

import (
	"errors"
	"fmt"
)

var (
	ErrCloneUnsupported          = errors.New("clone is not supported on host")
	ErrConflictingChannel        = errors.New("pass either channel_id or under, not both: one adopts a conversation that exists, the other creates one")
	ErrConflictingPlacement      = errors.New("terminal_only and under contradict each other: one keeps the session in the terminal, the other puts it in a gateway container")
	ErrExplicitCmdRefused        = errors.New("an explicit cmd is refused under the gateway-only route policy: it bypasses the model catalog; select a model instead")
	ErrMissingChannel            = errors.New("missing channel")
	ErrMissingName               = errors.New("missing name")
	ErrMissingPath               = errors.New("missing path")
	ErrMissingSoul               = errors.New("missing soul")
	ErrMissingTo                 = errors.New("missing to")
	ErrModelRequired             = errors.New("a model is required under the gateway-only route policy: without one the session would spawn on this machine's own vendor login")
	ErrNoHome                    = errors.New("no home set; run `set home` first")
	ErrNoHostRegistered          = errors.New("no host is registered")
	ErrServiceControlUnavailable = errors.New("service control unavailable")
	ErrSessionLimit              = errors.New("session limit reached")
)

type ChannelKindError struct {
	Channel string
	Kind    string
	Need    string
}

func (e *ChannelKindError) Error() string {
	return fmt.Sprintf("channel %s is %q: %s", e.Channel, e.Kind, e.Need)
}

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

type SessionNotFoundError struct {
	Name string
}

func (e *SessionNotFoundError) Error() string {
	return fmt.Sprintf("session %q not found", e.Name)
}

type SessionExistsError struct {
	Name string
}

func (e *SessionExistsError) Error() string {
	return fmt.Sprintf("session %q already exists", e.Name)
}

type SharedAgentError struct {
	Agent string
}

func (e *SharedAgentError) Error() string {
	return fmt.Sprintf("session create with agent %q cannot be shared: an agent needs a place of its own, and shared:true runs in the main checkout", e.Agent)
}

type UnknownAgentError struct {
	Name string
}

func (e *UnknownAgentError) Error() string {
	return fmt.Sprintf("unknown agent %q: create it with `agent create %s`", e.Name, e.Name)
}

type UnknownApprovalModeError struct {
	Mode string
}

func (e *UnknownApprovalModeError) Error() string {
	return fmt.Sprintf("unknown approval mode %q: ask, bypass or strict", e.Mode)
}

type UnsupportedHomeTypeError struct {
	Type string
}

func (e *UnsupportedHomeTypeError) Error() string {
	return fmt.Sprintf("home type %q unsupported", e.Type)
}
