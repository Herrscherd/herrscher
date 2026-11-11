//go:build windows

package control

import (
	"context"
	"net"
	"os"

	"github.com/Microsoft/go-winio"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// SocketPath returns the named pipe used to control the given session of the
// daemon instanceID. Named pipes have no directory, so the layout is unused.
func SocketPath(_ runtimedir.Layout, instanceID, session string) string {
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

func bindIdentity(string) (os.FileInfo, error) {
	return nil, nil
}

func releaseOwnedPath(path string, _ os.FileInfo) error {
	return removeStaleSocket(path)
}
