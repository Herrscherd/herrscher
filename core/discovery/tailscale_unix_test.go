//go:build unix

package discovery

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAStaleTailscaleSocketIsUnavailable(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "stale.sock")
	descriptor, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Bind(descriptor, &syscall.SockaddrUnix{Name: socket}); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Close(descriptor); err != nil {
		t.Fatal(err)
	}
	_, err = Tailscale{Socket: socket}.Find(context.Background())
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("got %v, want a refused connection on the stale socket", err)
	}
}

func TestAMissingTailscaleSocketStaysSilent(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "absent.sock")
	found, err := Tailscale{Socket: socket}.Find(context.Background())
	if err != nil || len(found) != 0 {
		t.Fatalf("got %+v, %v; want an empty silent source", found, err)
	}
}
