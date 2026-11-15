package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

const maxRetryListenerCalls = 1000

type retryListener struct {
	failingListener

	mu        sync.Mutex
	callTimes []time.Time
	script    []acceptResult
	repeat    error
}

type acceptResult struct {
	conn net.Conn
	err  error
}

func (l *retryListener) Accept() (net.Conn, error) {
	call := l.record()
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

func (l *retryListener) record() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.callTimes = append(l.callTimes, time.Now())
	return len(l.callTimes)
}

func (l *retryListener) recordedCalls() []time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.callTimes)
}

func retryIntervals(calls []time.Time) []time.Duration {
	intervals := make([]time.Duration, 0, len(calls))
	for i := 1; i < len(calls); i++ {
		intervals = append(intervals, calls[i].Sub(calls[i-1]))
	}
	return intervals
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	logs := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

func transientError() error {
	return fmt.Errorf("accept: %w", transientAcceptFailure())
}

func testAcceptRetriesEachTransientError(t *testing.T, errs []error) {
	t.Helper()
	captureLog(t)
	for _, failure := range errs {
		t.Run(failure.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := net.Pipe()
				defer closeConn(client)
				defer closeConn(server)

				ln := &retryListener{script: []acceptResult{{err: fmt.Errorf("accept: %w", failure)}, {conn: server}}}
				acc := newAcceptor(ln)
				defer closeConn(acc)
				go acc.loop()

				select {
				case c, ok := <-acc.Conns():
					if !ok || c.c != server {
						t.Fatal("the conn accepted after a transient error was not delivered")
					}
				case <-time.After(time.Second):
					t.Fatal("the accept loop gave up on a transient error instead of retrying")
				}
				if err := acc.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
			})
		})
	}
}

func TestAcceptBackoffDoublesUpToTheCap(t *testing.T) {
	captureLog(t)
	synctest.Test(t, func(t *testing.T) {
		ln := &retryListener{repeat: transientError()}
		acc := newAcceptor(ln)
		defer closeConn(acc)
		go acc.loop()

		time.Sleep(10 * time.Second)
		waits := retryIntervals(ln.recordedCalls())
		want := []time.Duration{
			5 * time.Millisecond,
			10 * time.Millisecond,
			20 * time.Millisecond,
			40 * time.Millisecond,
			80 * time.Millisecond,
			160 * time.Millisecond,
			320 * time.Millisecond,
			640 * time.Millisecond,
			time.Second,
			time.Second,
			time.Second,
			time.Second,
		}
		for i := range min(len(waits), len(want)) {
			if waits[i] != want[i] {
				t.Fatalf("retry %d waited %s, want %s: the delay doubles from 5ms and stops at 1s (waits %v)", i+1, waits[i], want[i], waits[:i+1])
			}
		}
		if len(waits) < len(want) {
			t.Fatalf("Accept retried %d times in 10s of persistent temporary errors, want at least %d", len(waits), len(want))
		}
		if err := acc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
}

func TestCloseInterruptsAcceptBackoff(t *testing.T) {
	captureLog(t)
	synctest.Test(t, func(t *testing.T) {
		ln := &retryListener{repeat: transientError()}
		acc := newAcceptor(ln)
		defer closeConn(acc)
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
		if waited := time.Since(start); waited >= initialAcceptRetryDelay {
			t.Fatalf("Close waited %s for the accept backoff, want it to return at once", waited)
		}
		requireConnsClosed(t, acc, "Conns stayed open after Close during the backoff")
	})
}

func TestAcceptLoopEndsOnClosedListener(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := &retryListener{repeat: fmt.Errorf("accept tcp: %w", net.ErrClosed)}
		acc := newAcceptor(ln)
		defer closeConn(acc)
		go acc.loop()
		go acc.closeWhenDone(context.Background())
		requireConnsClosed(t, acc, "Conns stayed open after the listener was closed")
		if calls := len(ln.recordedCalls()); calls != 1 {
			t.Fatalf("Accept called %d times after net.ErrClosed, want 1", calls)
		}
		if err := acc.Err(); err != nil {
			t.Fatalf("Err after the listener was closed normally = %v, want nil", err)
		}
	})
}

var errPermanentAccept = errors.New("permanent accept failure")

func TestAcceptLoopEndsOnPermanentError(t *testing.T) {
	captureLog(t)
	synctest.Test(t, func(t *testing.T) {
		ln := &retryListener{repeat: errPermanentAccept}
		acc := newAcceptor(ln)
		defer closeConn(acc)
		go acc.loop()
		go acc.closeWhenDone(context.Background())
		requireConnsClosed(t, acc, "Conns stayed open after a permanent accept error")
		if calls := len(ln.recordedCalls()); calls != 1 {
			t.Fatalf("Accept called %d times after a permanent error, want 1", calls)
		}
		if err := acc.Err(); !errors.Is(err, errPermanentAccept) {
			t.Fatalf("Err after a permanent accept error = %v, want %v", err, errPermanentAccept)
		}
	})
}

func TestAcceptFailureLogIsBoundedPerInterval(t *testing.T) {
	logs := captureLog(t)
	synctest.Test(t, func(t *testing.T) {
		ln := &retryListener{repeat: transientError()}
		acc := newAcceptor(ln)
		defer closeConn(acc)
		go acc.loop()

		time.Sleep(100 * time.Second)
		if err := acc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	if got := strings.Count(logs.String(), acceptFailureLogLine); got != 4 {
		t.Fatalf("%d accept failures logged over 100s of persistent temporary errors, want 4: the first, then at most one per 30s", got)
	}
}

func TestSuccessfulAcceptRestartsBackoffAndLogBudget(t *testing.T) {
	logs := captureLog(t)
	synctest.Test(t, func(t *testing.T) {
		client, server := net.Pipe()
		defer closeConn(client)
		defer closeConn(server)

		ln := &retryListener{
			script: []acceptResult{{err: transientError()}, {conn: server}, {err: transientError()}},
			repeat: transientError(),
		}
		acc := newAcceptor(ln)
		defer closeConn(acc)
		go acc.loop()

		time.Sleep(50 * time.Millisecond)
		calls := ln.recordedCalls()
		if len(calls) < 4 {
			t.Fatalf("Accept called %d times in 50ms, want at least 4 to reach the retry after the second EMFILE", len(calls))
		}
		if got := retryIntervals(calls)[2]; got != initialAcceptRetryDelay {
			t.Fatalf("the retry after an EMFILE that followed a successful Accept waited %s, want %s: a success restarts the backoff", got, initialAcceptRetryDelay)
		}
		if err := acc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	if got := strings.Count(logs.String(), acceptFailureLogLine); got != 2 {
		t.Fatalf("%d accept failures logged, want 2: a success restarts the log budget, so the failure after it is logged and the ones within 30s are not", got)
	}
}

const acceptFailureLogLine = "control accept failed, retrying"
