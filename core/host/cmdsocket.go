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

// dispatcher runs an operator command and returns its stdout. *hub satisfies it
// (hub.Dispatch), so the command socket reaches the live registry + coordinator.
type dispatcher interface {
	Dispatch(context.Context, []string) (string, error)
}

// defaultCommandReadTimeout bounds how long a connection may take to send its
// request line before the handler gives up.
const defaultCommandReadTimeout = 10 * time.Second

type cmdRequest struct {
	Argv []string `json:"argv"`
}

type cmdResponse struct {
	Ok  *string `json:"ok,omitempty"`
	Err *string `json:"err,omitempty"`
}

// dispatchLiveCommand attempts one request against the running daemon. A dial
// miss is reported as handled=false so a short-lived operator process can keep
// its historical local fallback; once connected, protocol/dispatch failures
// are authoritative and returned to the caller.
func dispatchLiveCommand(ctx context.Context, path string, argv []string) (string, bool, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	conn, err := dialCommandSocket(dialCtx, path)
	if err != nil {
		return "", false, nil
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	b, err := json.Marshal(cmdRequest{Argv: argv})
	if err != nil {
		return "", true, err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return "", true, err
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
		return "", true, fmt.Errorf("%s", *resp.Err)
	}
	if resp.Ok == nil {
		return "", true, fmt.Errorf("command socket: empty response")
	}
	return *resp.Ok, true, nil
}

// serveCommandSocket accepts operator commands on a local socket and dispatches
// each through disp. One connection = one JSON-line request {"argv":[...]} → one
// JSON-line response {"ok":...} | {"err":...}. Serialization of the actual
// command is disp's own concern (hub.Dispatch holds dispatchMu). Blocks until
// ctx is done; intended to run in a goroutine.
func serveCommandSocket(ctx context.Context, path string, disp dispatcher) {
	serveCommandSocketWithTimeout(ctx, path, disp, defaultCommandReadTimeout)
}

func serveCommandSocketWithTimeout(ctx context.Context, path string, disp dispatcher, readTimeout time.Duration) {
	_ = os.Remove(path)
	ln, err := listenCommandSocket(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "command socket: listen %s: %v\n", path, err)
		return
	}
	go func() { <-ctx.Done(); _ = ln.Close(); _ = os.Remove(path) }()
	for {
		c, err := ln.Accept()
		if err != nil {
			return // listener closed (ctx done)
		}
		go handleCommandConn(ctx, c, disp, readTimeout)
	}
}

// handleCommandConn reads one request line, dispatches it, writes one response.
func handleCommandConn(ctx context.Context, c net.Conn, disp dispatcher, readTimeout time.Duration) {
	defer c.Close()
	// Bound the read so a peer that connects but never sends a line can't pin this
	// goroutine open past shutdown. The legit client writes its request immediately.
	_ = c.SetReadDeadline(time.Now().Add(readTimeout))
	line, err := bufio.NewReader(c).ReadBytes('\n')
	var resp cmdResponse
	var req cmdRequest
	switch {
	case len(line) == 0 && err != nil:
		return // peer closed with nothing
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
	b, _ := json.Marshal(resp)
	_, _ = c.Write(append(b, '\n'))
}
