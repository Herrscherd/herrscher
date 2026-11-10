//go:build windows

package control

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

// SocketPath returns the named pipe used to control the given session of the
// daemon instanceID.
func SocketPath(instanceID, session string) string {
	return `\\.\pipe\herrscher-control-` + instancePrefix(instanceID) + SafeSessionName(session)
}

func listenControl(_ context.Context, path string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
	})
}

func dialControl(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}
