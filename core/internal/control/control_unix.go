//go:build !windows

package control

import (
	"net"
	"os"
	"path/filepath"
)

func SocketPath(session string) string {
	return filepath.Join(os.TempDir(), "dctl-control-"+safeSessionName(session)+".sock")
}

func listenControl(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}

func dialControl(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}
