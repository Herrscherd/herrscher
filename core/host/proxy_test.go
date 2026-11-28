package host

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
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
	for name, want := range map[string]ProxyChannel{"command": ProxyCommand, "events": ProxyEvents} {
		got, err := ParseProxyChannel(name)
		if err != nil || got != want || got.String() != name {
			t.Fatalf("ParseProxyChannel(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
	for _, name := range []string{"", "Command", "session", "approval", "command "} {
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

func TestProxyReportsADaemonThatIsNotRunning(t *testing.T) {
	layout := testRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	for _, channel := range []ProxyChannel{ProxyCommand, ProxyEvents} {
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

func TestProxyRelaysTheWholeRequestWhenStdinEndsFirst(t *testing.T) {
	layout := testRuntime(t)
	const request = `{"session":"a","seq":0}` + "\n"
	received := make(chan string, 1)
	serveProxyDaemon(t, EventsSocketPathIn(layout, ""), func(conn net.Conn) {
		body, err := io.ReadAll(conn)
		if err != nil {
			return
		}
		received <- string(body)
		_, writeErr := io.WriteString(conn, `{"session":"a","seq":1}`+"\n")
		logCleanup(writeErr)
	})
	stdin, stdinWriter := io.Pipe()
	t.Cleanup(func() { logCleanup(stdinWriter.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunProxy(ctx, layout, "", "", ProxyEvents, stdin, io.Discard)
	}()
	first, second := request[:len(request)/2], request[len(request)/2:]
	if _, err := io.WriteString(stdinWriter, first); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdinWriter, second); err != nil {
		t.Fatal(err)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunProxy error = %v, want nil once the daemon answered", err)
	}
	if got := <-received; got != request {
		t.Fatalf("daemon received %q, want the whole request %q followed by end of input", got, request)
	}
}

func TestProxyReportsAnUnsafeRuntimeDirAsItsOwnError(t *testing.T) {
	layout := testRuntime(t)
	if err := os.Chmod(layout.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	err := RunProxy(ctx, layout, "", "", ProxyCommand, strings.NewReader(""), io.Discard)
	if _, ok := errors.AsType[*runtimedir.UnsafeError](err); !ok {
		t.Fatalf("RunProxy error = %v, want the unsafe runtime directory", err)
	}
	if errors.Is(err, errProxyDaemonAbsent) {
		t.Fatal("an unsafe runtime directory must not read as a daemon that is not running")
	}
}
