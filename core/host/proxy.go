package host

import (
	"context"
	"errors"
	"io"
	"net"
)

const proxyVerb = "proxy"

// ProxyChannel is one local socket of the daemon that a route over ssh may carry.
type ProxyChannel int

const (
	// ProxyCommand is the operator command socket: one request line, one answer.
	ProxyCommand ProxyChannel = iota + 1
	// ProxyEvents is the events socket, a stream of NDJSON lines.
	ProxyEvents
	// ProxyApproval is the approval socket.
	ProxyApproval
)

var (
	errUnknownProxyChannel = errors.New("unknown proxy channel: use command, events or approval")
	errProxyDaemonAbsent   = errors.New("the herrscher daemon does not answer on this machine: start it with herrscher serve")
)

// ParseProxyChannel reads the channel name of herrscher proxy: command, events or approval.
func ParseProxyChannel(name string) (ProxyChannel, error) {
	switch name {
	case ProxyCommand.String():
		return ProxyCommand, nil
	case ProxyEvents.String():
		return ProxyEvents, nil
	case ProxyApproval.String():
		return ProxyApproval, nil
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
	case ProxyApproval:
		return "approval"
	}
	return ""
}

// RunProxy relays stdin and stdout to the daemon socket of channel on this machine.
// The daemon is the one serving statePath, found with the same instance rule as the
// operator CLI. Closing stdin ends the request side; the call returns once the daemon
// closes its side, and fails with a daemon-absent error when nothing listens.
func RunProxy(ctx context.Context, layout RuntimeLayout, statePath, optID string, channel ProxyChannel, stdin io.Reader, stdout io.Writer) error {
	path, err := proxySocketPath(layout, ServedInstanceID(statePath, optID), channel)
	if err != nil {
		return err
	}
	conn, err := dialCommandSocket(ctx, newHostMessages(), layout, path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errProxyDaemonAbsent
	}
	return relayProxy(ctx, conn, stdin, stdout)
}

func proxySocketPath(layout RuntimeLayout, instanceID string, channel ProxyChannel) (string, error) {
	switch channel {
	case ProxyCommand:
		return CommandSocketPathIn(layout, instanceID), nil
	case ProxyEvents:
		return EventsSocketPathIn(layout, instanceID), nil
	case ProxyApproval:
		return ApprovalSocketPathIn(layout, instanceID), nil
	}
	return "", errUnknownProxyChannel
}

func relayProxy(ctx context.Context, conn net.Conn, stdin io.Reader, stdout io.Writer) error {
	stop := closeOnDone(ctx, conn)
	defer stop()
	defer closeRead(conn)
	go func() {
		_, err := io.Copy(conn, stdin)
		logCleanup(err)
		logCleanup(closeWriteSide(conn))
	}()
	_, err := io.Copy(stdout, conn)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
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
