//go:build windows

package host

import "testing"

func TestCommandSocketPathWindows(t *testing.T) {
	if got := CommandSocketPathIn(RuntimeLayout{}, ""); got != `\\.\pipe\herrscher-command` {
		t.Fatalf("CommandSocketPathIn(\"\") = %q", got)
	}
	if got := CommandSocketPathIn(RuntimeLayout{}, "instance-1"); got != `\\.\pipe\herrscher-command-instance-1` {
		t.Fatalf("CommandSocketPathIn(instance-1) = %q", got)
	}
}
