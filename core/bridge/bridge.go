// Package bridge implements the bridge as a pure backend runner: the daemon hub
// owns all gateway I/O and feeds inputs over a control socket, the bridge runs
// the injected backend per turn and emits events back. The loop is
// model-agnostic: it never knows which backend (Claude, …) responds.
package bridge

import (
	"context"
	"errors"
	"fmt"

	"github.com/Herrscherd/herrscher-contracts"
)

// BackendFactory builds the model-edge backend for a resolved channel. It is
// injected so core stays free of any model-specific code: the binary supplies a
// factory closing over its chosen backend (e.g. claude.NewBackend). The channel
// id is passed because a backend may key its session/process on it, and the
// channel can be created inside Run.
type BackendFactory func(channelID string) (contracts.Backend, error)

// Options configures one bridge run (parsed from CLI flags by the binary). In
// pure-runner mode the bridge only needs the channel to key its backend and the
// hub socket to dial; the progress level is decided host-side by the renderer.
type Options struct {
	Channel string
	// HubSocket selects pure-runner (hub) mode: the bridge dials this socket,
	// reads input/pick frames from the daemon hub, and emits turn events back.
	HubSocket string
}

// Run is the bridge entry point: a pure backend runner. It requires a hub socket
// (the daemon hub owns all gateway I/O) and drives the backend over it.
func Run(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, o Options) error {
	if o.HubSocket == "" {
		return errors.New("bridge requires --hub-socket (pure-runner mode)")
	}
	return runHub(ctx, newBackend, orch, o)
}

// RunOneShot runs one backend turn in-process over event channels. It is the
// bridge seam used by the operator's short-lived session seed path: no control
// socket or gateway is involved, but the same backend turn machinery (including
// orchestrator context/observation) is exercised.
func RunOneShot(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, channel string, in <-chan contracts.Event, out chan<- contracts.Event) error {
	resp, err := newBackend(channel)
	if err != nil {
		return fmt.Errorf("backend: %w", err)
	}
	defer resp.Close()

	select {
	case ev := <-in:
		runOneTurn(ctx, channelSink{ctx: ctx, out: out}, resp, orch, ev)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type channelSink struct {
	ctx context.Context
	out chan<- contracts.Event
}

func (s channelSink) Emit(e contracts.Event) {
	select {
	case s.out <- e:
	case <-s.ctx.Done():
	}
}
