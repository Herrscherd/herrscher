//go:build !windows

package control_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestAcceptClosesTheSocketWhenItsContextEnds(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ctx.sock")
	ctx, cancel := context.WithCancel(t.Context())
	acc, err := control.Accept(ctx, sock)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	cancel()
	select {
	case _, ok := <-acc.Conns():
		if ok {
			t.Fatal("a conn was delivered after the context ended")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the acceptor stayed open after its context ended")
	}
	if conn, err := control.Dial(sock); err == nil {
		closeLogged(t, conn)
		t.Fatal("the socket still accepts after the context ended")
	}
}
