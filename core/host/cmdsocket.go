package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// dispatcher runs an operator command and returns its stdout. *hub satisfies it
// (hub.Dispatch), so the command socket reaches the live registry + coordinator.
type dispatcher interface {
	Dispatch(context.Context, []string) (string, error)
}

// CommandSocketPath is the daemon-level operator command socket. The daemon
// accepts on it; an external reader (Neublox) dials it to run session list
// against the LIVE hub — the only way to observe in-memory coordinator state
// across the process boundary (a fresh CLI has no running coordinator). The path
// is derived, mirroring control.SocketPath, so both sides compute it without
// extra coordination state. instanceID namespaces daemons sharing a host.
func CommandSocketPath(instanceID string) string {
	name := "dctl-command"
	if instanceID != "" {
		name += "-" + instanceID
	}
	return filepath.Join(os.TempDir(), name+".sock")
}

type cmdRequest struct {
	Argv []string `json:"argv"`
}

type cmdResponse struct {
	Ok  *string `json:"ok,omitempty"`
	Err *string `json:"err,omitempty"`
}

// serveCommandSocket accepts operator commands on a Unix socket and dispatches
// each through disp. One connection = one JSON-line request {"argv":[...]} → one
// JSON-line response {"ok":...} | {"err":...}. Serialization of the actual
// command is disp's own concern (hub.Dispatch holds dispatchMu). Blocks until
// ctx is done; intended to run in a goroutine.
func serveCommandSocket(ctx context.Context, path string, disp dispatcher) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "command socket: listen %s: %v\n", path, err)
		return
	}
	// The socket dispatches arbitrary operator commands (hub.Dispatch), so restrict
	// it to the owner: it sits in a world-writable temp dir, and unlike the session
	// control socket it is an execution surface, not just a bridge.
	if err := os.Chmod(path, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "command socket: chmod %s: %v\n", path, err)
		_ = ln.Close()
		_ = os.Remove(path)
		return
	}
	go func() { <-ctx.Done(); _ = ln.Close(); _ = os.Remove(path) }()
	for {
		c, err := ln.Accept()
		if err != nil {
			return // listener closed (ctx done)
		}
		go handleCommandConn(ctx, c, disp)
	}
}

// handleCommandConn reads one request line, dispatches it, writes one response.
func handleCommandConn(ctx context.Context, c net.Conn, disp dispatcher) {
	defer c.Close()
	// Bound the read so a peer that connects but never sends a line can't pin this
	// goroutine open past shutdown. The legit client writes its request immediately.
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
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
