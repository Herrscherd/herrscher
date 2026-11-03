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
		return "", false, nil
	}
	defer closeRead(conn)
	if deadline, ok := ctx.Deadline(); ok {
		logCleanup(conn.SetDeadline(deadline))
	}
	b, err := json.Marshal(cmdRequest{Argv: argv})
	if err != nil {
		return "", true, err
	}
	if _, writeErr := conn.Write(append(b, '\n')); writeErr != nil {
		return "", true, writeErr
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return "", true, err
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

func serveCommandSocket(ctx context.Context, path string, disp dispatcher) {
	serveCommandSocketWithTimeout(ctx, path, disp, defaultCommandReadTimeout)
}

func serveCommandSocketWithTimeout(ctx context.Context, path string, disp dispatcher, readTimeout time.Duration) {
	logCleanup(os.Remove(path))
	ln, err := listenCommandSocket(ctx, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "command socket: listen %s: %v\n", path, err)
		return
	}
	go func() { <-ctx.Done(); closeRead(ln); logCleanup(os.Remove(path)) }()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go handleCommandConn(ctx, c, disp, readTimeout)
	}
}

func handleCommandConn(ctx context.Context, c net.Conn, disp dispatcher, readTimeout time.Duration) {
	defer closeRead(c)
	logCleanup(c.SetReadDeadline(time.Now().Add(readTimeout)))
	line, err := bufio.NewReader(c).ReadBytes('\n')
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
	b, marshalErr := json.Marshal(resp)
	logCleanup(marshalErr)
	logWriteFailure(c.Write(append(b, '\n')))
}
