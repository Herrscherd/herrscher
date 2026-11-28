//go:build unix

package host

import (
	"context"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestProxyReturnsWhenTheDaemonClosesWhileStdinBlocks(t *testing.T) {
	layout := testRuntime(t)
	serveProxyDaemon(t, EventsSocketPathIn(layout, ""), func(conn net.Conn) {
		_, writeErr := io.WriteString(conn, `{"session":"a","seq":1}`+"\n")
		logCleanup(writeErr)
	})
	stdin := blockingStdin(t)
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunProxy(ctx, layout, "", "", ProxyEvents, stdin, io.Discard)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunProxy error = %v, want nil when the daemon closes", err)
		}
	case <-time.After(proxyTestTimeout):
		t.Fatal("RunProxy waits on a blocking stdin after the daemon closed its side")
	}
}

func blockingStdin(t *testing.T) *os.File {
	t.Helper()
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	reader := os.NewFile(uintptr(fds[0]), "proxy-stdin")
	writer := os.NewFile(uintptr(fds[1]), "proxy-stdin-writer")
	t.Cleanup(func() {
		logCleanup(writer.Close())
		logCleanup(reader.Close())
	})
	return reader
}
