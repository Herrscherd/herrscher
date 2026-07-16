//go:build windows

package host

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// CommandSocketPath is the daemon-level operator command pipe.
func CommandSocketPath(instanceID string) string {
	if instanceID != "" {
		return `\\.\pipe\dctl-command-` + instanceID
	}
	return `\\.\pipe\dctl-command`
}

func listenCommandSocket(path string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
	})
}
