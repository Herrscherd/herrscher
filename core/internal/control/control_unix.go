//go:build !windows

package control

import (
	"context"
	"net"
	"os"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// SocketPath returns the Unix socket path in layout used to control the given
// session of the daemon instanceID, so two daemons each running a session of one
// name never share a socket.
func SocketPath(layout runtimedir.Layout, instanceID, session string) string {
	return layout.Socket("herrscher-control-" + instancePrefix(instanceID) + SafeSessionName(session))
}

func listenControl(ctx context.Context, path string) (net.Listener, error) {
	listener, err := new(net.ListenConfig).Listen(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	if ul, ok := listener.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	return listener, nil
}

func bindIdentity(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func releaseOwnedPath(path string, own os.FileInfo) error {
	if own == nil {
		return nil
	}
	current, err := os.Lstat(path)
	if err == nil && os.SameFile(current, own) {
		return removeStaleSocket(path)
	}
	return nil
}

func dialControl(ctx context.Context, path string) (net.Conn, error) {
	return new(net.Dialer).DialContext(ctx, "unix", path)
}
