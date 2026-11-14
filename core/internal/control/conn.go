package control

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	initialAcceptRetryDelay = 5 * time.Millisecond
	acceptRetryGrowth       = 2
	maxAcceptRetryDelay     = time.Second
)

// Conn is a persistent bidirectional event connection over a net.Conn. The
// daemon (hub) accepts one per supervised bridge; the bridge dials. Each
// direction is an independent stream of JSON-line Events. Write is safe for
// concurrent callers (serialized by mu); Scan must be called from a single
// goroutine.
type Conn struct {
	c  net.Conn
	mu sync.Mutex
}

func newConn(c net.Conn) *Conn { return &Conn{c: c} }

// Write sends one Event as a JSON line.
func (c *Conn) Write(event contracts.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return WriteEvent(c.c, event)
}

// Emit satisfies contracts.EventSink so the bridge can hand a Conn directly to
// its turn loop as the event sink. A write error is logged at debug level only:
// emission is best-effort and the hub's read side detects a dead conn
// independently.
func (c *Conn) Emit(e contracts.Event) {
	if err := c.Write(e); err != nil {
		slog.Debug("control emit dropped", "err", err)
	}
}

// Scan reads events until the peer closes the connection or fn returns an error,
// calling fn for each. A clean peer close (io.EOF) yields nil; fn's error (or a
// malformed-line / read error) is returned otherwise.
func (c *Conn) Scan(fn func(contracts.Event) error) error {
	return ScanEvents(c.c, fn)
}

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.c.Close() }

// Acceptor listens on a control socket and yields one persistent Conn per
// dialing bridge, keeping each connection open for the session's life.
type Acceptor struct {
	ln        net.Listener
	conns     chan *Conn
	done      chan struct{}
	exited    chan struct{}
	closeOnce sync.Once
	closeErr  error
	failMu    sync.Mutex
	failure   error
}

var errNoSocketPath = errors.New("the control socket has no path, its runtime layout is zero")

// Accept binds the control socket at path, replacing a dead socket file left
// there by a crash, and starts accepting bridge connections. Each appears on
// Conns(). Conns also closes when the accept loop ends on a terminal error,
// which Err reports.
func Accept(ctx context.Context, path string) (*Acceptor, error) {
	listener, err := Listen(ctx, path)
	if err != nil {
		return nil, err
	}
	return Serve(ctx, listener), nil
}

// Listen binds the control socket at path, replacing a dead socket file left
// there by a crash.
func Listen(ctx context.Context, path string) (net.Listener, error) {
	if path == "" {
		return nil, errNoSocketPath
	}
	return listenControl(ctx, path)
}

// Serve accepts bridge connections on ln until ln is closed, ctx ends or the
// accept loop fails terminally. Each appears on Conns().
func Serve(ctx context.Context, ln net.Listener) *Acceptor {
	acceptor := newAcceptor(ln)
	go acceptor.loop()
	go acceptor.closeWhenDone(ctx)
	return acceptor
}

func newAcceptor(ln net.Listener) *Acceptor {
	return &Acceptor{ln: ln, conns: make(chan *Conn, 1), done: make(chan struct{}), exited: make(chan struct{})}
}

func (a *Acceptor) closeWhenDone(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-a.exited:
	}
	if err := a.Close(); err != nil {
		slog.Debug("control acceptor close", "err", err)
	}
}

func (a *Acceptor) loop() {
	defer close(a.exited)
	var retryDelay time.Duration
	for {
		conn, err := a.ln.Accept()
		if err != nil {
			delay, resume := a.recoverAccept(err, retryDelay)
			if !resume {
				return
			}
			retryDelay = delay
			continue
		}
		retryDelay = 0
		if !a.deliver(conn) {
			return
		}
	}
}

func (a *Acceptor) recoverAccept(err error, previous time.Duration) (time.Duration, bool) {
	if errors.Is(err, net.ErrClosed) || a.isDone() {
		return previous, false
	}
	if !isTransientAcceptError(err) {
		a.fail(err)
		slog.Error("control accept failed, stopping", "err", err)
		return previous, false
	}
	delay := nextAcceptRetryDelay(previous)
	slog.Warn("control accept failed, retrying", "err", err, "delay", delay)
	return delay, a.pause(delay)
}

func (a *Acceptor) deliver(conn net.Conn) bool {
	if a.isDone() {
		closeConn(conn)
		return false
	}
	select {
	case a.conns <- newConn(conn):
		return true
	case <-a.done:
		closeConn(conn)
		return false
	}
}

func nextAcceptRetryDelay(previous time.Duration) time.Duration {
	if previous == 0 {
		return initialAcceptRetryDelay
	}
	return min(previous*acceptRetryGrowth, maxAcceptRetryDelay)
}

func (a *Acceptor) isDone() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

func (a *Acceptor) pause(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-a.done:
		return false
	}
}

// Conns yields each bridge connection; it closes when the Acceptor is closed or
// its accept loop fails terminally.
func (a *Acceptor) Conns() <-chan *Conn { return a.conns }

// Err returns the terminal error that ended the accept loop, or nil after a
// Close or a cancelled context. Conns closes after a terminal error, so Err is
// meaningful once Conns is closed.
func (a *Acceptor) Err() error {
	a.failMu.Lock()
	defer a.failMu.Unlock()
	return a.failure
}

func (a *Acceptor) fail(err error) {
	a.failMu.Lock()
	defer a.failMu.Unlock()
	a.failure = err
}

// Close stops listening, waits for the accept loop to exit and then closes
// Conns. The socket file stays in place, dead, until the daemon's clean stop
// removes it.
func (a *Acceptor) Close() error {
	a.closeOnce.Do(func() { a.closeErr = a.shutdown() })
	return a.closeErr
}

func (a *Acceptor) shutdown() error {
	close(a.done)
	err := a.ln.Close()
	<-a.exited
	a.drainPending()
	close(a.conns)
	return err
}

func (a *Acceptor) drainPending() {
	for {
		select {
		case conn := <-a.conns:
			closeConn(conn)
		default:
			return
		}
	}
}

// Dial connects to the hub's control socket, returning a persistent Conn. The
// bridge calls this at startup and again on each reconnect.
func Dial(path string) (*Conn, error) {
	return DialContext(context.Background(), path)
}

// DialContext is Dial, abandoned when ctx is done.
func DialContext(ctx context.Context, path string) (*Conn, error) {
	conn, err := dialControl(ctx, path)
	if err != nil {
		return nil, err
	}
	return newConn(conn), nil
}

func closeConn(closer io.Closer) {
	if err := closer.Close(); err != nil {
		slog.Debug("control conn close", "err", err)
	}
}
