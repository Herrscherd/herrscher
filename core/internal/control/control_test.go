//go:build !windows

package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketPathDeterministicAndSafe(t *testing.T) {
	a := SocketPath("", "my session.1")
	b := SocketPath("", "my session.1")
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
	if SocketPath("u1", "herrscher") == SocketPath("u2", "herrscher") {
		t.Fatal("two daemons running a session of one name must not share its control socket")
	}
	if SocketPath("u1", "herrscher") == SocketPath("", "herrscher") {
		t.Fatal("an instance must not fall back on the unnamespaced path")
	}
}

func TestSocketPathsFitTheUnixSocketLimit(t *testing.T) {
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	longDir := filepath.Join(dir, strings.Repeat("d", 30))
	if err := os.Mkdir(longDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", longDir)
	instance := strings.Repeat("i", 16)
	session := strings.Repeat("s", 64)

	path := SocketPath(instance, session)
	if len(path) > MaxUnixSocketPath {
		t.Fatalf("control socket is %d bytes, over the %d sun_path holds: %q", len(path), MaxUnixSocketPath, path)
	}
	listener, err := listenControl(t.Context(), path)
	if err != nil {
		t.Fatalf("binding the control socket %q: %v", path, err)
	}
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	for _, remote := range []string{
		RemoteSocketPath(instance, session),
		RemoteCommandSocketPath(instance, session),
	} {
		if len(remote) > MaxUnixSocketPath {
			t.Fatalf("remote socket is %d bytes, over the %d sun_path holds: %q", len(remote), MaxUnixSocketPath, remote)
		}
	}
}

func TestSocketPathsOfLongSessionsStayApart(t *testing.T) {
	shared := strings.Repeat("s", 80)
	if SocketPath("u1", shared+"a") == SocketPath("u1", shared+"b") {
		t.Fatal("two sessions that only differ past the length limit share a control socket")
	}
	if RemoteSocketPath("u1", shared+"a") == RemoteSocketPath("u1", shared+"b") {
		t.Fatal("two sessions that only differ past the length limit share a remote control socket")
	}
}
