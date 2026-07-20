//go:build windows

package control

import "testing"

func TestSocketPathWindows(t *testing.T) {
	if got := SocketPath("my session.1"); got != `\\.\pipe\herrscher-control-my-session-1` {
		t.Fatalf("SocketPath = %q", got)
	}
}
