package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func hubWith(t *testing.T, path []string, got *contracts.Input) *hub {
	t.Helper()
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	var r cli.Registry
	cmd := contracts.New(path...).Param("name", "", true).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			*got = in
			return contracts.DataResult("ok"), nil
		})
	if err := r.Add(cmd); err != nil {
		t.Fatal(err)
	}
	return newHub(st, sup, nil, t.TempDir(), &r, nil, approvalTab, sessionTab)
}

func TestContributedCommandDoesNotBlockTheHub(t *testing.T) {
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	entered, release := make(chan struct{}), make(chan struct{})
	var r cli.Registry
	slow := contracts.New("plug", "read").Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		close(entered)
		<-release
		return contracts.DataResult("slow"), nil
	})
	quick := contracts.New("session", "list").Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		return contracts.DataResult("quick"), nil
	})
	for _, c := range []contracts.Cmd{slow, quick} {
		if err := r.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	h := newHub(state.NewState(t.TempDir()+"/s.json"),
		supervisor.NewSupervisor(ctx, "/nonexistent/herrscher"), nil, t.TempDir(), &r, nil, approvalTab, sessionTab)
	h.contributedKinds = contributedKinds([]contracts.Cmd{slow})

	go func() { _, dispatchErr := h.Dispatch(ctx, []string{"plug", "read"}); logCleanup(dispatchErr) }()
	<-entered

	done := make(chan string, 1)
	go func() {
		out, err := cli.Plain(h.Dispatch(ctx, []string{"session", "list"}))
		if err != nil {
			t.Errorf("session list: %v", err)
		}
		done <- out
	}()
	select {
	case out := <-done:
		if out != "quick" {
			t.Fatalf("session list = %q", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a built-in blocked behind an in-flight plugin verb")
	}
	close(release)
}

func TestSessionSeedRegisteredRequiresNameAndTask(t *testing.T) {
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Dispatch(context.Background(), []string{"session", "seed", "--name", "solo"}); err == nil || !strings.Contains(err.Error(), "missing required --task") {
		t.Fatal("expected error: missing required --task")
	}
}

func TestHubCreateMapsSpecToTypedInput(t *testing.T) {
	var got contracts.Input
	h := hubWith(t, []string{"session", "create"}, &got)
	if _, err := h.Create(context.Background(), contracts.CreateSession{
		Name: "main", Project: "alpha", Gateways: []string{"chat", "terminal"},
		TerminalOnly: true, Shared: true, Agent: "bishop", ConsolidateEvery: 3, Base: "session/a",
		MemoryProject: "neublox", MemoryAgent: "tui", ProjectPinned: true,
	}); err != nil {
		t.Fatal(err)
	}
	if got.Get("name") != "main" || got.Get("project") != "alpha" || got.Get("agent") != "bishop" {
		t.Fatalf("string fields not mapped: %+v", got.Args)
	}
	if got.Get("base") != "session/a" {
		t.Fatalf("base not mapped: %q", got.Get("base"))
	}
	if got.Get("gateways") != "chat,terminal" {
		t.Fatalf("gateways not joined: %q", got.Get("gateways"))
	}
	if !got.Bool("terminal_only") || !got.Bool("shared") {
		t.Fatalf("bool flags not mapped: %+v", got.Args)
	}
	if got.Get("consolidate_every") != "3" {
		t.Fatalf("consolidate_every not mapped: %q", got.Get("consolidate_every"))
	}
	if got.Get("memory_project") != "neublox" || got.Get("memory_agent") != "tui" {
		t.Fatalf("memory roots not mapped: %+v", got.Args)
	}
	if !got.Bool("project_pinned") {
		t.Fatalf("project_pinned not mapped: %+v", got.Args)
	}
}

func TestHubCreateOmitsUnsetFields(t *testing.T) {
	var got contracts.Input
	h := hubWith(t, []string{"session", "create"}, &got)
	if _, err := h.Create(context.Background(), contracts.CreateSession{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"project", "gateways", "agent", "shared", "terminal_only", "consolidate_every", "base", "memory_project", "memory_agent", "project_pinned"} {
		if _, ok := got.Args[k]; ok {
			t.Fatalf("unset field %q must be absent, got %q", k, got.Args[k])
		}
	}
}

func TestHubCloseMapsNameAndForce(t *testing.T) {
	var got contracts.Input
	h := hubWith(t, []string{"session", "close"}, &got)
	if _, err := h.Close(context.Background(), "demo", true); err != nil {
		t.Fatal(err)
	}
	if got.Get("name") != "demo" || !got.Bool("force") {
		t.Fatalf("close did not map name/force: %+v", got.Args)
	}
}

func TestHubCloseOmitsForceWhenFalse(t *testing.T) {
	var got contracts.Input
	h := hubWith(t, []string{"session", "close"}, &got)
	if _, err := h.Close(context.Background(), "demo", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Args["force"]; ok {
		t.Fatalf("force must be absent when false: %+v", got.Args)
	}
}

func TestHubDispatchSetSource(t *testing.T) {
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	o := Options{StatePath: t.TempDir() + "/s.json", DefaultCmd: "claude"}
	reg, _, err := buildRegistry(ctx, Deps{}, o, st, sup, "", nil, approvalTab, sessionTab)
	if err != nil {
		t.Fatal(err)
	}
	h := newHub(st, sup, nil, t.TempDir(), reg, nil, approvalTab, sessionTab)

	if _, err := h.Dispatch(ctx, []string{"set", "source", "--path", "/src/x"}); err != nil {
		t.Fatal(err)
	}
	if st.SourceDir() != "/src/x" {
		t.Fatalf("dispatch did not persist source: %q", st.SourceDir())
	}
}

func newTestHub(t *testing.T) *hub {
	t.Helper()
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	return newHub(st, sup, nil, t.TempDir(), &cli.Registry{}, nil, approvalTab, sessionTab)
}

func TestHubSessionsSnapshot(t *testing.T) {
	h := newTestHub(t)
	if got := h.Sessions(); len(got) != 0 {
		t.Fatalf("empty hub should report no sessions, got %+v", got)
	}
	if err := h.st.AddSession(state.Session{Name: "demo", ChannelID: "c1", Type: "text", Gateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	got := h.Sessions()
	if len(got) != 1 || got[0].Name != "demo" || got[0].ChannelID != "c1" || got[0].Type != "text" {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	persisted, ok := h.st.FindSession("demo")
	if !ok || persisted.Incarnation == "" || got[0].Incarnation != persisted.Incarnation {
		t.Fatalf("incarnation not surfaced: persisted=%+v snapshot=%+v", persisted, got[0])
	}
	if len(got[0].Gateways) != 1 || got[0].Gateways[0] != "chat" {
		t.Fatalf("gateways not surfaced: %+v", got[0].Gateways)
	}
}

func TestHubReconcileGoLiveGoDead(t *testing.T) {
	h := newTestHub(t)
	if err := h.st.AddSession(state.Session{Name: "live1", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t.Context())
	h.mu.Lock()
	_, up := h.live["live1"]
	h.mu.Unlock()
	if !up {
		t.Fatal("reconcile did not bring the persisted session live")
	}

	h.reconcile(t.Context())
	h.mu.Lock()
	n := len(h.live)
	h.mu.Unlock()
	if n != 1 {
		t.Fatalf("reconcile not idempotent; live=%d", n)
	}

	if err := h.st.RemoveSession("live1"); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t.Context())
	h.mu.Lock()
	_, stillUp := h.live["live1"]
	h.mu.Unlock()
	if stillUp {
		t.Fatal("reconcile did not tear down the removed session")
	}
}

type stubForgetCoord struct{ forgotten []string }

func (s *stubForgetCoord) Handoff(context.Context, contracts.HandoffRequest) (string, error) {
	return "", nil
}

func (s *stubForgetCoord) Delegate(context.Context, contracts.DelegateRequest) (string, error) {
	return "", nil
}

func (s *stubForgetCoord) Report(context.Context, contracts.ReportRequest) (string, error) {
	return "", nil
}

func (s *stubForgetCoord) Merge(context.Context, contracts.MergeRequest) (string, error) {
	return "", nil
}

func (s *stubForgetCoord) Seal(context.Context, contracts.SealRequest) (string, error) {
	return "", nil
}

func (s *stubForgetCoord) FanOut(context.Context, contracts.FanOutRequest) ([]string, error) {
	return nil, nil
}

func (s *stubForgetCoord) Route(context.Context, contracts.RouteRequest) (string, string, error) {
	return "", "", nil
}
func (s *stubForgetCoord) forget(name string) { s.forgotten = append(s.forgotten, name) }

func TestGoDeadCallsForget(t *testing.T) {
	stub := &stubForgetCoord{}
	h := &hub{
		coordinator:   stub,
		live:          make(map[string]liveSession),
		teardownGrace: defaultTeardownGrace,
	}
	h.goDead("worker")
	if len(stub.forgotten) != 1 || stub.forgotten[0] != "worker" {
		t.Fatalf("goDead devrait appeler forget(\"worker\"): %v", stub.forgotten)
	}
}

func TestHubStillSatisfiesSessionControl(_ *testing.T) {
	var _ contracts.SessionControl = (*hub)(nil)
}

func TestReposListsLocalCheckoutsOnly(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "myproj", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "notrepo"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := workspaceRepos(ws)
	if len(got) != 1 || got[0].Name != "myproj" || !got[0].Local {
		t.Fatalf("workspaceRepos(%q) = %+v, want one local myproj", ws, got)
	}
}

func TestReposOnAnUnsetWorkspaceIsEmpty(t *testing.T) {
	if got := workspaceRepos(""); got != nil {
		t.Fatalf("workspaceRepos(\"\") = %+v, want nil", got)
	}
}

func TestHubSessionsReportTheSessionLanguage(t *testing.T) {
	h := newTestHub(t)
	if err := h.st.AddSession(state.Session{Name: "fr", ChannelID: "c1", Language: "fr"}); err != nil {
		t.Fatal(err)
	}
	if err := h.st.AddSession(state.Session{Name: "unset", ChannelID: "c2"}); err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]contracts.SessionInfo)
	for _, info := range h.Sessions() {
		byName[info.Name] = info
	}
	if got := byName["fr"].Language; got != i18n.French {
		t.Fatalf("session fr Language = %v, want %v", got, i18n.French)
	}
	if got := byName["unset"].Language; got != 0 {
		t.Fatalf("session without a language Language = %v, want zero", got)
	}
}
