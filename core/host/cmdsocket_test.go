package host

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeDispatcher struct {
	out     string
	err     error
	gotArgv []string
}

func (f *fakeDispatcher) Dispatch(_ context.Context, argv []string) (string, error) {
	f.gotArgv = argv
	return f.out, f.err
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s never appeared", path)
}

func sendCommand(t *testing.T, path string, req cmdRequest) cmdResponse {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		t.Fatalf("read: %v", err)
	}
	var resp cmdResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal %q: %v", line, err)
	}
	return resp
}

func TestCommandSocketDispatchesOk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	disp := &fakeDispatcher{out: `[{"name":"lead1"}]`}
	go serveCommandSocket(ctx, path, disp)
	waitForSocket(t, path)

	resp := sendCommand(t, path, cmdRequest{Argv: []string{"session", "list", "--json"}})
	if resp.Ok == nil || *resp.Ok != disp.out {
		t.Fatalf("want ok=%q, got %+v", disp.out, resp)
	}
	if resp.Err != nil {
		t.Fatalf("unexpected err: %v", *resp.Err)
	}
	if len(disp.gotArgv) != 3 || disp.gotArgv[0] != "session" {
		t.Fatalf("argv not forwarded: %v", disp.gotArgv)
	}
}

func TestCommandSocketReportsDispatchError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	disp := &fakeDispatcher{err: errString("boom")}
	go serveCommandSocket(ctx, path, disp)
	waitForSocket(t, path)

	resp := sendCommand(t, path, cmdRequest{Argv: []string{"session", "list"}})
	if resp.Ok != nil {
		t.Fatalf("expected no ok, got %q", *resp.Ok)
	}
	if resp.Err == nil || *resp.Err != "boom" {
		t.Fatalf("want err=boom, got %+v", resp)
	}
}

func TestCommandSocketRejectsMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	disp := &fakeDispatcher{out: "unused"}
	go serveCommandSocket(ctx, path, disp)
	waitForSocket(t, path)

	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("not json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, _ := bufio.NewReader(c).ReadBytes('\n')
	var resp cmdResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal %q: %v", line, err)
	}
	if resp.Err == nil {
		t.Fatalf("malformed request should yield an err response: %+v", resp)
	}
	if disp.gotArgv != nil {
		t.Fatalf("dispatcher must not be called on malformed input: %v", disp.gotArgv)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
