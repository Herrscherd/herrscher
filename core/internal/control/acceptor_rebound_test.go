//go:build !windows

package control_test

import (
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestLateCloseLeavesTheReboundSocketAlone(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "rebound.sock")
	old, err := control.Accept(t.Context(), sock)
	if err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := control.Accept(t.Context(), sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err = replacement.Close(); err != nil {
			t.Error(err)
		}
	})

	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	conn, err := control.Dial(sock)
	if err != nil {
		t.Fatalf("a second Close of a released acceptor removed the replacement socket: %v", err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
}
