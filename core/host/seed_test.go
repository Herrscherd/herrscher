package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSelectBackendVendorPrecedence(t *testing.T) {
	plugins := []contracts.Plugin{
		{Manifest: contracts.Manifest{Kind: "claude"}, Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) { return seedBackend{}, nil }},
		{Manifest: contracts.Manifest{Kind: "codex"}, Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) { return seedBackend{}, nil }},
		{Manifest: contracts.Manifest{Kind: "cursor"}, Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) { return seedBackend{}, nil }},
	}
	tests := []struct {
		name    string
		session string
		env     string
		want    string
		wantErr string
	}{
		{name: "explicit beats env", session: "cursor", env: "codex", want: "cursor"},
		{name: "env beats fallback", env: "codex", want: "codex"},
		{name: "fallback is first", want: "claude"},
		{name: "unknown is an error", session: "gemini", env: "codex", wantErr: `unknown backend "gemini"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "" {
				_ = os.Unsetenv("HERRSCHER_BACKEND")
			} else {
				t.Setenv("HERRSCHER_BACKEND", tt.env)
			}
			desired := tt.session
			if desired == "" {
				desired = os.Getenv("HERRSCHER_BACKEND")
			}
			plugin, err := selectBackend(desired, plugins)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("selectBackend error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plugin.Manifest.Kind != tt.want {
				t.Fatalf("selected kind = %q, want %q", plugin.Manifest.Kind, tt.want)
			}
		})
	}
}

func TestBuildBackendForSelectsByVendor(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}

	var built string
	makePlugin := func(kind string) contracts.Plugin {
		return contracts.Plugin{
			Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryBackend},
			Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
				built = kind
				return seedBackend{}, nil
			},
		}
	}
	contracts.Default.Register(makePlugin("claude"))
	contracts.Default.Register(makePlugin("codex"))

	if _, err := BuildBackendFor(context.Background(), BackendRequest{Vendor: "codex", Cmd: "codex --model gpt-5.6"}); err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	if built != "codex" {
		t.Fatalf("built %q, want codex", built)
	}
}

func TestNewSeedBackendResolvesRelativeWorktreeOnce(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}

	relative := filepath.Join(".herrscher-sessions", "instance", "worker")
	worker := filepath.Join(t.TempDir(), relative)
	if err := os.MkdirAll(worker, 0o755); err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(worker); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	var gotDir string
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "codex", Category: contracts.CategoryBackend},
		Backend: func(_ context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			gotDir = cfg.Settings["dir"]
			return seedBackend{}, nil
		},
	})

	sess := state.Session{Vendor: "codex", Worktree: relative}
	if _, err := newSeedBackend(context.Background(), sess); err != nil {
		t.Fatalf("newSeedBackend: %v", err)
	}
	if gotDir != worker {
		t.Fatalf("backend dir = %q, want already-resolved absolute cwd %q", gotDir, worker)
	}
}

type seedSpyOrchestrator struct {
	consolidated bool
	closed       bool
}

func (s *seedSpyOrchestrator) Context(context.Context) string                          { return "" }
func (s *seedSpyOrchestrator) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (s *seedSpyOrchestrator) Consolidate(context.Context) error {
	s.consolidated = true
	return nil
}
func (s *seedSpyOrchestrator) Close() error {
	s.closed = true
	return nil
}

func TestRunOneShotSeedWithConsolidatesAndCloses(t *testing.T) {
	old := oneShotBackendFactory
	t.Cleanup(func() { oneShotBackendFactory = old })
	oneShotBackendFactory = func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{}, nil
	}

	sess := state.Session{Name: "solo", ChannelID: "channel"}
	spy := &seedSpyOrchestrator{}
	if _, err := runOneShotSeedWith(context.Background(), sess, "tâche", spy); err != nil {
		t.Fatalf("runOneShotSeedWith: %v", err)
	}
	if !spy.consolidated {
		t.Fatal("expected consolidation after the one-shot turn")
	}
	if !spy.closed {
		t.Fatal("expected orchestrator close after the one-shot turn")
	}
}

func TestRunOneShotSeedWithCoordinatesDelegateTrailer(t *testing.T) {
	old := oneShotBackendFactory
	t.Cleanup(func() { oneShotBackendFactory = old })
	oneShotBackendFactory = func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "travail préparé\n⟢ delegate: roblox-scripter — inspection en lecture seule"}, nil
	}

	sess := state.Session{Name: "orchestrator", ChannelID: "channel"}
	coord := &recordingCoord{}
	if _, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "délègue", newTurnID(), nil, oneShotSeedRuntime{coordinator: coord}); err != nil {
		t.Fatalf("runOneShotSeedWithIDRuntime: %v", err)
	}
	if len(coord.delegates) != 1 {
		t.Fatalf("delegate calls = %d, want 1", len(coord.delegates))
	}
	want := contracts.DelegateRequest{
		FromSession: "orchestrator",
		ToAgent:     "roblox-scripter",
		Task:        "inspection en lecture seule",
	}
	if got := coord.delegates[0]; got != want {
		t.Fatalf("delegate request = %+v, want %+v", got, want)
	}
}

func TestRunOneShotSeedCommandForwardsToLiveCoordinator(t *testing.T) {
	var gotPath string
	var gotArgv []string
	forward := func(_ context.Context, path string, argv []string) (string, bool, error) {
		gotPath = path
		gotArgv = append([]string(nil), argv...)
		return "réponse live", true, nil
	}

	got, err := runOneShotSeedCommand(
		context.Background(), nil, "orchestrator", "lecture seule",
		"turn-test", oneShotSeedRuntime{}, nil, "instance-test", forward,
	)
	if err != nil {
		t.Fatalf("runOneShotSeedCommand: %v", err)
	}
	if got != "réponse live" {
		t.Fatalf("reply = %q, want %q", got, "réponse live")
	}
	if want := CommandSocketPath("instance-test"); gotPath != want {
		t.Fatalf("command path = %q, want %q", gotPath, want)
	}
	wantArgv := []string{"session", "seed", "--name", "orchestrator", "--task", "lecture seule", "--turn_id", "turn-test"}
	if len(gotArgv) != len(wantArgv) {
		t.Fatalf("argv = %v, want %v", gotArgv, wantArgv)
	}
	for i := range wantArgv {
		if gotArgv[i] != wantArgv[i] {
			t.Fatalf("argv = %v, want %v", gotArgv, wantArgv)
		}
	}
}

// TestNewSeedBackendPropagatesSessionModelID pins the connective line in
// newSeedBackend that copies sess.ModelID into BackendRequest.ModelID. A
// session naming a gateway-route model must reach BuildBackendFor's
// credential-resolution path and, with no gateway credentials in the
// environment, fail closed with the gateway error — proving the ID actually
// arrived, not just that some error occurred. If that line were reverted to
// ModelID: "", the request would take the legacy path instead and this model
// lookup would never happen.
func TestNewSeedBackendPropagatesSessionModelID(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "codex",
			Category: contracts.CategoryBackend,
			Models: []contracts.ModelSpec{
				{ID: "gpt-gateway", Label: "GPT Gateway", Arg: "gpt-gateway", Route: contracts.RouteGateway},
			},
		},
		Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
			return seedBackend{}, nil
		},
	})
	t.Setenv("HERRSCHER_ROUTE_POLICY", "")
	t.Setenv("NEUBLOX_GATEWAY_URL", "")
	t.Setenv("NEUBLOX_GATEWAY_TOKEN", "")

	sess := state.Session{Vendor: "codex", ModelID: "gpt-gateway"}
	_, err := newSeedBackend(context.Background(), sess)
	if err == nil {
		t.Fatal("newSeedBackend succeeded with a gateway-route model and no gateway credentials; want fail-closed error")
	}
	if !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("error does not indicate the gateway credential path was reached: %v", err)
	}

	// Sanity: without a ModelID the same session takes the legacy path and
	// succeeds, since the registered plugin needs no route resolution.
	legacy := state.Session{Vendor: "codex"}
	if _, err := newSeedBackend(context.Background(), legacy); err != nil {
		t.Fatalf("legacy path (no ModelID) failed: %v", err)
	}
}

type seedBackend struct{ reply string }

func (b seedBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	if b.reply != "" {
		return b.reply, nil
	}
	return "reply", nil
}
func (seedBackend) Close() error { return nil }
