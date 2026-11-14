package control

import (
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type transientThenListener struct {
	net.Listener

	mu     sync.Mutex
	failed bool
}

func (l *transientThenListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	failedBefore := l.failed
	l.failed = true
	l.mu.Unlock()
	if !failedBefore {
		return nil, transientError()
	}
	return l.Listener.Accept()
}

func TestServeRetriesATransientErrorFromListen(t *testing.T) {
	captureLog(t)
	sock := filepath.Join(t.TempDir(), "retry.sock")
	ln, err := Listen(t.Context(), sock)
	if err != nil {
		t.Fatal(err)
	}
	acc := Serve(t.Context(), &transientThenListener{Listener: ln})
	t.Cleanup(func() { closeConn(acc) })

	client, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeConn(client) })

	select {
	case conn, ok := <-acc.Conns():
		if !ok {
			t.Fatalf("the accept loop ended on a transient error instead of retrying: %v", acc.Err())
		}
		t.Cleanup(func() { closeConn(conn) })
	case <-time.After(5 * time.Second):
		t.Fatal("no connection was delivered after a transient accept error on a Listen socket")
	}
}
