//go:build windows

package host

import (
	"context"
	"net"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Microsoft/go-winio"
)

// CommandSocketPath is the daemon-level operator command pipe.
func CommandSocketPath(instanceID string) string {
	if instanceID != "" {
		return `\\.\pipe\herrscher-command-` + instanceID
	}
	return `\\.\pipe\herrscher-command`
}

// SessionCommandSocketPath is the command pipe one session's own processes
// dial, distinct from the operator's. See the unix file for why identity comes
// from which listener a connection arrives on rather than from a secret.
func SessionCommandSocketPath(instanceID, session string) string {
	name := `\\.\pipe\herrscher-command`
	if instanceID != "" {
		name += "-" + instanceID
	}
	return name + "-s-" + control.SafeSessionName(session)
}

// EventsSocketPath is the daemon-level per-session events fan-out pipe: a sibling
// of the command pipe (herrscher-command → herrscher-events).
func EventsSocketPath(instanceID string) string {
	if instanceID != "" {
		return `\\.\pipe\herrscher-events-` + instanceID
	}
	return `\\.\pipe\herrscher-events`
}

func listenCommandSocket(path string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
	})
}

func dialCommandSocket(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}
