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
	"sync"
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
	daemon := startTUIDaemon(t, "")
	window, _ := attachTUIWindow(t, daemon)

	window.Type("hello daemon")
	window.Send(tea.KeyMsg{Type: tea.KeyEnter})

	awaitTUIScreen(t, window, "echo: hello daemon")
}

func TestE2EAttachedWindowRereadsHistory(t *testing.T) {
	daemon := startTUIDaemon(t, "text answer one\n---\ntext answer two\n")
	first, stopFirst := attachTUIWindow(t, daemon)

	first.Type("first question")
	first.Send(tea.KeyMsg{Type: tea.KeyEnter})
	awaitTUIScreen(t, first, "answer one")

	first.Type("second question")
	first.Send(tea.KeyMsg{Type: tea.KeyEnter})
	awaitTUIScreen(t, first, "answer two")

	first.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	first.WaitFinished(t, teatest.WithFinalTimeout(tuiScreenWait))
	stopFirst()

	reopened, _ := attachTUIWindow(t, daemon)
	awaitTUIScreen(t, reopened, onlySessionName(t, daemon), "first question", "answer one", "second question", "answer two")
}

type tuiSetup struct {
	binDir     string
	root       string
	home       string
	repo       string
	scriptPath string
}

func newTUISetup(t *testing.T, vendorScript string) tuiSetup {
	t.Helper()
	setup := tuiSetup{binDir: t.TempDir(), root: shortRuntimeRoot(t), home: t.TempDir(), repo: t.TempDir()}
	buildTUIBinary(t, filepath.Join(setup.binDir, "claude"), "./e2e/vendortest/cmd/claude")
	buildTUIBinary(t, filepath.Join(setup.binDir, "herrscher"), "-tags", "e2e", ".")
	if err := os.Mkdir(filepath.Join(setup.root, strconv.Itoa(os.Getuid())), 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", setup.repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if vendorScript != "" {
		setup.scriptPath = filepath.Join(setup.home, "claude.script")
		if err := os.WriteFile(setup.scriptPath, []byte(vendorScript), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range tuiEnv(setup.home, setup.root, setup.binDir, setup.scriptPath) {
		name, value, _ := strings.Cut(pair, "=")
		t.Setenv(name, value)
	}
	t.Chdir(setup.repo)
	return setup
}

func shortRuntimeRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func startTUIDaemon(t *testing.T, vendorScript string) tuiDaemon {
	t.Helper()
	daemon, stop := newTUISetup(t, vendorScript).serve(t)
	t.Cleanup(stop)
	return daemon
}

func (setup tuiSetup) serve(t *testing.T) (tuiDaemon, func()) {
	t.Helper()
	layout, err := host.RuntimeAt(filepath.Join(setup.root, strconv.Itoa(os.Getuid()), "herrscher"))
	if err != nil {
		t.Fatal(err)
	}
	env := tuiEnv(setup.home, setup.root, setup.binDir, setup.scriptPath)
	daemon := exec.CommandContext(context.Background(), filepath.Join(setup.binDir, "herrscher"), "serve")
	daemon.Dir = setup.repo
	daemon.Env = env
	var stderr bytes.Buffer
	daemon.Stderr = &stderr
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- daemon.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() { stopDaemon(t, daemon, stopped, &stderr) })
	}
	waitTUIDaemon(t, layout, tuiInstance)
	return tuiDaemon{layout: layout, instance: tuiInstance, statePath: host.DefaultStatePath()}, stop
}

func tuiEnv(home, root, binDir, scriptPath string) []string {
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
		"VENDORTEST_SCRIPT=" + scriptPath,
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
	backend, err := newAttachedDaemon(ctx, daemon.layout, testMessages(), host.NewMessages(), daemon.instance, daemon.statePath, i18n.English)
	if err != nil {
		t.Fatal(err)
	}
	sessions := backend.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("want one session on the daemon, got %d", len(sessions))
	}
	return sessions[0].Name
}

func attachTUIWindow(t *testing.T, daemon tuiDaemon) (*teatest.TestModel, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	backend, err := newAttachedDaemon(ctx, daemon.layout, testMessages(), host.NewMessages(), daemon.instance, daemon.statePath, i18n.English)
	if err != nil {
		t.Fatal(err)
	}
	window := tui.NewWindow(ctx, backend.messages, backend, tui.Language(backend.lang))
	model := teatest.NewTestModel(t, window, teatest.WithInitialTermSize(120, 40))
	go tui.Pump(ctx, backend, model.Send)
	return model, cancel
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
