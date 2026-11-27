package host

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

const (
	proxyStandInEnv    = "HERRSCHER_TEST_SSH_STAND_IN"
	proxyStandInRoot   = "HERRSCHER_TEST_SSH_ROOT"
	proxyStandInStatus = "HERRSCHER_TEST_SSH_STATUS"
)

func TestMain(m *testing.M) {
	if os.Getenv(proxyStandInEnv) != "1" {
		m.Run()
		return
	}
	status := []byte(strconv.Itoa(runSSHStandIn()))
	if err := os.WriteFile(os.Getenv(proxyStandInStatus), status, 0o600); err != nil {
		panic(err)
	}
}

func runSSHStandIn() int {
	layout, err := runtimedir.At(os.Getenv(proxyStandInRoot))
	if err != nil {
		return 3
	}
	remote := strings.Fields(os.Args[len(os.Args)-1])
	if len(remote) != 4 || remote[0] != "exec" || strings.Trim(remote[1], "'") != "herrscher" || strings.Trim(remote[2], "'") != "proxy" {
		return 4
	}
	channel, err := ParseProxyChannel(strings.Trim(remote[3], "'"))
	if err != nil {
		return 5
	}
	if err := RunProxy(context.Background(), layout, "", "", channel, os.Stdin, os.Stdout); err != nil {
		_, writeErr := os.Stderr.WriteString(err.Error() + "\n")
		logCleanup(writeErr)
		return 1
	}
	return 0
}

func installSSHStandIn(t *testing.T, layout runtimedir.Layout) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	status := filepath.Join(dir, "status")
	script := "#!/bin/sh\n'" + binary + "' \"$@\"\nexit \"$(cat '" + status + "')\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(proxyStandInEnv, "1")
	t.Setenv(proxyStandInRoot, layout.Dir())
	t.Setenv(proxyStandInStatus, status)
}

func TestDialProxyCarriesACommandOverSSH(t *testing.T) {
	layout := testRuntime(t)
	installSSHStandIn(t, layout)
	serveProxyDaemon(t, CommandSocketPathIn(layout, ""), func(conn net.Conn) {
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		_, writeErr := io.WriteString(conn, "echo "+strings.TrimSpace(string(line))+"\n")
		logCleanup(writeErr)
	})
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	conn, err := DialProxy(ctx, "build1", ProxyCommand)
	if err != nil {
		t.Fatal(err)
	}
	if _, writeErr := io.WriteString(conn, `{"argv":["host","list"]}`+"\n"); writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr := conn.CloseWrite(); closeErr != nil {
		t.Fatal(closeErr)
	}
	answer, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if answer != "echo {\"argv\":[\"host\",\"list\"]}\n" {
		t.Fatalf("answer = %q", answer)
	}
	if closeErr := conn.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestDialProxyStreamsEventsAndStopsOnClose(t *testing.T) {
	layout := testRuntime(t)
	installSSHStandIn(t, layout)
	serveProxyDaemon(t, EventsSocketPathIn(layout, ""), func(conn net.Conn) {
		reader := bufio.NewReader(conn)
		if _, err := reader.ReadBytes('\n'); err != nil {
			return
		}
		_, writeErr := io.WriteString(conn, `{"session":"a","seq":7}`+"\n")
		logCleanup(writeErr)
		_, drainErr := io.Copy(io.Discard, reader)
		logCleanup(drainErr)
	})
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	conn, err := DialProxy(ctx, "build1", ProxyEvents)
	if err != nil {
		t.Fatal(err)
	}
	if _, writeErr := io.WriteString(conn, `{"session":"a"}`+"\n"); writeErr != nil {
		t.Fatal(writeErr)
	}
	event, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if event != `{"session":"a","seq":7}`+"\n" {
		t.Fatalf("event = %q", event)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close after a stopped stream = %v, want nil", err)
	}
}

func TestDialProxyCarriesApprovalOverSSH(t *testing.T) {
	layout := testRuntime(t)
	installSSHStandIn(t, layout)
	serveProxyDaemon(t, ApprovalSocketPathIn(layout, ""), func(conn net.Conn) {
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		_, writeErr := conn.Write(line)
		logCleanup(writeErr)
	})
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	conn, err := DialProxy(ctx, "build1", ProxyApproval)
	if err != nil {
		t.Fatal(err)
	}
	request := `{"ID":"req-9","Tool":"Bash","Subject":"make"}` + "\n"
	if _, writeErr := io.WriteString(conn, request); writeErr != nil {
		t.Fatal(writeErr)
	}
	answer, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if answer != request {
		t.Fatalf("answer = %q, want the request echoed back", answer)
	}
	if closeErr := conn.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestDialProxyReportsADaemonThatIsNotRunning(t *testing.T) {
	layout := testRuntime(t)
	installSSHStandIn(t, layout)
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	conn, err := DialProxy(ctx, "build1", ProxyEvents)
	if err != nil {
		t.Fatal(err)
	}
	if _, readErr := io.ReadAll(conn); readErr != nil {
		t.Fatal(readErr)
	}
	closeErr := conn.Close()
	if closeErr == nil {
		t.Fatal("Close after a missing daemon = nil, want the remote exit status")
	}
	if !strings.Contains(conn.Stderr(), errProxyDaemonAbsent.Error()) {
		t.Fatalf("stderr = %q, want the daemon-absent reason", conn.Stderr())
	}
}

func TestDialProxyRefusesATargetSSHWouldReadAsAnOption(t *testing.T) {
	_, err := DialProxy(context.Background(), "-oProxyCommand=x", ProxyEvents)
	if !errors.Is(err, runner.ErrTargetDash) {
		t.Fatalf("DialProxy error = %v, want runner.ErrTargetDash", err)
	}
}
