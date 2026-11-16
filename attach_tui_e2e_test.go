//go:build e2e && linux

package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

const (
	tuiDaemonWait = 30 * time.Second
	tuiScreenWait = 20 * time.Second
	tuiStopWait   = 15 * time.Second
	tuiInstance   = "e2e-tui"
)

type tuiDaemon struct {
	layout    host.RuntimeLayout
	instance  string
	statePath string
}

func TestE2EAttachedWindowFirstMessage(t *testing.T) {
	daemon := startTUIDaemon(t)
	window := attachTUIWindow(t, daemon)

	window.Type("hello daemon")
	window.Send(tea.KeyMsg{Type: tea.KeyEnter})

	awaitTUIScreen(t, window, "echo: hello daemon")
}

func TestE2EAttachedWindowRereadsHistory(t *testing.T) {
	daemon := startTUIDaemon(t)
	first := attachTUIWindow(t, daemon)

	first.Type("hello daemon")
	first.Send(tea.KeyMsg{Type: tea.KeyEnter})
	awaitTUIScreen(t, first, "echo: hello daemon")

	first.Type("second turn")
	first.Send(tea.KeyMsg{Type: tea.KeyEnter})
	awaitTUIScreen(t, first, "echo: second turn")

	first.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	first.WaitFinished(t, teatest.WithFinalTimeout(tuiScreenWait))

	reopened := attachTUIWindow(t, daemon, tui.OpenOn(onlySessionName(t, daemon), ""))
	awaitTUIScreen(t, reopened, "hello daemon", "echo: hello daemon", "second turn", "echo: second turn")
}

func startTUIDaemon(t *testing.T) tuiDaemon {
	t.Helper()
	binDir := t.TempDir()
	buildTUIBinary(t, filepath.Join(binDir, "claude"), "./e2e/vendortest/cmd/claude")
	buildTUIBinary(t, filepath.Join(binDir, "herrscher"), "-tags", "e2e", ".")

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, strconv.Itoa(os.Getuid())), 0o700); err != nil {
		t.Fatal(err)
	}
	layout, err := host.RuntimeAt(filepath.Join(root, strconv.Itoa(os.Getuid()), "herrscher"))
	if err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	repo := t.TempDir()
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	env := tuiEnv(home, root, binDir)
	for _, pair := range env {
		name, value, _ := strings.Cut(pair, "=")
		t.Setenv(name, value)
	}
	t.Chdir(repo)

	daemon := exec.CommandContext(context.Background(), filepath.Join(binDir, "herrscher"), "serve")
	daemon.Dir = repo
	daemon.Env = env
	var stderr bytes.Buffer
	daemon.Stderr = &stderr
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- daemon.Wait() }()
	t.Cleanup(func() {
		stopDaemon(t, daemon, stopped, &stderr)
	})

	waitTUIDaemon(t, layout, tuiInstance)
	return tuiDaemon{layout: layout, instance: tuiInstance, statePath: host.DefaultStatePath()}
}

func tuiEnv(home, root, binDir string) []string {
	return []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_RUNTIME_DIR=" + root,
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"HERRSCHER_INSTANCE_ID=" + tuiInstance,
		"HERRSCHER_E2E_RUNTIME_ROOT=" + root,
		"HERRSCHER_MODELS_DISCOVER=none",
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.invalid",
		"GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.invalid",
		"VENDORTEST_SCRIPT=",
		"VENDORTEST_LOG=" + filepath.Join(home, "claude.log"),
		"VENDORTEST_TURNS=" + filepath.Join(home, "claude.turns"),
	}
}

func buildTUIBinary(t *testing.T, out string, args ...string) {
	t.Helper()
	build := exec.CommandContext(t.Context(), "go", append([]string{"build", "-o", out}, args...)...)
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build %v: %v\n%s", args, err, output)
	}
}

func waitTUIDaemon(t *testing.T, layout host.RuntimeLayout, instance string) {
	t.Helper()
	socket := host.CommandSocketPathIn(layout, instance)
	deadline := time.Now().Add(tuiDaemonWait)
	for {
		dialer := net.Dialer{Timeout: time.Second}
		conn, err := dialer.DialContext(t.Context(), "unix", socket)
		if err == nil {
			closeQuietly(conn)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no daemon answered on %s within %s: %v", socket, tuiDaemonWait, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func stopDaemon(t *testing.T, daemon *exec.Cmd, stopped <-chan error, stderr *bytes.Buffer) {
	t.Helper()
	if err := daemon.Process.Signal(os.Interrupt); err != nil {
		t.Errorf("interrupt the daemon: %v", err)
	}
	select {
	case err := <-stopped:
		if err != nil && !exitedAfterInterrupt(err) {
			t.Errorf("the daemon did not exit cleanly: %v", err)
		}
	case <-time.After(tuiStopWait):
		if err := daemon.Process.Kill(); err != nil {
			t.Errorf("kill the daemon: %v", err)
		}
		t.Errorf("the daemon ignored the interrupt for %s", tuiStopWait)
	}
	if t.Failed() {
		t.Logf("daemon stderr:\n%s", stderr.String())
	}
}

func exitedAfterInterrupt(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == exitInterrupted
}

func onlySessionName(t *testing.T, daemon tuiDaemon) string {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	backend, err := newAttachedDaemon(ctx, daemon.layout, daemon.instance, daemon.statePath, i18n.English)
	if err != nil {
		t.Fatal(err)
	}
	sessions := backend.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("want one session on the daemon, got %d", len(sessions))
	}
	return sessions[0].Name
}

func attachTUIWindow(t *testing.T, daemon tuiDaemon, opts ...tui.Option) *teatest.TestModel {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	backend, err := newAttachedDaemon(ctx, daemon.layout, daemon.instance, daemon.statePath, i18n.English)
	if err != nil {
		t.Fatal(err)
	}
	opts = append([]tui.Option{tui.Language(backend.lang)}, opts...)
	window := tui.NewWindow(ctx, backend, opts...)
	model := teatest.NewTestModel(t, window, teatest.WithInitialTermSize(120, 40))
	go relayTUIEvents(ctx, backend.Frontend(), model)
	return model
}

func relayTUIEvents(ctx context.Context, events <-chan tui.RoutedEvent, model *teatest.TestModel) {
	for {
		select {
		case <-ctx.Done():
			return
		case routed, ok := <-events:
			if !ok {
				return
			}
			model.Send(tui.Event(routed))
		}
	}
}

func awaitTUIScreen(t *testing.T, window *teatest.TestModel, want ...string) {
	t.Helper()
	teatest.WaitFor(t, window.Output(), func(out []byte) bool {
		screen := ansi.Strip(string(out))
		for _, text := range want {
			if !strings.Contains(screen, text) {
				return false
			}
		}
		return true
	}, teatest.WithDuration(tuiScreenWait), teatest.WithCheckInterval(50*time.Millisecond))
}
