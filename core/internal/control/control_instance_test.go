package control

import "testing"

const remoteDirForTest = "/tmp/herrscher-1000"

func TestSocketPathSeparatesInstanceFromSession(t *testing.T) {
	layout := useShortRuntimeRoot(t)
	first := SocketPath(layout, "u1", "x-y")
	second := SocketPath(layout, "u1-x", "y")
	if first == second {
		t.Fatalf("instance u1 session x-y and instance u1-x session y share the control socket %q", first)
	}
}

func TestRemoteSocketPathsAreInstanceScoped(t *testing.T) {
	if RemoteSocketPath(remoteDirForTest, "u1", "s1") == RemoteSocketPath(remoteDirForTest, "u2", "s1") {
		t.Fatal("two instances forward one session to the same remote control socket")
	}
	if RemoteCommandSocketPath(remoteDirForTest, "u1", "s1") == RemoteCommandSocketPath(remoteDirForTest, "u2", "s1") {
		t.Fatal("two instances forward one session to the same remote command socket")
	}
	if RemoteSocketPath(remoteDirForTest, "u1", "s1") == RemoteCommandSocketPath(remoteDirForTest, "u1", "s1") {
		t.Fatal("the control and command sockets of one session share a path")
	}
}
