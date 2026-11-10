package control

import "testing"

func TestSocketPathSeparatesInstanceFromSession(t *testing.T) {
	first := SocketPath("u1", "x-y")
	second := SocketPath("u1-x", "y")
	if first == second {
		t.Fatalf("instance u1 session x-y and instance u1-x session y share the control socket %q", first)
	}
}

func TestRemoteSocketPathsAreInstanceScoped(t *testing.T) {
	if RemoteSocketPath("u1", "s1") == RemoteSocketPath("u2", "s1") {
		t.Fatal("two instances forward one session to the same remote control socket")
	}
	if RemoteCommandSocketPath("u1", "s1") == RemoteCommandSocketPath("u2", "s1") {
		t.Fatal("two instances forward one session to the same remote command socket")
	}
	if RemoteSocketPath("u1", "s1") == RemoteCommandSocketPath("u1", "s1") {
		t.Fatal("the control and command sockets of one session share a path")
	}
}
