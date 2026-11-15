//go:build windows

package control

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func TestSocketPathWindows(t *testing.T) {
	if got := SocketPath(runtimedir.Layout{}, "", "my session.1"); got != `\\.\pipe\herrscher-control-my-session-1` {
		t.Fatalf("SocketPath = %q", got)
	}
}
