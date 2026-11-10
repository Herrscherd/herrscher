package supervisor

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
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

func TestRestartWaitsForOldBridgeAndStartsTargetedSession(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	oldStarted := make(chan struct{})
	allowOldExit := make(chan struct{})
	oldExited := make(chan struct{})
	newStarted := make(chan state.Session, 1)
	s.runBridge = func(ctx context.Context, sess state.Session) {
		if sess.Cmd == "claude" {
			close(oldStarted)
			<-ctx.Done()
			<-allowOldExit
			close(oldExited)
			return
		}
		newStarted <- sess
		<-ctx.Done()
	}

	s.Start(state.Session{Name: "worker", Vendor: "claude", Cmd: "claude"})
	<-oldStarted

	restarted := make(chan error, 1)
	go func() {
		restarted <- s.Restart(state.Session{
			Name: "worker", Vendor: "codex", Cmd: "codex --model gpt-5.6-terra",
		})
	}()

	select {
	case got := <-newStarted:
		t.Fatalf("new bridge started before old bridge exited: %+v", got)
	case err := <-restarted:
		t.Fatalf("Restart returned before old bridge exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(allowOldExit)
	if err := <-restarted; err != nil {
		t.Fatalf("Restart: %v", err)
	}
	select {
	case <-oldExited:
	default:
		t.Fatal("Restart returned while the old bridge was still active")
	}
	select {
	case got := <-newStarted:
		if got.Vendor != "codex" || got.Cmd != "codex --model gpt-5.6-terra" {
			t.Fatalf("replacement bridge target = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement bridge did not start")
	}
	if err := s.Stop("worker"); err != nil {
		t.Fatal(err)
	}
}

func TestStopWaitsForRunLoopCompletionWithoutHoldingMutex(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	replacementStarted := make(chan struct{})
	var runCount atomic.Int32
	s.run = func(ctx context.Context, _ state.Session) {
		switch runCount.Add(1) {
		case 1:
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
		case 2:
			close(replacementStarted)
			<-ctx.Done()
		default:
			panic("unexpected extra supervised run")
		}
	}

	s.Start(state.Session{Name: "demo"})
	<-started

	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop("demo") }()
	<-cancelled

	select {
	case err := <-stopped:
		t.Fatalf("Stop returned before runLoop completed: %v", err)
	default:
	}

	otherStopped := make(chan error, 1)
	go func() { otherStopped <- s.Stop("other") }()
	select {
	case err := <-otherStopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop held supervisor.mu while waiting for runLoop")
	}

	startReturned := make(chan struct{})
	go func() {
		s.Start(state.Session{Name: "demo"})
		close(startReturned)
	}()
	select {
	case <-startReturned:
		t.Fatal("Start returned while the previous run was still stopping")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after runLoop completed")
	}
	select {
	case <-startReturned:
	case <-time.After(time.Second):
		t.Fatal("Start did not return after the previous run completed")
	}
	select {
	case <-replacementStarted:
	case <-time.After(time.Second):
		t.Fatal("Start did not launch a replacement run")
	}
	if err := s.Stop("demo"); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeArgsIncludeSession(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1"})
	if !strings.Contains(strings.Join(args, " "), "--session demo") {
		t.Fatalf("expected --session <name> in args: %v", args)
	}
}

func TestBridgeArgsIncludeBackend(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1", Backend: "oneshot"})
	if !strings.Contains(strings.Join(args, " "), "--backend oneshot") {
		t.Fatalf("expected --backend oneshot in args: %v", args)
	}
}

func TestBridgeArgsThreadsVendor(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	args := s.bridgeArgs(state.Session{Name: "w", ChannelID: "c", Vendor: "codex"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--vendor codex") {
		t.Fatalf("args missing --vendor codex: %v", args)
	}

	args = s.bridgeArgs(state.Session{Name: "w", ChannelID: "c"})
	if strings.Contains(strings.Join(args, " "), "--vendor") {
		t.Fatalf("--vendor present for empty vendor: %v", args)
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

func TestBridgeArgsThreadsLearningConfig(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	args := s.bridgeArgs(state.Session{
		Name: "demo", ChannelID: "c1",
		Extractor: "roblox", Journal: ".neublox/calls.log", ConsolidateEvery: 5,
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--extractor roblox") {
		t.Fatalf("expected --extractor for the learning loop: %v", args)
	}
	if !strings.Contains(joined, "--journal .neublox/calls.log") {
		t.Fatalf("expected --journal for the consolidation input: %v", args)
	}
	if !strings.Contains(joined, "--consolidate-every 5") {
		t.Fatalf("expected --consolidate-every for the cadence: %v", args)
	}
}

func TestBridgeArgsOmitsLearningConfigWhenUnset(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--extractor") || strings.Contains(joined, "--journal") ||
		strings.Contains(joined, "--consolidate-every") {
		t.Fatalf("no learning flags expected when extractor/journal/cadence unset: %v", args)
	}
}

func TestRunLoopLogsRestartAsStructuredWarn(t *testing.T) {
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := NewSupervisor(ctx, "/herrscher/does-not-exist")
	s.SetLogger(obs.NewLogger(&buf, slog.LevelDebug))
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

func captureDelays(t *testing.T, wantN int, now func() time.Time) []time.Duration {
	t.Helper()
	var delays []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewSupervisor(ctx, "/herrscher/does-not-exist")
	s.SetLogger(obs.NewLogger(io.Discard, slog.LevelInfo))
	s.now = now
	s.sleep = func(d time.Duration) <-chan time.Time {
		delays = append(delays, d)
		if len(delays) >= wantN {
			cancel()
		}
		ch := make(chan time.Time, 1)
		ch <- time.Time{}
		return ch
	}
	s.runLoop(ctx, state.Session{Name: "demo"})
	return delays
}

func TestRunLoopCountsBridgeRestart(t *testing.T) {
	var delays []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewSupervisor(ctx, "/herrscher/does-not-exist")
	s.SetLogger(obs.NewLogger(io.Discard, slog.LevelInfo))
	m := metrics.NewRegistry()
	s.SetMetrics(m)
	s.sleep = func(time.Duration) <-chan time.Time {
		delays = append(delays, 0)
		cancel()
		ch := make(chan time.Time, 1)
		ch <- time.Time{}
		return ch
	}
	s.runLoop(ctx, state.Session{Name: "demo"})
	if got := m.Snapshot().BridgeRestarts; got != 1 {
		t.Fatalf("bridge restarts = %d, want 1", got)
	}
}

func TestRunLoopBacksOffGeometrically(t *testing.T) {
	frozen := time.Unix(0, 0)
	delays := captureDelays(t, 4, func() time.Time { return frozen })
	if len(delays) < 4 {
		t.Fatalf("expected at least 4 restart delays, got %v", delays)
	}
	pol := obs.RestartBackoff()
	for i := 1; i < len(delays); i++ {
		if delays[i-1] < pol.Max && delays[i] <= delays[i-1] {
			t.Fatalf("delay did not grow at %d: %v", i, delays)
		}
	}
}

func TestRunLoopResetsBackoffAfterHealthyRun(t *testing.T) {
	pol := obs.RestartBackoff()
	tick := time.Unix(0, 0)
	delays := captureDelays(t, 3, func() time.Time {
		tick = tick.Add(2 * pol.Reset)
		return tick
	})
	floor := time.Duration(float64(pol.Base) * (1 - pol.Jitter))
	for i, d := range delays {
		if d < floor || d > pol.Base {
			t.Fatalf("delay %d = %v, want within [%v, %v] (reset to base each time)", i, d, floor, pol.Base)
		}
	}
}

func TestBridgeArgsNoBackendWhenStream(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	for _, b := range []string{"", "stream"} {
		args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1", Backend: b})
		if strings.Contains(strings.Join(args, " "), "--backend") {
			t.Fatalf("no --backend expected for backend %q: %v", b, args)
		}
	}
}

func TestBridgeArgsThreadsModelID(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	args := s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1", ModelID: "gw-claude-opus-5"})
	if !strings.Contains(strings.Join(args, " "), "--model gw-claude-opus-5") {
		t.Fatalf("expected --model to carry the catalog model id: %v", args)
	}

	args = s.bridgeArgs(state.Session{Name: "demo", ChannelID: "c1"})
	if strings.Contains(strings.Join(args, " "), "--model") {
		t.Fatalf("--model present for a legacy session with no model: %v", args)
	}
}

func TestBridgeArgsPrefersTheMemoryRoots(t *testing.T) {
	s := &Supervisor{}
	joined := strings.Join(s.bridgeArgs(state.Session{
		Name: "demo", MemoryProject: "neublox", MemoryAgent: "tui", ProjectPinned: true,
	}), " ")
	for _, want := range []string{"--project neublox", "--agent tui", "--project-pinned"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
}

func TestBridgeArgsKeepsThePlacementFieldsWhenNoMemoryRootIsSet(t *testing.T) {
	s := &Supervisor{}
	joined := strings.Join(s.bridgeArgs(state.Session{Name: "demo", Project: "game", Agent: "scout"}), " ")
	if !strings.Contains(joined, "--project game") || !strings.Contains(joined, "--agent scout") {
		t.Fatalf("legacy scope flags changed: %s", joined)
	}
	if strings.Contains(joined, "--project-pinned") {
		t.Fatalf("nothing pinned this: %s", joined)
	}
}

func TestMemoryAgentBeatsTheProvisionedAgent(t *testing.T) {
	got := strings.Join((&Supervisor{}).bridgeArgs(state.Session{Name: "s", Agent: "bob", MemoryAgent: "tui"}), " ")
	if !strings.Contains(got, "--agent tui") {
		t.Fatalf("args = %q, want the memory agent", got)
	}
}

func TestBridgeCarriesTheSessionName(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude"})
	if err != nil {
		t.Fatalf("bridgeCommand: %v", err)
	}
	var found bool
	for _, kv := range cmd.Env {
		if kv == "HERRSCHER_SESSION=s1" {
			found = true
		}
	}
	if !found {
		t.Fatal("a bridge must know which session it is, or its hooks cannot say")
	}
}

func TestBridgeIsGivenItsOwnSessionSocket(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	s.SetCommandSocket(func(session string) string { return "/tmp/herrscher-command-alt-s-" + session + ".sock" })
	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude"})
	if err != nil {
		t.Fatalf("bridgeCommand: %v", err)
	}
	var got string
	for _, kv := range cmd.Env {
		if v, ok := strings.CutPrefix(kv, control.CommandSocketVar+"="); ok {
			got = v
		}
	}
	if got != "/tmp/herrscher-command-alt-s-s1.sock" {
		t.Fatalf("%s = %q, want the socket this daemon serves for this session", control.CommandSocketVar, got)
	}
}

func TestRemoteForwardPointsAtTheSessionSocket(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	s.SetCommandSocket(func(session string) string { return "/tmp/sock-" + session })
	var found bool
	for _, f := range s.forwardsFor(state.Session{Name: "revue", Host: "build1"}) {
		if f.Remote != control.RemoteCommandSocketPath("", "revue") {
			continue
		}
		found = true
		if f.Local != "/tmp/sock-revue" {
			t.Fatalf("forward local = %q, want the session socket", f.Local)
		}
	}
	if !found {
		t.Fatal("no command-socket forward for the session")
	}
}

func TestBridgeOmitsAnUnknownCommandSocket(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/herrscher")
	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude"})
	if err != nil {
		t.Fatalf("bridgeCommand: %v", err)
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, control.CommandSocketVar+"=") {
			t.Fatalf("env names a socket nobody set: %q", kv)
		}
	}
	for _, f := range s.forwardsFor(state.Session{Name: "s1", Host: "build1"}) {
		if f.Remote == control.RemoteCommandSocketPath("", "s1") {
			t.Fatal("forwarding a socket nobody set")
		}
	}
}
