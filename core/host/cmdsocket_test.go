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

func TestCommandSocketRestrictedToOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveCommandSocket(ctx, path, &fakeDispatcher{out: "unused"})
	waitForSocket(t, path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// The socket runs operator commands, so it must not be group/world reachable.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket perm = %o, want 600", perm)
	}
}

func TestCommandSocketClosesSilentConnection(t *testing.T) {
	// A peer that connects but never sends its request line must not pin the
	// handler goroutine: the read deadline closes it. Use a short deadline so the
	// test doesn't wait the production bound.
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveCommandSocketWithTimeout(ctx, path, &fakeDispatcher{out: "unused"}, 50*time.Millisecond)
	waitForSocket(t, path)

	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	// Send nothing. Once the deadline fires the handler returns and closes the
	// conn, so our read unblocks (EOF) well within a generous bound.
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Fatal("expected the server to close the silent connection, got data")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// Identity comes from the listener, not the message: two sessions must never
// share a socket, or the daemon could not tell them apart.
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

// A session name is free text. It reaches a filesystem path here.
func TestASessionSocketPathFoldsUnsafeNames(t *testing.T) {
	got := SessionCommandSocketPath("", "../../etc/passwd")
	if strings.ContainsAny(filepath.Base(got), "/\\") {
		t.Fatalf("path %q keeps a separator from the session name", got)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("path %q keeps a traversal from the session name", got)
	}
}

// The longest name the slugifier produces, on the longest instance id the
// validator accepts, used to make a path bind refused with "invalid argument".
// That failure is quiet where it matters: the session still starts, and its
// agent's `herrscher <verb>` then falls back to the state file with nobody
// deciding anything, which is the boundary going away exactly where the name
// got long. So the path must fold, and still bind.
func TestALongSessionNameStillGetsASocketThatBinds(t *testing.T) {
	long := strings.Repeat("b", 64)
	p := SessionCommandSocketPath(strings.Repeat("a", 16), long)
	if len(p) > maxUnixSocketPath {
		t.Fatalf("path is %d bytes, over the %d sun_path holds: %q", len(p), maxUnixSocketPath, p)
	}
	ln, err := net.Listen("unix", p)
	if err != nil {
		t.Fatalf("listen %q: %v", p, err)
	}
	_ = ln.Close()
	_ = os.Remove(p)
	// Folding must not fold two sessions onto one socket, which would hand one
	// session the other's identity.
	other := SessionCommandSocketPath(strings.Repeat("a", 16), long[:63]+"c")
	if p == other {
		t.Fatalf("two long names share %q", p)
	}
	// And it must be a function of the name alone, or the daemon and the
	// supervisor would each bind a different path.
	if again := SessionCommandSocketPath(strings.Repeat("a", 16), long); again != p {
		t.Fatalf("not stable: %q then %q", p, again)
	}
}

func TestSessionSocketsOfTwoInstancesDoNotCollide(t *testing.T) {
	if SessionCommandSocketPath("a", "revue") == SessionCommandSocketPath("b", "revue") {
		t.Fatal("two daemons on one machine must not share a session socket")
	}
}

// 0600 for the same reason the operator socket has it: it does not isolate the
// daemon from its own agent, which shares its uid, but it does keep every other
// account on the machine out.
func TestASessionSocketIsNotWorldReachable(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveCommandSocket(ctx, sock, dispatcherFunc(func(context.Context, []string) (string, error) {
		return "", nil
	}))
	waitForSocket(t, sock)
	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket mode %04o, want 0600", perm)
	}
}
