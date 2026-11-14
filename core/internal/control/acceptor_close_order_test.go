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

type blockedAcceptListener struct {
	closing    chan struct{}
	release    chan struct{}
	failAccept chan struct{}
}

func (l *blockedAcceptListener) Accept() (net.Conn, error) {
	<-l.failAccept
	return nil, net.ErrClosed
}

func (l *blockedAcceptListener) Close() error {
	close(l.closing)
	<-l.release
	return nil
}

func (l *blockedAcceptListener) Addr() net.Addr { return nil }

func TestCloseReturnsOnlyOnceTheAcceptLoopExited(t *testing.T) {
	ln := &blockedAcceptListener{closing: make(chan struct{}), release: make(chan struct{}), failAccept: make(chan struct{})}
	acc := &Acceptor{ln: ln, conns: make(chan *Conn, 1), done: make(chan struct{}), exited: make(chan struct{})}
	go acc.loop()
	closed := make(chan error, 1)
	go func() { closed <- acc.Close() }()
	<-ln.closing
	close(ln.release)
	select {
	case err := <-closed:
		t.Fatalf("Close returned %v while the accept loop was still running", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(ln.failAccept)
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case _, ok := <-acc.Conns():
		if ok {
			t.Fatal("a conn was delivered after Close")
		}
	default:
		t.Fatal("Conns still open after Close")
	}
}
