//go:build !windows

package host

import (
	"context"
	"net"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

// CommandSocketPath is the daemon-level operator command socket.
func CommandSocketPath(instanceID string) string {
	name := "herrscher-command.sock"
	if instanceID != "" {
		name = "herrscher-command-" + instanceID + ".sock"
	}
	return filepath.Join(os.TempDir(), name)
}

// SessionCommandSocketPath is the command socket one session's own processes
// dial, distinct from the operator's.
//
// It is what tells the daemon who is calling. Locally the agent and the daemon
// share a uid, so no secret the agent can read distinguishes it from the
// operator: any token it could present, it could also have replayed. Where the
// connection arrives is the one thing an agent does not get to author, because
// the daemon chose which listener serves which caller.
//
// Locally that is a guardrail, not a wall: nothing stops a determined agent
// from looking for the operator socket in the same directory. Remotely it is a
// real boundary, since ssh forwards this session's socket and no other, so the
// far machine has no path to anything else.
func SessionCommandSocketPath(instanceID, session string) string {
	name := "herrscher-command"
	if instanceID != "" {
		name += "-" + instanceID
	}
	return filepath.Join(os.TempDir(), name+"-s-"+control.SafeSessionName(session)+".sock")
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

func dialCommandSocket(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}
