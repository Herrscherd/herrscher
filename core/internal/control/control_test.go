//go:build !windows

package control

import (
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func TestSocketPathDeterministicAndSafe(t *testing.T) {
	layout := useShortRuntimeRoot(t)
	a := SocketPath(layout, "", "my session.1")
	b := SocketPath(layout, "", "my session.1")
	if a != b {
		t.Fatalf("SocketPath not deterministic: %q vs %q", a, b)
	}
	base := filepath.Base(a)
	stem := strings.TrimSuffix(strings.TrimPrefix(base, "herrscher-control-"), ".sock")
	if stem == base || strings.ContainsAny(stem, " .") {
		t.Fatalf("unsafe socket filename: %q (stem %q)", base, stem)
	}
}

func TestSocketPathKeepsTwoInstancesApart(t *testing.T) {
	layout := useShortRuntimeRoot(t)
	if SocketPath(layout, "u1", "herrscher") == SocketPath(layout, "u2", "herrscher") {
		t.Fatal("two daemons running a session of one name must not share its control socket")
	}
	if SocketPath(layout, "u1", "herrscher") == SocketPath(layout, "", "herrscher") {
		t.Fatal("an instance must not fall back on the unnamespaced path")
	}
}

func TestSocketPathsFitTheUnixSocketLimit(t *testing.T) {
	layout := useShortRuntimeRoot(t)
	if _, err := layout.Private(); err != nil {
		t.Fatal(err)
	}
	instance := strings.Repeat("i", 16)
	session := strings.Repeat("s", 64)

	socket := SocketPath(layout, instance, session)
	if len(socket) > runtimedir.MaxUnixSocketPath {
		t.Fatalf("control socket is %d bytes, over the %d sun_path holds: %q", len(socket), runtimedir.MaxUnixSocketPath, socket)
	}
	listener, err := listenControl(t.Context(), socket)
	if err != nil {
		t.Fatalf("binding the control socket %q: %v", socket, err)
	}
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	for _, remote := range []string{
		RemoteSocketPath(remoteDirForTest, instance, session),
		RemoteCommandSocketPath(remoteDirForTest, instance, session),
	} {
		if len(remote) > runtimedir.MaxUnixSocketPath {
			t.Fatalf("remote socket is %d bytes, over the %d sun_path holds: %q", len(remote), runtimedir.MaxUnixSocketPath, remote)
		}
		if path.Dir(remote) != remoteDirForTest {
			t.Fatalf("remote socket %q is outside the remote private dir", remote)
		}
	}
}

func TestSocketPathsOfLongSessionsStayApart(t *testing.T) {
	layout := useShortRuntimeRoot(t)
	shared := strings.Repeat("s", 80)
	if SocketPath(layout, "u1", shared+"a") == SocketPath(layout, "u1", shared+"b") {
		t.Fatal("two sessions that only differ past the length limit share a control socket")
	}
	if RemoteSocketPath(remoteDirForTest, "u1", shared+"a") == RemoteSocketPath(remoteDirForTest, "u1", shared+"b") {
		t.Fatal("two sessions that only differ past the length limit share a remote control socket")
	}
}
