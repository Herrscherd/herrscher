//go:build !windows

package control_test

import (
	"io"
	"path/filepath"
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
			acc, err := control.Accept(sock)
			if err != nil {
				t.Fatalf("Accept: %v", err)
			}
			var clients []*control.Conn
			for i := 0; i < tc.dials; i++ {
				c, err := control.Dial(sock)
				if err != nil {
					t.Fatalf("Dial %d: %v", i, err)
				}
				clients = append(clients, c)
			}
			time.Sleep(50 * time.Millisecond)
			if err := acc.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			done := make(chan error, 1)
			go func() {
				done <- clients[0].Scan(func(contracts.Event) error { return nil })
			}()
			select {
			case err := <-done:
				if err != nil && err != io.EOF {
					t.Fatalf("scan after close: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("a conn left in the acceptor after Close was never closed")
			}
			for _, c := range clients {
				_ = c.Close()
			}
		})
	}
}
