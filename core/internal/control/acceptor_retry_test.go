package control

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

const maxRetryListenerCalls = 1000

type retryListener struct {
	failingListener

	calls  atomic.Int64
	script []acceptResult
	repeat error
}

type acceptResult struct {
	conn net.Conn
	err  error
}

func (l *retryListener) Accept() (net.Conn, error) {
	call := int(l.calls.Add(1))
	switch {
	case call > maxRetryListenerCalls:
		return nil, net.ErrClosed
	case call <= len(l.script):
		step := l.script[call-1]
		return step.conn, step.err
	case l.repeat != nil:
		return nil, l.repeat
	default:
		return nil, net.ErrClosed
	}
}

func emfileError() error {
	return fmt.Errorf("accept tcp: %w", syscall.EMFILE)
}

func TestAcceptRetriesTemporaryErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := net.Pipe()
		defer closeConn(client)
		defer closeConn(server)

		ln := &retryListener{script: []acceptResult{{err: emfileError()}, {conn: server}}}
		acc := newAcceptor(ln)
		go acc.loop()

		select {
		case c, ok := <-acc.Conns():
			if !ok || c.c != server {
				t.Fatal("the conn accepted after a temporary error was not delivered")
			}
		case <-time.After(time.Second):
			t.Fatal("the accept loop gave up on a temporary error instead of retrying")
		}
		if err := acc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
}

func TestAcceptBacksOffWhileTemporaryErrorsPersist(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := &retryListener{repeat: emfileError()}
		acc := newAcceptor(ln)
		go acc.loop()

		time.Sleep(10 * time.Second)
		calls := ln.calls.Load()
		if calls < 12 || calls > 20 {
			t.Fatalf("Accept called %d times in 10s of persistent temporary errors, want a capped exponential backoff (12 to 20 calls)", calls)
		}
		if err := acc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
}

func TestCloseInterruptsAcceptBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := &retryListener{repeat: emfileError()}
		acc := newAcceptor(ln)
		go acc.loop()
		synctest.Wait()

		select {
		case <-acc.exited:
			t.Fatal("the accept loop exited on a temporary error instead of backing off")
		default:
		}

		start := time.Now()
		if err := acc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if waited := time.Since(start); waited >= 5*time.Millisecond {
			t.Fatalf("Close waited %s for the accept backoff, want it to return at once", waited)
		}
		requireConnsClosed(t, acc, "Conns stayed open after Close during the backoff")
	})
}

func TestAcceptLoopEndsOnClosedListener(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := &retryListener{repeat: fmt.Errorf("accept tcp: %w", net.ErrClosed)}
		acc := newAcceptor(ln)
		go acc.loop()
		go acc.closeWhenDone(context.Background())
		requireConnsClosed(t, acc, "Conns stayed open after the listener was closed")
		if calls := ln.calls.Load(); calls != 1 {
			t.Fatalf("Accept called %d times after net.ErrClosed, want 1", calls)
		}
	})
}
