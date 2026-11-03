//go:build !windows

package control_test

import (
	"errors"
	"io"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestCloseReleasesPendingConns(t *testing.T) {
	cases := []struct {
		name  string
		dials int
	}{
		{name: "one buffered", dials: 1},
		{name: "one buffered one blocked", dials: 2},
		{name: "several blocked", dials: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sock := filepath.Join(t.TempDir(), "p.sock")
			acc, err := control.Accept(t.Context(), sock)
			if err != nil {
				t.Fatalf("Accept: %v", err)
			}
			clients := dialClients(t, sock, tc.dials)
			if err := acc.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			assertScanEndsAfterClose(t, clients[0])
			for _, c := range clients {
				closeLogged(t, c)
			}
		})
	}
}

func dialClients(t *testing.T, sock string, n int) []*control.Conn {
	t.Helper()
	var clients []*control.Conn
	for i := range n {
		c, err := control.Dial(sock)
		if err != nil {
			t.Fatalf("Dial %d: %v", i, err)
		}
		clients = append(clients, c)
	}
	return clients
}

func assertScanEndsAfterClose(t *testing.T, c *control.Conn) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- c.Scan(func(contracts.Event) error { return nil })
	}()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
			t.Fatalf("scan after close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a conn left in the acceptor after Close was never closed")
	}
}
