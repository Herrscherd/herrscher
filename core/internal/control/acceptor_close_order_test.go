package control

import (
	"context"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

func TestConnsCloseOnlyAfterTheListenerIsClosed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := &closeBlockingListener{release: make(chan struct{})}
		acc := newAcceptor(ln)
		go acc.loop()
		synctest.Wait()

		closed := make(chan error, 1)
		go func() { closed <- acc.Close() }()
		synctest.Wait()

		select {
		case _, ok := <-acc.Conns():
			if !ok {
				close(ln.release)
				t.Fatal("Conns closed while the listener was still closing")
			}
		default:
		}

		close(ln.release)
		if err := <-closed; err != nil {
			t.Fatalf("Close: %v", err)
		}
		requireConnsClosed(t, acc, "Conns stayed open after Close")
	})
}

func TestCloseReturnsOnlyOnceTheAcceptLoopExited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := &acceptBlockingListener{gate: make(chan struct{})}
		acc := newAcceptor(ln)
		go acc.loop()
		closed := make(chan error, 1)
		go func() { closed <- acc.Close() }()
		synctest.Wait()
		select {
		case err := <-closed:
			close(ln.gate)
			t.Fatalf("Close returned %v while the accept loop was still running", err)
		default:
		}
		close(ln.gate)
		if err := <-closed; err != nil {
			t.Fatalf("Close: %v", err)
		}
		requireConnsClosed(t, acc, "Conns stayed open after Close")
	})
}

func TestConnsClosesWhenTheLoopExitsOnItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acc := newAcceptor(failingListener{})
		go acc.loop()
		go acc.closeWhenDone(context.Background())
		requireConnsClosed(t, acc, "Conns stayed open after the loop exited on its own")
	})
}

func requireConnsClosed(tb testing.TB, acc *Acceptor, stayedOpen string) {
	tb.Helper()
	select {
	case _, ok := <-acc.Conns():
		if ok {
			tb.Fatal("a conn was delivered after Close")
		}
	case <-time.After(5 * time.Second):
		tb.Fatal(stayedOpen)
	}
}

type closeBlockingListener struct {
	failingListener

	release chan struct{}
}

func (l *closeBlockingListener) Close() error {
	<-l.release
	return nil
}

type acceptBlockingListener struct {
	failingListener

	gate chan struct{}
}

func (l *acceptBlockingListener) Accept() (net.Conn, error) {
	<-l.gate
	return nil, net.ErrClosed
}

type failingListener struct{}

func (failingListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }

func (failingListener) Close() error { return nil }

func (failingListener) Addr() net.Addr { return nil }
