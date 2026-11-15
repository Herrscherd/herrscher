package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSelectBackendVendorPrecedence(t *testing.T) {
	plugins := []contracts.Plugin{
		{Manifest: contracts.Manifest{Kind: "claude"}, Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) { return seedBackend{}, nil }},
		{Manifest: contracts.Manifest{Kind: "codex"}, Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) { return seedBackend{}, nil }},
		{Manifest: contracts.Manifest{Kind: "cursor"}, Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) { return seedBackend{}, nil }},
	}
	tests := []struct {
		name        string
		session     string
		env         string
		want        string
		wantUnknown bool
	}{
		{name: "explicit beats env", session: "cursor", env: "codex", want: "cursor"},
		{name: "env beats fallback", env: "codex", want: "codex"},
		{name: "fallback is first", want: "claude"},
		{name: "unknown is an error", session: "gemini", env: "codex", wantUnknown: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBackendEnv(t, tt.env)
			desired := tt.session
			if desired == "" {
				desired = os.Getenv("HERRSCHER_BACKEND")
			}
			plugin, err := selectBackend(desired, plugins)
			if tt.wantUnknown {
				assertUnknownBackend(t, err, desired)
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

func setBackendEnv(t *testing.T, value string) {
	t.Helper()
	if value == "" {
		logCleanup(os.Unsetenv("HERRSCHER_BACKEND"))
		return
	}
	t.Setenv("HERRSCHER_BACKEND", value)
}

func assertUnknownBackend(t *testing.T, err error, name string) {
	t.Helper()
	var unknown *UnknownError
	if !errors.As(err, &unknown) || unknown.Name != name {
		t.Fatalf("selectBackend error = %v, want an UnknownError for %q", err, name)
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

func TestBuildBackendForInfersVendorFromCmd(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	setBackendEnv(t, "")

	var built string
	makePlugin := func(kind string) contracts.Plugin {
		return contracts.Plugin{
			Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryBackend, Config: []contracts.Setting{{Key: "cmd", Default: kind}}},
			Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
				built = kind
				return seedBackend{}, nil
			},
		}
	}
	contracts.Default.Register(makePlugin("codex"))
	contracts.Default.Register(makePlugin("claude"))

	if _, err := BuildBackendFor(context.Background(), BackendRequest{Cmd: "/usr/bin/claude --model claude-opus-5-5 --effort medium"}); err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	if built != "claude" {
		t.Fatalf("built %q, want claude: a vendorless session must run the backend its cmd names, not the first registered one", built)
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
	t.Chdir(worker)

	var gotDir string
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "codex", Category: contracts.CategoryBackend},
		Backend: func(_ context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			gotDir = cfg.Settings["dir"]
			return seedBackend{}, nil
		},
	})

	sess := state.Session{Vendor: "codex", Worktree: relative}
	if _, err := newSeedBackend(context.Background(), sess, nil, nil); err != nil {
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
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{}, nil
	}

	sess := state.Session{Name: "solo", ChannelID: "channel"}
	spy := &seedSpyOrchestrator{}
	if _, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), spy, oneShotSeedRuntime{backend: backend}); err != nil {
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
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "travail préparé\n⟢ delegate: roblox-scripter " + trailerSeparator + " inspection en lecture seule"}, nil
	}

	sess := state.Session{Name: "orchestrator", ChannelID: "channel"}
	coord := &recordingCoord{}
	if _, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "délègue", newTurnID(), nil, oneShotSeedRuntime{backend: backend, coordinator: coord}); err != nil {
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
	layout := testRuntime(t)
	forward := func(_ context.Context, path string, argv []string) (string, bool, error) {
		gotPath = path
		gotArgv = append([]string(nil), argv...)
		return "réponse live", true, nil
	}

	got, err := runOneShotSeedCommand(
		context.Background(), nil, "orchestrator", "lecture seule",
		"turn-test", oneShotSeedRuntime{}, nil, layout, "instance-test", forward,
	)
	if err != nil {
		t.Fatalf("runOneShotSeedCommand: %v", err)
	}
	if got != "réponse live" {
		t.Fatalf("reply = %q, want %q", got, "réponse live")
	}
	if want := CommandSocketPathIn(layout, "instance-test"); gotPath != want {
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
	_, err := newSeedBackend(context.Background(), sess, nil, nil)
	if err == nil {
		t.Fatal("newSeedBackend succeeded with a gateway-route model and no gateway credentials; want fail-closed error")
	}
	if !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("error does not indicate the gateway credential path was reached: %v", err)
	}

	legacy := state.Session{Vendor: "codex"}
	if _, err := newSeedBackend(context.Background(), legacy, nil, nil); err != nil {
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

type consolidateSpy struct {
	err    error
	called bool
	ctxErr error
}

func (c *consolidateSpy) Context(context.Context) string                          { return "" }
func (c *consolidateSpy) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (c *consolidateSpy) Consolidate(ctx context.Context) error {
	c.called, c.ctxErr = true, ctx.Err()
	return c.err
}
func (c *consolidateSpy) Close() error { return nil }

func TestASeedKeepsItsReplyWhenConsolidationFails(t *testing.T) {
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "réponse gardée"}, nil
	}

	sess := state.Session{Name: "solo", ChannelID: "channel"}
	spy := &consolidateSpy{err: context.DeadlineExceeded}
	got, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), spy, oneShotSeedRuntime{backend: backend, warnings: io.Discard})
	if err != nil {
		t.Fatalf("runOneShotSeedWith: %v", err)
	}
	if got != "réponse gardée" {
		t.Fatalf("reply = %q, want %q", got, "réponse gardée")
	}
}

