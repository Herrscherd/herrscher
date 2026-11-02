//go:build windows

package control

import (
	"net"

	"github.com/Microsoft/go-winio"
)

func SocketPath(session string) string {
	return `\\.\pipe\herrscher-control-` + SafeSessionName(session)
}

func listenControl(path string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
	})
}

func dialControl(path string) (net.Conn, error) {
	return winio.DialPipe(path, nil)
}
