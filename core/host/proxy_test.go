package host

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

const proxyTestTimeout = 5 * time.Second

func serveProxyDaemon(t *testing.T, path string, handle func(net.Conn)) {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeRead(listener) })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer closeRead(conn)
				handle(conn)
			}()
		}
	}()
}

func TestParseProxyChannelAcceptsTheThreeSocketsOnly(t *testing.T) {
	for name, want := range map[string]ProxyChannel{"command": ProxyCommand, "events": ProxyEvents, "approval": ProxyApproval} {
		got, err := ParseProxyChannel(name)
		if err != nil || got != want || got.String() != name {
			t.Fatalf("ParseProxyChannel(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
	for _, name := range []string{"", "Command", "session", "command "} {
		if _, err := ParseProxyChannel(name); !errors.Is(err, errUnknownProxyChannel) {
			t.Fatalf("ParseProxyChannel(%q) error = %v, want errUnknownProxyChannel", name, err)
		}
	}
}

func TestProxyCommandCarriesOneRequestAndItsAnswer(t *testing.T) {
	layout := testRuntime(t)
	serveProxyDaemon(t, CommandSocketPathIn(layout, ""), func(conn net.Conn) {
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		if !strings.Contains(string(line), `"argv":["session","list"]`) {
			return
		}
		answer := `{"ok":"listed"}` + "\n"
		_, writeErr := io.WriteString(conn, answer)
		logCleanup(writeErr)
	})
	request := `{"argv":["session","list"],"lang":"en"}` + "\n"
	var answer strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	if err := RunProxy(ctx, layout, "", "", ProxyCommand, strings.NewReader(request), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.String() != `{"ok":"listed"}`+"\n" {
		t.Fatalf("answer = %q", answer.String())
	}
}

func TestProxyEventsStreamUntilTheDaemonCloses(t *testing.T) {
	layout := testRuntime(t)
	serveProxyDaemon(t, EventsSocketPathIn(layout, ""), streamEventsAfterCursor)
	stdin, stdinWriter := io.Pipe()
	t.Cleanup(func() { logCleanup(stdinWriter.Close()) })
	stdout, stdoutWriter := io.Pipe()
	t.Cleanup(func() { logCleanup(stdoutWriter.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunProxy(ctx, layout, "", "", ProxyEvents, stdin, stdoutWriter)
	}()
	if _, err := io.WriteString(stdinWriter, `{"session":"a"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	expectLines(t, bufio.NewReader(stdout), `{"session":"a","seq":1}`, `{"session":"a","seq":2}`)
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunProxy returned %v after the client closed its side, want nil", err)
	}
}

func streamEventsAfterCursor(conn net.Conn) {
	reader := bufio.NewReader(conn)
	cursor, err := reader.ReadBytes('\n')
	if err != nil || !strings.Contains(string(cursor), `"session"`) {
		return
	}
	for _, line := range []string{`{"session":"a","seq":1}`, `{"session":"a","seq":2}`} {
		if _, writeErr := io.WriteString(conn, line+"\n"); writeErr != nil {
			return
		}
	}
	_, drainErr := io.Copy(io.Discard, reader)
	logCleanup(drainErr)
}

func expectLines(t *testing.T, lines *bufio.Reader, want ...string) {
	t.Helper()
	for _, expected := range want {
		got, err := lines.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if got != expected+"\n" {
			t.Fatalf("line = %q, want %q", got, expected)
		}
	}
}

func TestProxyApprovalReachesTheApprovalSocket(t *testing.T) {
	layout := testRuntime(t)
	serveProxyDaemon(t, ApprovalSocketPathIn(layout, ""), func(conn net.Conn) {
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		_, writeErr := conn.Write(line)
		logCleanup(writeErr)
	})
	request := `{"ID":"req-1","Tool":"Bash","Subject":"ls"}` + "\n"
	var answer strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	if err := RunProxy(ctx, layout, "", "", ProxyApproval, strings.NewReader(request), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.String() != request {
		t.Fatalf("answer = %q, want the request echoed back", answer.String())
	}
}

func TestProxyReportsADaemonThatIsNotRunning(t *testing.T) {
	layout := testRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	for _, channel := range []ProxyChannel{ProxyCommand, ProxyEvents, ProxyApproval} {
		err := RunProxy(ctx, layout, "", "", channel, strings.NewReader(""), io.Discard)
		if !errors.Is(err, errProxyDaemonAbsent) {
			t.Fatalf("%s: RunProxy error = %v, want errProxyDaemonAbsent", channel, err)
		}
	}
}

func TestProxyReturnsCleanlyWhenTheDaemonHangsUp(t *testing.T) {
	layout := testRuntime(t)
	serveProxyDaemon(t, EventsSocketPathIn(layout, ""), func(net.Conn) {})
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	if err := RunProxy(ctx, layout, "", "", ProxyEvents, strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("RunProxy error = %v, want nil when the daemon closes", err)
	}
}

func TestProxyStopsWhenItsContextEnds(t *testing.T) {
	layout := testRuntime(t)
	serveProxyDaemon(t, EventsSocketPathIn(layout, ""), func(conn net.Conn) {
		_, drainErr := io.Copy(io.Discard, conn)
		logCleanup(drainErr)
	})
	stdin, stdinWriter := io.Pipe()
	t.Cleanup(func() { logCleanup(stdinWriter.Close()) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunProxy(ctx, layout, "", "", ProxyEvents, stdin, io.Discard)
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunProxy error = %v, want context.Canceled", err)
		}
	case <-time.After(proxyTestTimeout):
		t.Fatal("RunProxy did not return after its context ended")
	}
}
