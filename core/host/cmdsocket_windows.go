//go:build windows

package host

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// CommandSocketPath is the daemon-level operator command pipe.
func CommandSocketPath(instanceID string) string {
	if instanceID != "" {
		return `\\.\pipe\herrscher-command-` + instanceID
	}
	return `\\.\pipe\herrscher-command`
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
