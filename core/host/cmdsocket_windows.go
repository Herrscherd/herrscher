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
	if instanceID != "" {
		return `\\.\pipe\herrscher-command-` + instanceID
	}
	return `\\.\pipe\herrscher-command`
}

// SessionCommandSocketPathIn is the per-session command pipe; the layout is not
// used on this platform.
func SessionCommandSocketPathIn(_ RuntimeLayout, instanceID, session string) string {
	name := `\\.\pipe\herrscher-command`
	if instanceID != "" {
		name += "-" + instanceID
	}
	return name + "-s-" + control.SafeSessionName(session)
}

// EventsSocketPath is the daemon-level per-session events fan-out pipe: a sibling
// of the command pipe (herrscher-command → herrscher-events).
func EventsSocketPath(instanceID string) string {
	return EventsSocketPathIn(RuntimeLayout{}, instanceID)
}

// EventsSocketPathIn is the per-session events pipe; the layout is not used on
// this platform.
func EventsSocketPathIn(_ RuntimeLayout, instanceID string) string {
	if instanceID != "" {
		return `\\.\pipe\herrscher-events-` + instanceID
	}
	return `\\.\pipe\herrscher-events`
}

func listenCommandSocket(_ context.Context, _ RuntimeLayout, path string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
	})
}

func removeSocketFiles(_ RuntimeLayout, _ string, _ []string) error {
	return nil
}

func dialCommandSocket(ctx context.Context, _ RuntimeLayout, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}
