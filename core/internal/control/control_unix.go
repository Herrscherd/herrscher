//go:build !windows

package control

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

func SocketPath(session string) string {
	return filepath.Join(os.TempDir(), "herrscher-control-"+SafeSessionName(session)+".sock")
}

func listenControl(path string) (net.Listener, error) {
	return new(net.ListenConfig).Listen(context.Background(), "unix", path)
}

func dialControl(path string) (net.Conn, error) {
	return new(net.Dialer).DialContext(context.Background(), "unix", path)
}
