//go:build !windows

package host

import (
	"net"
	"os"
	"path/filepath"
)

// CommandSocketPath is the daemon-level operator command socket.
func CommandSocketPath(instanceID string) string {
	name := "herrscher-command.sock"
	if instanceID != "" {
		name = "herrscher-command-" + instanceID + ".sock"
	}
	return filepath.Join(os.TempDir(), name)
}

// EventsSocketPath is the daemon-level per-session events fan-out socket: a
// sibling of the command socket (herrscher-command → herrscher-events). It is the path
// Neublox's HerrscherEventSource connects to, derived there the same way.
func EventsSocketPath(instanceID string) string {
	name := "herrscher-events.sock"
	if instanceID != "" {
		name = "herrscher-events-" + instanceID + ".sock"
	}
	return filepath.Join(os.TempDir(), name)
}

func listenCommandSocket(path string) (net.Listener, error) {
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = l.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return l, nil
}
