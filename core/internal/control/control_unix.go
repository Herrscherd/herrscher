//go:build !windows

package control

import (
	"context"
	"net"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
	"github.com/Herrscherd/herrscher/core/internal/sockfile"
)

// SocketPath returns the Unix socket path in layout used to control the given
// session of the daemon instanceID, so two daemons each running a session of one
// name never share a socket.
func SocketPath(layout runtimedir.Layout, instanceID, session string) string {
	return layout.Socket("herrscher-control-" + instancePrefix(instanceID) + SafeSessionName(session))
}

const controlSocketMode = 0o600

func listenControl(ctx context.Context, path string) (net.Listener, error) {
	return sockfile.Listen(ctx, path, controlSocketMode)
}

func dialControl(ctx context.Context, path string) (net.Conn, error) {
	return new(net.Dialer).DialContext(ctx, "unix", path)
}
