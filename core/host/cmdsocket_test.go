//go:build !windows

package host

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
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

func sendCommand(t *testing.T, path string, req cmdRequest) cmdResponse {
	t.Helper()
	c, err := (&net.Dialer{}).DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer closeRead(c)
	b, marshalErr := json.Marshal(req)
	logCleanup(marshalErr)
	if _, writeErr := c.Write(append(b, '\n')); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
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
	serveCommandSocket(ctx, isolatedRuntimeDir(t), path, disp)

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
	disp := &fakeDispatcher{err: stringError("boom")}
	serveCommandSocket(ctx, isolatedRuntimeDir(t), path, disp)

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
	serveCommandSocket(ctx, isolatedRuntimeDir(t), path, disp)

	c, err := (&net.Dialer{}).DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer closeRead(c)
	if _, err := c.Write([]byte("not json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, readErr := bufio.NewReader(c).ReadBytes('\n')
	logCleanup(readErr)
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

func TestCommandSocketRestrictedToOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveCommandSocket(ctx, isolatedRuntimeDir(t), path, &fakeDispatcher{out: "unused"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket perm = %o, want 600", perm)
	}
}

func TestCommandSocketClosesSilentConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := serveCommandSocketWithTimeout(ctx, isolatedRuntimeDir(t), path, &fakeDispatcher{out: "unused"}, 50*time.Millisecond); err != nil {
		t.Fatalf("serve: %v", err)
	}

	c, err := (&net.Dialer{}).DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer closeRead(c)
	logCleanup(c.SetReadDeadline(time.Now().Add(2 * time.Second)))
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Fatal("expected the server to close the silent connection, got data")
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }

func TestEachSessionGetsItsOwnCommandSocket(t *testing.T) {
	a := SessionCommandSocketPath("inst", "revue")
	b := SessionCommandSocketPath("inst", "release")
	if a == b {
		t.Fatalf("two sessions share %q", a)
	}
	if a == CommandSocketPath("inst") || b == CommandSocketPath("inst") {
		t.Fatal("a session socket must not be the operator socket")
	}
}

func TestASessionSocketPathFoldsUnsafeNames(t *testing.T) {
	got := SessionCommandSocketPath("", "../../etc/passwd")
	if strings.ContainsAny(filepath.Base(got), "/\\") {
		t.Fatalf("path %q keeps a separator from the session name", got)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("path %q keeps a traversal from the session name", got)
	}
}

func TestALongSessionNameStillGetsASocketThatBinds(t *testing.T) {
	layout := testRuntime(t)
	long := strings.Repeat("b", 64)
	p := SessionCommandSocketPathIn(layout, strings.Repeat("a", 16), long)
	if len(p) > runtimedir.MaxUnixSocketPath {
		t.Fatalf("path is %d bytes, over the %d sun_path holds: %q", len(p), runtimedir.MaxUnixSocketPath, p)
	}
	if err := ensureRuntimeDir(layout, p); err != nil {
		t.Fatalf("private runtime dir for %q: %v", p, err)
	}
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", p)
	if err != nil {
		t.Fatalf("listen %q: %v", p, err)
	}
	logCleanup(ln.Close())
	logCleanup(os.Remove(p))
	other := SessionCommandSocketPathIn(layout, strings.Repeat("a", 16), long[:63]+"c")
	if p == other {
		t.Fatalf("two long names share %q", p)
	}
	if again := SessionCommandSocketPathIn(layout, strings.Repeat("a", 16), long); again != p {
		t.Fatalf("not stable: %q then %q", p, again)
	}
}

func TestSessionSocketsOfTwoInstancesDoNotCollide(t *testing.T) {
	if SessionCommandSocketPath("a", "revue") == SessionCommandSocketPath("b", "revue") {
		t.Fatal("two daemons on one machine must not share a session socket")
	}
}

func TestASessionSocketIsNotWorldReachable(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveCommandSocket(ctx, isolatedRuntimeDir(t), sock, dispatcherFunc(func(context.Context, []string) (string, error) {
		return "", nil
	}))
	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket mode %04o, want 0600", perm)
	}
}
