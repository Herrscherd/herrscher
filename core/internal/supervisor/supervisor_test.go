package supervisor

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestBridgeArgsIncludesHubSocket(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1", Cmd: "claude"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--hub-socket") {
		t.Fatalf("bridgeArgs missing --hub-socket: %v", args)
	}
}

func TestBridgeArgsIncludeSession(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/dctl")
	args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1"})
	if !strings.Contains(strings.Join(args, " "), "--session demo") {
		t.Fatalf("expected --session <name> in args: %v", args)
	}
}

func TestBridgeArgsIncludeBackend(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/dctl")
	args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1", Backend: "oneshot"})
	if !strings.Contains(strings.Join(args, " "), "--backend oneshot") {
		t.Fatalf("expected --backend oneshot in args: %v", args)
	}
}

func TestBridgeArgsThreadsMemoryScope(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1", Project: "obby", Agent: "roblox"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--project obby") {
		t.Fatalf("expected --project for the shared memory scope: %v", args)
	}
	if !strings.Contains(joined, "--agent roblox") {
		t.Fatalf("expected --agent for the private memory scope: %v", args)
	}
}

func TestBridgeArgsOmitsScopeWhenUnset(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--project") || strings.Contains(joined, "--agent") {
		t.Fatalf("no scope flags expected when project/agent unset: %v", args)
	}
}

// TestRunLoopLogsRestartAsStructuredWarn drives one crash-restart cycle and
// asserts the restart line is a structured slog record (level=warn, session
// field) routed through the injected logger — not a raw fmt.Fprintf string.
func TestRunLoopLogsRestartAsStructuredWarn(t *testing.T) {
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A binary that cannot start makes cmd.Run() return immediately, exercising
	// the restart path without spawning a real bridge.
	s := NewSupervisor(ctx, "/herrscher/does-not-exist")
	s.SetLogger(obs.NewLogger(&buf, slog.LevelDebug))
	// Cancel on the first backoff sleep so the loop logs exactly once then exits.
	s.sleep = func(time.Duration) <-chan time.Time {
		cancel()
		ch := make(chan time.Time, 1)
		ch <- time.Time{}
		return ch
	}

	s.runLoop(ctx, state.Session{Name: "demo"})

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("expected a warn-level restart record, got %q", out)
	}
	if !strings.Contains(out, "session=demo") {
		t.Fatalf("expected a session field on the restart record, got %q", out)
	}
}

func TestBridgeArgsNoBackendWhenStream(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/dctl")
	for _, b := range []string{"", "stream"} {
		args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1", Backend: b})
		if strings.Contains(strings.Join(args, " "), "--backend") {
			t.Fatalf("no --backend expected for backend %q: %v", b, args)
		}
	}
}
