package control

import (
	"net"
	"testing"
	"time"
)

func TestConnsCloseOnlyAfterTheListenerIsClosed(t *testing.T) {
	ln := newGatedListener()
	acc := &Acceptor{ln: ln, conns: make(chan *Conn, 1), done: make(chan struct{}), exited: make(chan struct{})}
	go acc.loop()
	<-ln.accepted
	<-acc.exited

	closed := make(chan error, 1)
	go func() { closed <- acc.Close() }()
	<-ln.closing

	select {
	case _, ok := <-acc.Conns():
		if !ok {
			t.Fatal("Conns closed while the listener was still closing")
		}
	default:
	}

	close(ln.release)
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case _, ok := <-acc.Conns():
		if ok {
			t.Fatal("a conn was delivered after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Conns stayed open after Close")
	}
}

type gatedListener struct {
	accepted chan struct{}
	closing  chan struct{}
	release  chan struct{}
}

func newGatedListener() *gatedListener {
	return &gatedListener{accepted: make(chan struct{}), closing: make(chan struct{}), release: make(chan struct{})}
}

func (l *gatedListener) Accept() (net.Conn, error) {
	close(l.accepted)
	return nil, net.ErrClosed
}

func (l *gatedListener) Close() error {
	close(l.closing)
	<-l.release
	return nil
}

func (l *gatedListener) Addr() net.Addr { return nil }
