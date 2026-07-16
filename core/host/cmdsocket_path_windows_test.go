//go:build windows

package host

import "testing"

func TestCommandSocketPathWindows(t *testing.T) {
	if got := CommandSocketPath(""); got != `\\.\pipe\dctl-command` {
		t.Fatalf("CommandSocketPath(\"\") = %q", got)
	}
	if got := CommandSocketPath("instance-1"); got != `\\.\pipe\dctl-command-instance-1` {
		t.Fatalf("CommandSocketPath(instance-1) = %q", got)
	}
}
