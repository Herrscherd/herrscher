package tui

import (
	"strings"
	"testing"
)

func TestShellCommandParsing(t *testing.T) {
	if got, ok := shellCommand("!ls -la"); !ok || got != "ls -la" {
		t.Fatalf("shellCommand = %q, %v", got, ok)
	}
	if _, ok := shellCommand("!"); ok {
		t.Fatal("a bare ! is not a command")
	}
	if _, ok := shellCommand("ls"); ok {
		t.Fatal("a line without ! is not a shell command")
	}
}

func TestBangLineDispatchesShellThroughSession(t *testing.T) {
	m, f := queueFixture(t)
	typeLine(m, "!git status")
	runCmd(m.handleEnter())
	if len(f.dispatched) != 1 {
		t.Fatalf("! line must dispatch once: %+v", f.dispatched)
	}
	args := strings.Join(f.dispatched[0], " ")
	if !strings.Contains(args, "session shell") || !strings.Contains(args, "--cmd git status") {
		t.Fatalf("dispatch args = %q", args)
	}
	if len(f.submitted) != 0 {
		t.Fatal("a ! line must not be sent as a message")
	}
}

func TestShellResultEntersSessionContextOnce(t *testing.T) {
	m, _ := queueFixture(t)
	tb := m.tabs["a"]
	tb.shellRunning = true
	m.handleShellResult(shellResultMsg{channel: "a", command: "ls", out: "a.go"})
	if tb.shellRunning {
		t.Fatal("result must clear shellRunning")
	}
	got := tb.withShellContext("suite")
	if !strings.Contains(got, "$ ls\na.go") || !strings.HasSuffix(got, "suite") {
		t.Fatalf("next turn must carry the output: %q", got)
	}
	if again := tb.withShellContext("autre"); again != "autre" {
		t.Fatalf("context must be consumed once: %q", again)
	}
}

func TestShellErrorIsShownWithoutContext(t *testing.T) {
	m, _ := queueFixture(t)
	tb := m.tabs["a"]
	m.handleShellResult(shellResultMsg{channel: "a", command: "rm", err: errShellTest{}})
	if tb.shellContext != "" {
		t.Fatal("a failed command must not enter the session context")
	}
}

type errShellTest struct{}

func (errShellTest) Error() string { return "refusé" }
