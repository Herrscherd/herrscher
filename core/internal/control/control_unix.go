//go:build !windows

package control

import (
	"context"
	"net"
	"os"
)

// SocketPath returns the Unix socket path used to control the given session of
// the daemon instanceID, so two daemons each running a session of one name
// never share a socket.
func SocketPath(instanceID, session string) string {
	return BoundedSocketPath(os.TempDir(), "herrscher-control-"+instancePrefix(instanceID)+SafeSessionName(session))
}

func listenControl(ctx context.Context, path string) (net.Listener, error) {
	return new(net.ListenConfig).Listen(ctx, "unix", path)
}

func dialControl(ctx context.Context, path string) (net.Conn, error) {
	return new(net.Dialer).DialContext(ctx, "unix", path)
}