func TestASeedConsolidatesOnItsOwnBudget(t *testing.T) {
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{}, nil
	}

	sess := state.Session{Name: "solo", ChannelID: "channel"}
	spy := &consolidateSpy{}
	if _, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), spy,
		oneShotSeedRuntime{backend: backend, timeout: 50 * time.Millisecond}); err != nil {
		t.Fatalf("runOneShotSeedWithIDRuntime: %v", err)
	}
	if !spy.called {
		t.Fatal("expected Consolidate to run")
	}
	if spy.ctxErr != nil {
		t.Fatalf("consolidation ran on the turn's spent budget: %v", spy.ctxErr)
	}
}

func TestASeedWithACadenceDoesNotConsolidateTwice(t *testing.T) {
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{}, nil
	}

	sess := state.Session{Name: "solo", ChannelID: "channel", ConsolidateEvery: 1}
	spy := &consolidateSpy{}
	if _, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), spy, oneShotSeedRuntime{backend: backend}); err != nil {
		t.Fatalf("runOneShotSeedWith: %v", err)
	}
	if spy.called {
		t.Fatal("expected no closing Consolidate when the orchestrator has a cadence")
	}
}

func TestSeedBackendUsesTheStoredAccountThroughTheProxy(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := accounts.Store{Root: accounts.DefaultRoot(home)}
	name, err := contracts.ParseProfileName("perso")
	if err != nil {
		t.Fatal(err)
	}
	profileDir, err := store.Create(contracts.VendorClaude, name)
	if err != nil {
		t.Fatal(err)
	}

	var seenKey atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seenKey.Store(request.Header.Get("X-Api-Key"))
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	keys := keysWithUpstream(t, "anthropic", upstream.URL, map[string]string{"KEY_MAIN": "sk-main"})

	settings := registerClaudeWithSettings(t, func() (contracts.Backend, error) { return seedBackend{}, nil })
	if _, err := newSeedBackend(context.Background(), state.Session{Vendor: contracts.VendorClaude, Account: "perso"}, keys, nil); err != nil {
		t.Fatalf("newSeedBackend: %v", err)
	}
	env := contracts.ParseEnvSetting(settings()["env"])
	if got := env[contracts.EnvClaudeConfigDir]; got != profileDir {
		t.Fatalf("seed profile dir = %q, want the stored account %q", got, profileDir)
	}
	token := env[contracts.EnvAnthropicAPIKey]
	if token == "" || token == "sk-main" {
		t.Fatalf("seed carries %q, want a grant token and never the real key", token)
	}
	if status := askProxy(t, env[contracts.EnvAnthropicBaseURL], token); status != http.StatusOK {
		t.Fatalf("grant token status %d, want 200 through the proxy", status)
	}
	if got := seenKey.Load(); got != "sk-main" {
		t.Fatalf("upstream got key %v, want the real key injected by the proxy", got)
	}
}

func TestSeedBackendNeverCarriesTheGatewayTokenWithAProxy(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERRSCHER_ROUTE_POLICY", "")
	t.Setenv("NEUBLOX_GATEWAY_URL", "https://gateway.example")
	t.Setenv("NEUBLOX_GATEWAY_TOKEN", "neublox-secret")
	keys := keysWithUpstream(t, "openai", "https://api.openai.example", map[string]string{"KEY_MAIN": "sk-openai"})

	var settings map[string]string
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "codex",
			Category: contracts.CategoryBackend,
			Models: []contracts.ModelSpec{
				{ID: "gpt-gateway", Label: "GPT Gateway", Arg: "gpt-gateway", Route: contracts.RouteGateway},
			},
		},
		Backend: func(_ context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			settings = cfg.Settings
			return seedBackend{}, nil
		},
	})
	if _, err := newSeedBackend(context.Background(), state.Session{Vendor: "codex", ModelID: "gpt-gateway"}, keys, nil); err != nil {
		t.Fatalf("newSeedBackend: %v", err)
	}
	for name, value := range contracts.ParseEnvSetting(settings["env"]) {
		if strings.Contains(value, "neublox-secret") {
			t.Fatalf("seed env %s carries the Neublox token", name)
		}
	}
	if base := contracts.ParseEnvSetting(settings["env"])[contracts.EnvOpenAIBaseURL]; !strings.HasPrefix(base, "http://127.0.0.1") {
		t.Fatalf("gateway base = %q, want the local key proxy", base)
	}
}

func TestConsolidationFailureIsWarnedInTheSessionLanguage(t *testing.T) {
	spy := &consolidateSpy{err: errors.New("disque plein")}
	var printed bytes.Buffer
	consolidateOneShot(context.Background(), spy, state.Session{Name: "solo"}, &printed, fixedLanguageSource(i18n.French))
	if !spy.called {
		t.Fatal("consolidation was not attempted")
	}
	got := printed.String()
	if !strings.Contains(got, `session "solo" : consolidation après`) {
		t.Fatalf("consolidation failure printed %q, want the french warning", got)
	}
	if strings.Contains(got, "consolidate after") {
		t.Fatalf("consolidation failure fell back to english: %q", got)
	}
}
