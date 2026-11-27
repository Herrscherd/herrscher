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
	"time"

	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

const (
	proxyStandInEnv    = "HERRSCHER_TEST_SSH_STAND_IN"
	proxyStandInRoot   = "HERRSCHER_TEST_SSH_ROOT"
	proxyStandInStatus = "HERRSCHER_TEST_SSH_STATUS"
	proxyStandInBin    = "HERRSCHER_TEST_SSH_BIN"
	proxyTestTarget    = "build1"
	proxyTestBin       = "/home/build/.herrscher/bin/herrscher"
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
	args := os.Args
	if len(args) < 3 || args[len(args)-3] != "--" || args[len(args)-2] != proxyTestTarget {
		return 6
	}
	remote := strings.Fields(args[len(args)-1])
	if len(remote) != 4 || remote[0] != "exec" || strings.Trim(remote[1], "'") != os.Getenv(proxyStandInBin) || strings.Trim(remote[2], "'") != "proxy" {
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

func installSSHStandIn(t *testing.T, layout runtimedir.Layout, bin string) {
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
	t.Setenv(proxyStandInBin, bin)
}

func TestDialProxyCarriesACommandOverSSH(t *testing.T) {
	layout := testRuntime(t)
	installSSHStandIn(t, layout, proxyTestBin)
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
	conn, err := DialProxy(ctx, proxyTestTarget, proxyTestBin, ProxyCommand)
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
	installSSHStandIn(t, layout, proxyTestBin)
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
	conn, err := DialProxy(ctx, proxyTestTarget, proxyTestBin, ProxyEvents)
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

func TestDialProxyReportsADaemonThatIsNotRunning(t *testing.T) {
	layout := testRuntime(t)
	installSSHStandIn(t, layout, proxyTestBin)
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	conn, err := DialProxy(ctx, proxyTestTarget, proxyTestBin, ProxyEvents)
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
	_, err := DialProxy(context.Background(), "-oProxyCommand=x", proxyTestBin, ProxyEvents)
	if !errors.Is(err, runner.ErrTargetDash) {
		t.Fatalf("DialProxy error = %v, want runner.ErrTargetDash", err)
	}
}

func TestDialProxyFallsBackToTheLoginPathWithoutARecordedBinary(t *testing.T) {
	layout := testRuntime(t)
	installSSHStandIn(t, layout, proxyLoginBin)
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	conn, err := DialProxy(ctx, proxyTestTarget, "", ProxyEvents)
	if err != nil {
		t.Fatal(err)
	}
	if _, readErr := io.ReadAll(conn); readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr := conn.Close(); closeErr == nil {
		t.Fatal("Close after a missing daemon = nil, want the remote exit status")
	}
	if !strings.Contains(conn.Stderr(), errProxyDaemonAbsent.Error()) {
		t.Fatalf("stderr = %q, want the daemon-absent reason from the login-path herrscher", conn.Stderr())
	}
}

func TestDialProxyStderrCanBeReadWhileTheProcessRuns(t *testing.T) {
	layout := testRuntime(t)
	installSSHStandIn(t, layout, proxyTestBin)
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestTimeout)
	defer cancel()
	conn, err := DialProxy(ctx, proxyTestTarget, proxyTestBin, ProxyEvents)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(proxyTestTimeout)
	for !strings.Contains(conn.Stderr(), errProxyDaemonAbsent.Error()) {
		if time.Now().After(deadline) {
			t.Fatalf("stderr never showed the daemon-absent reason, got %q", conn.Stderr())
		}
		time.Sleep(time.Millisecond)
	}
	logCleanup(conn.Close())
}
