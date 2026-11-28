//go:build windows

package host

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

// CommandSocketPath is the daemon-level operator command pipe.
func CommandSocketPath(instanceID string) string {
	return CommandSocketPathIn(RuntimeLayout{}, instanceID)
}

// CommandSocketPathIn is the operator command pipe; pipes have no directory, so
// the layout is not used on this platform.
func CommandSocketPathIn(_ RuntimeLayout, instanceID string) string {
	return `\\.\pipe\herrscher-command-` + instanceID
}

// SessionCommandSocketPathIn is the per-session command pipe; the layout is not
// used on this platform.
func SessionCommandSocketPathIn(_ RuntimeLayout, instanceID, session string) string {
	return `\\.\pipe\herrscher-command-` + instanceID + "-s-" + control.SafeSessionName(session)
}

// EventsSocketPath is the daemon-level per-session events fan-out pipe: a sibling
// of the command pipe (herrscher-command → herrscher-events). instanceID is the
// one [ServedInstanceID] reads; an empty one names no daemon, so the path is
// empty.
func EventsSocketPath(instanceID string) string {
	return servedSocket(RuntimeLayout{}, instanceID, EventsSocketPathIn)
}

// EventsSocketPathIn is the per-session events pipe; the layout is not used on
// this platform.
func EventsSocketPathIn(_ RuntimeLayout, instanceID string) string {
	return `\\.\pipe\herrscher-events-` + instanceID
}

func listenCommandSocket(_ context.Context, _ *hostMessages, _ RuntimeLayout, path string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
	})
}

func removeSocketFiles(_ RuntimeLayout, _ string, _ []string) error {
	return nil
}

func dialCommandSocket(ctx context.Context, _ *hostMessages, _ RuntimeLayout, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}
