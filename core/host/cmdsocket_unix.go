//go:build !windows

package host

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/sockfile"
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
	return sockfile.Listen(ctx, path, ownerOnlyFileMode)
}

func removeSocketFiles(layout RuntimeLayout, instanceID string, sessions []string) error {
	paths := []string{CommandSocketPathIn(layout, instanceID), EventsSocketPathIn(layout, instanceID)}
	for _, session := range sessions {
		paths = append(paths, SessionCommandSocketPathIn(layout, instanceID, session), control.SocketPath(layout, instanceID, session))
	}
	var errs []error
	for _, path := range paths {
		if err := removeSocketFile(path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func removeSocketFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode().Type() != fs.ModeSocket {
		return nil
	}
	return os.Remove(path)
}

func dialCommandSocket(ctx context.Context, layout RuntimeLayout, path string) (net.Conn, error) {
	if err := ensureRuntimeDir(layout, path); err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}
