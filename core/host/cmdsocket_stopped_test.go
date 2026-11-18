package host

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

type gatedListener struct {
	closing chan struct{}
	gate    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func newGatedListener() *gatedListener {
	return &gatedListener{closing: make(chan struct{}), gate: make(chan struct{}), done: make(chan struct{})}
}

func (l *gatedListener) Accept() (net.Conn, error) {
	<-l.done
	return nil, net.ErrClosed
}

func (l *gatedListener) Close() error {
	l.once.Do(func() {
		close(l.closing)
		<-l.gate
		close(l.done)
	})
	return nil
}

func (l *gatedListener) Addr() net.Addr { return nil }

func TestCommandSocketStopsOnlyOnceItsListenerIsReallyClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listener := newGatedListener()
	idle := dispatcherFunc(func(context.Context, []string) (string, error) { return "unused", nil })
	stopped := acceptCommands(ctx, listener, idle, defaultCommandReadTimeout, time.Second, func(time.Duration) {}).stopped
	cancel()
	<-listener.closing
	select {
	case <-stopped:
		t.Fatal("the command socket reported stopped while its listener was still closing")
	default:
	}
	close(listener.gate)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the command socket never stopped after its listener closed")
	}
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

func TestReleaseCommandSocketsWaitsForTheAcceptorToStop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listener := newGatedListener()
	idle := dispatcherFunc(func(context.Context, []string) (string, error) { return "unused", nil })
	stopped := acceptCommands(ctx, listener, idle, defaultCommandReadTimeout, time.Second, func(time.Duration) {}).stopped
	released := make(chan struct{})
	go func() {
		releaseCommandSockets(noopCloser{}, noopCloser{}, cancel, stopped)
		close(released)
	}()
	<-listener.closing
	select {
	case <-released:
		t.Fatal("the release returned while the command listener was still closing")
	case <-time.After(time.Second):
	}
	close(listener.gate)
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the release never returned after the command listener closed")
	}
}
