package control

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

func TestCloseReleasesAConnBlockedOnHandoff(t *testing.T) {
	var first, second *closeRecorder
	synctest.Test(t, func(t *testing.T) {
		ln := &scriptedListener{conns: make(chan net.Conn, 2)}
		first, second = newCloseRecorder(), newCloseRecorder()
		ln.conns <- first
		ln.conns <- second
		acc := newAcceptor(ln)
		go acc.loop()
		synctest.Wait()
		if err := acc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	if !first.isClosed() {
		t.Error("the conn left buffered in the acceptor was never closed")
	}
	if !second.isClosed() {
		t.Error("the conn blocked on the handoff at Close was never closed")
	}
}

type scriptedListener struct {
	conns     chan net.Conn
	closeOnce sync.Once
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	conn, ok := <-l.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return conn, nil
}

func (l *scriptedListener) Close() error {
	l.closeOnce.Do(func() { close(l.conns) })
	return nil
}

func (l *scriptedListener) Addr() net.Addr { return nil }

type closeRecorder struct {
	net.Conn

	closed atomic.Bool
}

func newCloseRecorder() *closeRecorder {
	return &closeRecorder{}
}

func (c *closeRecorder) Close() error {
	c.closed.Store(true)
	return nil
}

func (c *closeRecorder) isClosed() bool {
	return c.closed.Load()
}
