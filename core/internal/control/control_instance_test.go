package control

import "testing"

func TestSocketPathSeparatesInstanceFromSession(t *testing.T) {
	first := SocketPath("u1", "x-y")
	second := SocketPath("u1-x", "y")
	if first == second {
		t.Fatalf("instance u1 session x-y and instance u1-x session y share the control socket %q", first)
	}
}
