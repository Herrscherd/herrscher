package host

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
)

const proxyVerb = "proxy"

// ProxyChannel is one local socket of the daemon that a route over ssh may carry.
type ProxyChannel int

const (
	// ProxyCommand is the operator command socket: one request line, one answer.
	ProxyCommand ProxyChannel = iota + 1
	// ProxyEvents is the events socket, a stream of NDJSON lines.
	ProxyEvents
)

var (
	errUnknownProxyChannel = errors.New("unknown proxy channel: use command or events")
	errProxyDaemonAbsent   = errors.New("the herrscher daemon does not answer on this machine: start it with herrscher serve")
)

// ParseProxyChannel reads the channel name of herrscher proxy: command or events.
func ParseProxyChannel(name string) (ProxyChannel, error) {
	switch name {
	case ProxyCommand.String():
		return ProxyCommand, nil
	case ProxyEvents.String():
		return ProxyEvents, nil
	}
	return 0, errUnknownProxyChannel
}

// String is the name of the channel on the command line.
func (c ProxyChannel) String() string {
	switch c {
	case ProxyCommand:
		return "command"
	case ProxyEvents:
		return "events"
	}
	return ""
}

// RunProxy relays stdin and stdout to the daemon socket of channel on this machine.
// The daemon is the one serving statePath, found with the same instance rule as the
// operator CLI. When stdin ends before the daemon closes, every byte read from it is
// sent and the daemon then sees end of input. The call returns once the daemon closes
// its side, or with the context error when ctx ends first; it does not wait for stdin
// to end, since a blocking read cannot be interrupted, so input not yet read by then is
// not sent. It closes stdin when it implements io.Closer. It fails with a daemon-absent
// error when nothing listens.
func RunProxy(ctx context.Context, layout RuntimeLayout, statePath, optID string, channel ProxyChannel, stdin io.Reader, stdout io.Writer) error {
	path, err := proxySocketPath(layout, ServedInstanceID(statePath, optID), channel)
	if err != nil {
		return err
	}
	conn, err := dialProxySocket(ctx, layout, path)
	if err != nil {
		return err
	}
	return relayProxy(ctx, conn, stdin, stdout)
}

func proxySocketPath(layout RuntimeLayout, instanceID string, channel ProxyChannel) (string, error) {
	switch channel {
	case ProxyCommand:
		return CommandSocketPathIn(layout, instanceID), nil
	case ProxyEvents:
		return EventsSocketPathIn(layout, instanceID), nil
	}
	return "", errUnknownProxyChannel
}

func relayProxy(ctx context.Context, conn net.Conn, stdin io.Reader, stdout io.Writer) error {
	stop := closeOnDone(ctx, conn)
	defer stop()
	go relayRequests(conn, stdin)
	_, err := io.Copy(stdout, conn)
	endRequests(stdin, conn)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func relayRequests(conn net.Conn, stdin io.Reader) {
	_, err := io.Copy(conn, stdin)
	logCleanup(ignoreVoluntaryClose(err))
	logCleanup(ignoreVoluntaryClose(closeWriteSide(conn)))
}

func endRequests(stdin io.Reader, conn net.Conn) {
	if closer, ok := stdin.(io.Closer); ok {
		logCleanup(closer.Close())
	}
	closeRead(conn)
}

func proxyDialError(ctx context.Context) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return errProxyDaemonAbsent
}

func ignoreVoluntaryClose(err error) error {
	if errors.Is(err, os.ErrClosed) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
		return nil
	}
	return err
}

type writeHalfCloser interface {
	CloseWrite() error
}

func closeWriteSide(conn net.Conn) error {
	half, ok := conn.(writeHalfCloser)
	if !ok {
		return nil
	}
	return half.CloseWrite()
}
