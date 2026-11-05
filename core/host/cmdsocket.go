package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

type dispatcher interface {
	Dispatch(context.Context, []string) (string, error)
}

const defaultCommandReadTimeout = 10 * time.Second

type cmdRequest struct {
	Argv []string `json:"argv"`
}

type cmdResponse struct {
	Ok  *string `json:"ok,omitempty"`
	Err *string `json:"err,omitempty"`
}

func dispatchLiveCommand(ctx context.Context, path string, argv []string) (string, bool, error) {
	dialCtx, cancel := context.WithTimeout(ctx, commandDialTimeout)
	defer cancel()
	conn, err := dialCommandSocket(dialCtx, path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", true, ctxErr
		}
		return "", false, nil
	}
	defer closeRead(conn)
	stopClose := closeOnDone(ctx, conn)
	defer stopClose()
	if deadline, ok := ctx.Deadline(); ok {
		logCleanup(conn.SetDeadline(deadline))
	}
	payload, err := json.Marshal(cmdRequest{Argv: argv})
	if err != nil {
		return "", true, err
	}
	if _, writeErr := conn.Write(append(payload, '\n')); writeErr != nil {
		return "", true, cancellationOr(ctx, writeErr)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return "", true, cancellationOr(ctx, err)
	}
	var resp cmdResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return "", true, fmt.Errorf("command socket: decode response: %w", err)
	}
	if resp.Err != nil {
		return "", true, &commandSocketError{Message: *resp.Err}
	}
	if resp.Ok == nil {
		return "", true, errCommandSocketEmpty
	}
	return *resp.Ok, true, nil
}

func cancellationOr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func serveCommandSocket(ctx context.Context, path string, disp dispatcher) {
	serveCommandSocketWithTimeout(ctx, path, disp, defaultCommandReadTimeout)
}

func serveCommandSocketWithTimeout(ctx context.Context, path string, disp dispatcher, readTimeout time.Duration) {
	logCleanup(os.Remove(path))
	listener, err := listenCommandSocket(ctx, path)
	if err != nil {
		warnStderr("command socket: listen %s: %v\n", path, err)
		return
	}
	go acceptCommands(ctx, listener, path, disp, readTimeout)
}

func acceptCommands(ctx context.Context, listener net.Listener, path string, disp dispatcher, readTimeout time.Duration) {
	go func() { <-ctx.Done(); closeRead(listener); logCleanup(os.Remove(path)) }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleCommandConn(ctx, conn, disp, readTimeout)
	}
}

func handleCommandConn(ctx context.Context, conn net.Conn, disp dispatcher, readTimeout time.Duration) {
	defer closeRead(conn)
	logCleanup(conn.SetReadDeadline(time.Now().Add(readTimeout)))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	var resp cmdResponse
	var req cmdRequest
	switch {
	case len(line) == 0 && err != nil:
		return
	case json.Unmarshal(line, &req) != nil || len(req.Argv) == 0:
		msg := "command socket: malformed request"
		resp.Err = &msg
	default:
		out, derr := disp.Dispatch(ctx, req.Argv)
		if derr != nil {
			msg := derr.Error()
			resp.Err = &msg
		} else {
			resp.Ok = &out
		}
	}
	payload, marshalErr := json.Marshal(resp)
	logCleanup(marshalErr)
	logWriteFailure(conn.Write(append(payload, '\n')))
}

func closeOnDone(ctx context.Context, conn net.Conn) func() {
	closed := make(chan struct{})
	stopAfter := context.AfterFunc(ctx, func() {
		defer close(closed)
		logCleanup(conn.Close())
	})
	return func() {
		if !stopAfter() {
			<-closed
		}
	}
}
