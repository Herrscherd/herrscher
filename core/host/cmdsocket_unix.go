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

// CommandSocketPath is the daemon-level operator command socket of the user
// running this process.
func CommandSocketPath(instanceID string) string {
	return CommandSocketPathIn(ProductionRuntime(), instanceID)
}

// CommandSocketPathIn is the operator command socket of the daemon serving layout.
func CommandSocketPathIn(layout RuntimeLayout, instanceID string) string {
	name := "herrscher-command"
	if instanceID != "" {
		name += "-" + instanceID
	}
	return layout.Socket(name)
}

// SessionCommandSocketPathIn is the command socket of one session under layout.
func SessionCommandSocketPathIn(layout RuntimeLayout, instanceID, session string) string {
	name := "herrscher-command"
	if instanceID != "" {
		name += "-" + instanceID
	}
	return layout.Socket(name + "-s-" + control.SafeSessionName(session))
}

// EventsSocketPath is the unix socket of the daemon's per-session events fan-out
// for instanceID in the per-user directory of the user running this process. It
// is the contract for Neublox's HerrscherEventSource, which must call this
// function rather than rebuild the name. The directory is <run user>/<uid>/herrscher
// when systemd has made a private one for the uid, and /tmp/herrscher-<uid>
// otherwise. The socket is named herrscher-events (with -<instanceID> when one is
// set), or takes its hashed name in that directory when the full path is longer
// than the unix socket limit.
func EventsSocketPath(instanceID string) string {
	return EventsSocketPathIn(ProductionRuntime(), instanceID)
}

// EventsSocketPathIn is the events socket of the daemon serving layout.
func EventsSocketPathIn(layout RuntimeLayout, instanceID string) string {
	name := "herrscher-events"
	if instanceID != "" {
		name += "-" + instanceID
	}
	return layout.Socket(name)
}

func listenCommandSocket(ctx context.Context, layout RuntimeLayout, path string) (net.Listener, error) {
	if err := ensureRuntimeDir(layout, path); err != nil {
		return nil, err
	}
	return listenRestricted(ctx, path)
}

func dialCommandSocket(ctx context.Context, layout RuntimeLayout, path string) (net.Conn, error) {
	if err := ensureRuntimeDir(layout, path); err != nil {
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
	if len(tmp) > runtimedir.MaxUnixSocketPath {
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
