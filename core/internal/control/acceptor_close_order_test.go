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
		ln := &gatedListener{release: make(chan struct{})}
		acc := newBareAcceptor(ln)
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
		ln := &gatedListener{acceptGate: make(chan struct{})}
		acc := newBareAcceptor(ln)
		go acc.loop()
		closed := make(chan error, 1)
		go func() { closed <- acc.Close() }()
		synctest.Wait()
		select {
		case err := <-closed:
			close(ln.acceptGate)
			t.Fatalf("Close returned %v while the accept loop was still running", err)
		default:
		}
		close(ln.acceptGate)
		if err := <-closed; err != nil {
			t.Fatalf("Close: %v", err)
		}
		requireConnsClosed(t, acc, "Conns stayed open after Close")
	})
}

func TestConnsClosesWhenTheLoopExitsOnItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acc := newBareAcceptor(&gatedListener{})
		go acc.loop()
		go acc.closeWhenDone(context.Background())
		requireConnsClosed(t, acc, "Conns stayed open after the loop exited on its own")
	})
}

func newBareAcceptor(ln net.Listener) *Acceptor {
	return &Acceptor{ln: ln, conns: make(chan *Conn, 1), done: make(chan struct{}), exited: make(chan struct{})}
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

type gatedListener struct {
	acceptGate chan struct{}
	release    chan struct{}
}

func (l *gatedListener) Accept() (net.Conn, error) {
	if l.acceptGate != nil {
		<-l.acceptGate
	}
	return nil, net.ErrClosed
}

func (l *gatedListener) Close() error {
	if l.release != nil {
		<-l.release
	}
	return nil
}

func (l *gatedListener) Addr() net.Addr { return nil }
