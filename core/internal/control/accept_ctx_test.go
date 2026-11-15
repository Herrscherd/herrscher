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
	acc, err := serveSocket(ctx, sock)
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
	if err := acc.Err(); err != nil {
		t.Fatalf("Err after the context ended = %v, want nil", err)
	}
}

func TestAcceptErrIsNilAfterClose(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "close.sock")
	acc, err := serveSocket(t.Context(), sock)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err = closeBounded(t, acc); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err = acc.Err(); err != nil {
		t.Fatalf("Err after Close = %v, want nil", err)
	}
}
