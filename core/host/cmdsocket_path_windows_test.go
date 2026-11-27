//go:build windows

package host

import "testing"

func TestCommandSocketPathWindows(t *testing.T) {
	if got := CommandSocketPathIn(RuntimeLayout{}, "instance-1"); got != `\\.\pipe\herrscher-command-instance-1` {
		t.Fatalf("CommandSocketPathIn(instance-1) = %q", got)
	}
}

func TestEventsSocketPathOfNoInstanceIsEmpty(t *testing.T) {
	if got := EventsSocketPath(""); got != "" {
		t.Fatalf("EventsSocketPath(\"\") = %q, want no pipe", got)
	}
}
