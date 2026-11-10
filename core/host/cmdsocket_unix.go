//go:build !windows

package host

import (
	"context"
	"net"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// CommandSocketPath is the daemon-level operator command socket.
func CommandSocketPath(instanceID string) string {
	name := "herrscher-command"
	if instanceID != "" {
		name += "-" + instanceID
	}
	return runtimedir.Socket(name)
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
	return runtimedir.Socket(name + "-s-" + control.SafeSessionName(session))
}

// EventsSocketPath is the daemon-level per-session events fan-out socket: a
// sibling of the command socket (herrscher-command → herrscher-events). It is the path
// Neublox's HerrscherEventSource connects to, derived there the same way.
func EventsSocketPath(instanceID string) string {
	name := "herrscher-events"
	if instanceID != "" {
		name += "-" + instanceID
	}
	return runtimedir.Socket(name)
}

func listenCommandSocket(ctx context.Context, path string) (net.Listener, error) {
	if err := ensureRuntimeDir(path); err != nil {
		return nil, err
	}
	return listenRestricted(ctx, path)
}

func dialCommandSocket(ctx context.Context, path string) (net.Conn, error) {
	if err := ensureRuntimeDir(path); err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}

func listenRestricted(ctx context.Context, path string) (net.Listener, error) {
	staging, err := os.MkdirTemp(filepath.Dir(path), ".hs-sock-")
	if err != nil {
		return listenAndRestrict(ctx, path)
	}
	defer func() { logCleanup(os.RemoveAll(staging)) }()
	tmp := filepath.Join(staging, "s")
	if len(tmp) > control.MaxUnixSocketPath {
		return listenAndRestrict(ctx, path)
	}
	listener, err := listenAndRestrict(ctx, tmp)
	if err != nil {
		return nil, err
	}
	if ul, ok := listener.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	if err := os.Rename(tmp, path); err != nil {
		logCleanup(listener.Close())
		return nil, err
	}
	return listener, nil
}

func listenAndRestrict(ctx context.Context, path string) (net.Listener, error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	if ul, ok := listener.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	if err := os.Chmod(path, ownerOnlyFileMode); err != nil {
		logCleanup(listener.Close())
		logCleanup(os.Remove(path))
		return nil, err
	}
	return listener, nil
}
