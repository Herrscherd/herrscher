//go:build !windows

package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	prefix := filepath.Join(os.TempDir(), name+"-s-")
	short := control.SafeSessionName(session)
	if p := prefix + short + ".sock"; len(p) <= maxUnixSocketPath {
		return p
	}
	sum := sha256.Sum256([]byte(session))
	suffix := "-" + hex.EncodeToString(sum[:])[:12] + ".sock"
	room := max(maxUnixSocketPath-len(prefix)-len(suffix), 0)
	if len(short) > room {
		short = short[:room]
	}
	return prefix + short + suffix
}

const maxUnixSocketPath = 103

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

func listenCommandSocket(ctx context.Context, path string) (net.Listener, error) {
	return listenRestricted(ctx, path)
}

func dialCommandSocket(ctx context.Context, path string) (net.Conn, error) {
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
	if len(tmp) > maxUnixSocketPath {
		return listenAndRestrict(ctx, path)
	}
	l, err := listenAndRestrict(ctx, tmp)
	if err != nil {
		return nil, err
	}
	if ul, ok := l.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	if err := os.Rename(tmp, path); err != nil {
		logCleanup(l.Close())
		return nil, err
	}
	return l, nil
}

func listenAndRestrict(ctx context.Context, path string) (net.Listener, error) {
	l, err := (&net.ListenConfig{}).Listen(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, ownerOnlyFileMode); err != nil {
		logCleanup(l.Close())
		logCleanup(os.Remove(path))
		return nil, err
	}
	return l, nil
}
