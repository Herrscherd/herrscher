// Package bridge implements the bridge as a pure backend runner: the daemon hub
// owns all gateway I/O and feeds inputs over a control socket, the bridge runs
// the injected backend per turn and emits events back. The loop is
// model-agnostic: it never knows which backend (Claude, …) responds.
package bridge

import (
	"context"
	"errors"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var errNoHubSocket = errors.New("bridge requires --hub-socket (pure-runner mode)")

// BackendFactory builds the model-edge backend for a resolved channel. It is
// injected so core stays free of any model-specific code: the binary supplies a
// factory closing over its chosen backend (e.g. claude.NewBackend). The channel
// id is passed because a backend may key its session/process on it, and the
// channel can be created inside Run.
type BackendFactory func(channelID string) (contracts.Backend, error)

// ScopeResolver answers which memory project a prompt is about, having ensured
// that project's root exists. "" means "none of the ones I know", which the
// caller reads as "keep the scope you launched with".
type ScopeResolver interface {
	Resolve(ctx context.Context, prompt string) string
}

// Options configures one bridge run (parsed from CLI flags by the binary). In
// pure-runner mode the bridge only needs the channel to key its backend and the
// hub socket to dial; the progress level is decided host-side by the renderer.
type Options struct {
	Channel string
	// HubSocket selects pure-runner (hub) mode: the bridge dials this socket,
	// reads input/pick frames from the daemon hub, and emits turn events back.
	HubSocket string
	// Roster lists the agents this session may delegate to (optional). When nil or
	// empty, no delegation affordance is injected.
	Roster contracts.RosterProvider
	// Capabilities is the verb summary of the daemon supervising this bridge,
	// handed down at spawn (the bridge holds no registry of its own). Empty
	// injects no <capabilities> block.
	Capabilities string
	// Scope decides which memory project a session's conversation is actually
	// about, on its first prompt. It is injected because the answer lives in the
	// vault and the bridge package holds no memory of its own, which is also
	// what lets the turn driver be tested without one. Nil never settles
	// anything, and neither does a session whose project a human already chose.
	Scope ScopeResolver
	// LaunchProject is the memory project the session was created with, and
	// ProjectPinned says a human chose it rather than the host guessing from a
	// directory. Only a guess is ever revised.
	LaunchProject string
	ProjectPinned bool
	// MemoryAgent is the private memory root, carried so a re-rooted scope keeps
	// both halves of the tree instead of dropping the agent on the way.
	MemoryAgent string
}

// Run is the bridge entry point: a pure backend runner. It requires a hub socket
// (the daemon hub owns all gateway I/O) and drives the backend over it.
func Run(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, opts Options) error {
	if opts.HubSocket == "" {
		return errNoHubSocket
	}
	if starter, ok := orch.(interface{ Start(context.Context) }); ok {
		starter.Start(ctx)
	}
	return runHub(ctx, newBackend, orch, opts)
}

// RunOneShot runs one backend turn in-process over event channels. It is the
// bridge seam used by the operator's short-lived session seed path: no control
// socket or gateway is involved, but the same backend turn machinery (including
// orchestrator context/observation) is exercised.
func RunOneShot(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, channel string, input <-chan contracts.Event, out chan<- contracts.Event) error {
	resp, err := newBackend(channel)
	if err != nil {
		return fmt.Errorf("backend: %w", err)
	}
	defer closeAndWarn(resp)

	select {
	case ev := <-input:
		runOneTurn(ctx, channelSink{done: ctx.Done(), out: out}, resp, orch, ev, nil, newSkillEngine(resp), affordances{}, nil)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type channelSink struct {
	done <-chan struct{}
	out  chan<- contracts.Event
}

func (s channelSink) Emit(event contracts.Event) {
	select {
	case s.out <- event:
	case <-s.done:
	}
}
