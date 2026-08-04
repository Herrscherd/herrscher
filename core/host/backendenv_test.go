package host

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func creds(t *testing.T) contracts.GatewayCreds {
	t.Helper()
	c, err := contracts.NewGatewayCreds("https://gw.neublox.xyz", "tok-123")
	if err != nil {
		t.Fatalf("NewGatewayCreds: %v", err)
	}
	return c
}

func TestGatewayEnvClaudeSetsBothHalves(t *testing.T) {
	env := gatewayEnvFor("claude", creds(t), "glm-4.7")
	if env["ANTHROPIC_BASE_URL"] != "https://gw.neublox.xyz" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "tok-123" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q", env["ANTHROPIC_AUTH_TOKEN"])
	}
}

func TestGatewayEnvNeverHalfSet(t *testing.T) {
	// THE INVARIANT. Setting the base URL without a token would route traffic
	// to the gateway while billing it to the user's own claude.ai
	// subscription — the shape Anthropic forbids third-party developers from
	// producing. GatewayCreds makes that state unconstructible; this test
	// locks the translation into environment variables.
	for _, vendor := range []string{"claude", "codex"} {
		env := gatewayEnvFor(vendor, creds(t), "m")
		var urlKeys, tokenKeys int
		for k, v := range env {
			if v == "" {
				t.Errorf("%s: %q is set to the empty string", vendor, k)
			}
			switch {
			case strings.HasSuffix(k, "_BASE_URL"):
				urlKeys++
			case strings.HasSuffix(k, "_AUTH_TOKEN"), strings.HasSuffix(k, "_API_KEY"), k == "NEUBLOX_TOKEN":
				tokenKeys++
			}
		}
		if urlKeys == 0 || tokenKeys == 0 {
			t.Errorf("%s: produced %d url key(s) and %d token key(s); both must be present", vendor, urlKeys, tokenKeys)
		}
	}
}

func TestGatewayEnvUnknownVendorIsEmpty(t *testing.T) {
	// A vendor we don't know how to redirect (cursor) must produce NO
	// variables — especially not an orphaned base URL.
	if env := gatewayEnvFor("cursor", creds(t), "m"); len(env) != 0 {
		t.Fatalf("gatewayEnvFor(cursor) = %+v, want empty", env)
	}
}

// resetGatewayCapture clears the process-wide capture around a test, so one
// test's capture cannot leak into the getenv-seam tests that follow.
func resetGatewayCapture(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		capturedGateway.mu.Lock()
		capturedGateway.url, capturedGateway.token, capturedGateway.captured = "", "", false
		capturedGateway.mu.Unlock()
	})
}

// THE LEAK. The pair used to be read live from the environment and never
// removed, so it rode os.Environ() into every vendor CLI on every route. A
// coding agent has a shell: `env | grep NEUBLOX` handed it the product's shared
// paid credential from a session never routed to the gateway.
func TestCaptureGatewayCredsRemovesThePairFromTheEnvironment(t *testing.T) {
	resetGatewayCapture(t)
	env := map[string]string{EnvGatewayURL: "https://gw", EnvGatewayToken: "tok"}
	unset := []string{}
	captureGatewayCreds(
		func(k string) string { return env[k] },
		func(k string) error { unset = append(unset, k); delete(env, k); return nil },
	)
	for _, k := range []string{EnvGatewayURL, EnvGatewayToken} {
		if _, still := env[k]; still {
			t.Errorf("%s survived the capture: it would propagate to every child", k)
		}
	}
	if len(unset) != 2 {
		t.Errorf("unset %v, want both variables", unset)
	}
	// And the captured value is now the ONLY source: an empty getenv must still
	// resolve, or every gateway spawn fails after the scrub.
	creds, err := loadGatewayCreds(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadGatewayCreds after capture: %v", err)
	}
	if creds.BaseURL() != "https://gw" || creds.Token() != "tok" {
		t.Fatalf("captured creds = %q %q", creds.BaseURL(), creds.Token())
	}
}

// The pair must be re-offered to the trusted bridge child through its
// ENVIRONMENT — the bridge is the process that builds backends.
func TestGatewayEnvPairsCarriesTheCapturedPair(t *testing.T) {
	resetGatewayCapture(t)
	env := map[string]string{EnvGatewayURL: "https://gw", EnvGatewayToken: "tok"}
	captureGatewayCreds(func(k string) string { return env[k] }, func(k string) error { delete(env, k); return nil })

	got := GatewayEnvPairs()
	want := []string{EnvGatewayURL + "=https://gw", EnvGatewayToken + "=tok"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("GatewayEnvPairs() = %v, want %v", got, want)
	}
}

func TestGatewayEnvPairsEmptyWithoutCredentials(t *testing.T) {
	resetGatewayCapture(t)
	captureGatewayCreds(func(string) string { return "" }, func(string) error { return nil })
	if got := GatewayEnvPairs(); len(got) != 0 {
		t.Fatalf("GatewayEnvPairs() = %v, want empty", got)
	}
}

// Defence in depth: the environment a NATIVE-route child is actually spawned
// with (backends use MergeEnv(os.Environ(), env)) must contain neither variable
// after the capture. This is the assertion that fails if anything ever puts
// them back.
func TestNativeSpawnEnvironmentCarriesNoGatewayVariables(t *testing.T) {
	resetGatewayCapture(t)
	t.Setenv(EnvGatewayURL, "https://gw")
	t.Setenv(EnvGatewayToken, "tok")
	CaptureGatewayCreds()

	spawnEnv, err := spawnEnvFor(CatalogEntry{
		Vendor:    "claude",
		ModelSpec: contracts.ModelSpec{ID: "m", Label: "M", Arg: "m", Route: contracts.RouteNative},
	}, os.Getenv)
	if err != nil {
		t.Fatalf("spawnEnvFor(native): %v", err)
	}
	for _, kv := range contracts.MergeEnv(os.Environ(), spawnEnv) {
		if strings.HasPrefix(kv, EnvGatewayURL+"=") || strings.HasPrefix(kv, EnvGatewayToken+"=") {
			t.Fatalf("a native-route child would be spawned with %q — the product credential leaks to the vendor CLI", kv)
		}
	}
}

func TestLoadGatewayCredsRefusesHalfConfig(t *testing.T) {
	resetGatewayCapture(t)
	half := func(k string) string {
		if k == "NEUBLOX_GATEWAY_URL" {
			return "https://gw"
		}
		return ""
	}
	if _, err := loadGatewayCreds(half); err == nil {
		t.Fatal("loadGatewayCreds accepted a URL with no token")
	}
}

func TestLoadGatewayCredsAcceptsFullConfig(t *testing.T) {
	resetGatewayCapture(t)
	full := func(k string) string {
		switch k {
		case "NEUBLOX_GATEWAY_URL":
			return "https://gw"
		case "NEUBLOX_GATEWAY_TOKEN":
			return "tok"
		}
		return ""
	}
	c, err := loadGatewayCreds(full)
	if err != nil {
		t.Fatalf("loadGatewayCreds: %v", err)
	}
	if c.BaseURL() != "https://gw" || c.Token() != "tok" {
		t.Fatalf("loadGatewayCreds = %q %q", c.BaseURL(), c.Token())
	}
}

func TestBackendRequestNativeInjectsNothing(t *testing.T) {
	// Non-regression: a native route must not add any variable.
	env, err := spawnEnvFor(CatalogEntry{
		Vendor:    "claude",
		ModelSpec: contracts.ModelSpec{ID: "m", Label: "M", Arg: "m", Route: contracts.RouteNative},
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("spawnEnvFor(native): %v", err)
	}
	if len(env) != 0 {
		t.Fatalf("native route produced env %+v, want empty", env)
	}
}

func TestBackendRequestGatewayWithoutCredsFails(t *testing.T) {
	// THE dangerous case: if resolution fails, we must NOT fall back to the
	// bare environment. On a machine with a claude.ai login, this spawn would
	// silently run on the user's subscription.
	_, err := spawnEnvFor(CatalogEntry{
		Vendor:    "claude",
		ModelSpec: contracts.ModelSpec{ID: "g", Label: "G", Arg: "g", Route: contracts.RouteGateway},
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("spawnEnvFor(gateway) succeeded with no credentials — it must fail closed")
	}
}

func TestBuildBackendForRejectsUnknownModel(t *testing.T) {
	_, err := BuildBackendFor(context.Background(), BackendRequest{
		Vendor: "claude", Cmd: "claude", ModelID: "no-such-model",
	})
	if err == nil {
		t.Fatal("BuildBackendFor accepted an unknown model ID")
	}
	if !strings.Contains(err.Error(), "no-such-model") {
		t.Fatalf("error does not name the model: %v", err)
	}
}

// Defence in depth behind the manager's own refusal: the choke point has other
// callers (a legacy state.json, a seed, a future verb). A modelless spawn gets
// no gateway environment, so under gateway-only it would run on this machine's
// own vendor login — the exact outcome the policy exists to forbid.
func TestBuildBackendForRefusesAModellessSpawnUnderGatewayOnly(t *testing.T) {
	t.Setenv("HERRSCHER_ROUTE_POLICY", "gateway-only")
	_, err := BuildBackendFor(context.Background(), BackendRequest{Vendor: "claude", Cmd: "claude"})
	if err == nil {
		t.Fatal("BuildBackendFor spawned with no model under gateway-only — it would run on the local login")
	}
	if !strings.Contains(err.Error(), "gateway-only") {
		t.Fatalf("error does not name the policy that refused it: %v", err)
	}
}

func TestBuildBackendForWithoutModelIDKeepsLegacyPath(t *testing.T) {
	// Sessions created before this change have no ModelID. They must
	// continue to start on their Cmd alone.
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "claude", Category: contracts.CategoryBackend},
		Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
			return seedBackend{}, nil
		},
	})

	if _, err := BuildBackendFor(context.Background(), BackendRequest{
		Vendor: "claude", Cmd: "claude", Kind: "oneshot",
	}); err != nil {
		t.Fatalf("legacy path (no ModelID) failed: %v", err)
	}
}

// gatewayPlugin registers a single claude backend offering one gateway-routed
// model, capturing the PluginConfig it is built with.
func gatewayPlugin(t *testing.T, gotCfg *contracts.PluginConfig) {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind: "claude", Category: contracts.CategoryBackend,
			Models: []contracts.ModelSpec{{
				ID: "gw-opus", Label: "GW Opus", Arg: "opus", Route: contracts.RouteGateway,
			}},
		},
		Backend: func(_ context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			*gotCfg = cfg
			return seedBackend{}, nil
		},
	})
	t.Setenv("NEUBLOX_GATEWAY_URL", "https://gw.neublox.xyz")
	t.Setenv("NEUBLOX_GATEWAY_TOKEN", "tok-123")
}

// TestBuildBackendForInjectsResolvedGatewayEnv pins the ONE line that actually
// delivers the gateway credentials to the plugin:
//
//	cfg.Settings["env"] = contracts.EncodeEnvSetting(spawnEnv)
//
// Every other gateway test covers a failure path, so deleting that assignment
// left the suite green while the spawned CLI silently ran on the machine's own
// claude.ai login — precisely the outcome the routing feature exists to stop.
func TestBuildBackendForInjectsResolvedGatewayEnv(t *testing.T) {
	var cfg contracts.PluginConfig
	gatewayPlugin(t, &cfg)

	if _, err := BuildBackendFor(context.Background(), BackendRequest{
		Cmd: "claude", ModelID: "gw-opus",
	}); err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	raw, ok := cfg.Settings["env"]
	if !ok {
		t.Fatal(`no "env" setting handed to the plugin: the gateway credentials never reached the backend`)
	}
	env := contracts.ParseEnvSetting(raw)
	if env["ANTHROPIC_BASE_URL"] != "https://gw.neublox.xyz" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "tok-123" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if env["ANTHROPIC_MODEL"] != "opus" {
		t.Errorf("ANTHROPIC_MODEL = %q", env["ANTHROPIC_MODEL"])
	}
}

// nativePlugin registers a single claude backend offering one native-routed
// model, capturing the PluginConfig it is built with.
func nativePlugin(t *testing.T, gotCfg *contracts.PluginConfig) {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind: "claude", Category: contracts.CategoryBackend,
			Models: []contracts.ModelSpec{{
				ID: "native-opus", Label: "Opus", Arg: "opus", Route: contracts.RouteNative,
			}},
		},
		Backend: func(_ context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			*gotCfg = cfg
			return seedBackend{}, nil
		},
	})
}

// The catalog's Arg is the model the CLI must actually be told to run. Every
// backend declares a "model" setting and plumbs cfg.Get("model") into its
// options, so the setting — not the environment — is the authoritative path.
// Without it the native route selects NO model at all, and the codex gateway
// route runs codex's default model billed to our account.
func TestBuildBackendForSetsModelSettingOnNativeRoute(t *testing.T) {
	var cfg contracts.PluginConfig
	nativePlugin(t, &cfg)

	if _, err := BuildBackendFor(context.Background(), BackendRequest{
		Cmd: "claude", ModelID: "native-opus",
	}); err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	if got := cfg.Settings["model"]; got != "opus" {
		t.Fatalf(`cfg.Settings["model"] = %q, want "opus" — the resolved model never reached the backend`, got)
	}
}

func TestBuildBackendForSetsModelSettingOnGatewayRoute(t *testing.T) {
	var cfg contracts.PluginConfig
	gatewayPlugin(t, &cfg)

	if _, err := BuildBackendFor(context.Background(), BackendRequest{
		Cmd: "claude", ModelID: "gw-opus",
	}); err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	if got := cfg.Settings["model"]; got != "opus" {
		t.Fatalf(`cfg.Settings["model"] = %q, want "opus"`, got)
	}
}

// A session whose --vendor disagrees with the model's owning backend used to
// spawn the REQUESTED vendor with the OTHER vendor's environment: codex ignores
// ANTHROPIC_*, never gets a gateway CODEX_HOME, and runs the turn on the
// machine's own ChatGPT login while the session still reads gateway-routed.
// `session switch --vendor codex` on a gateway session reaches exactly this.
func TestBuildBackendForRejectsVendorModelMismatch(t *testing.T) {
	var cfg contracts.PluginConfig
	gatewayPlugin(t, &cfg) // registers "claude" owning "gw-opus"
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "codex", Category: contracts.CategoryBackend},
		Backend: func(_ context.Context, c contracts.PluginConfig) (contracts.Backend, error) {
			cfg = c
			return seedBackend{}, nil
		},
	})

	_, err := BuildBackendFor(context.Background(), BackendRequest{
		Vendor: "codex", Cmd: "codex", ModelID: "gw-opus",
	})
	if err == nil {
		t.Fatal("a codex spawn carrying a claude model was accepted; it would run on the machine's own login")
	}
	if !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "gw-opus") {
		t.Fatalf("error names neither the vendor nor the model: %v", err)
	}
}

// Non-regression: an explicit vendor that AGREES with the model's owner is the
// normal case and must still build.
func TestBuildBackendForAcceptsMatchingVendor(t *testing.T) {
	var cfg contracts.PluginConfig
	nativePlugin(t, &cfg)
	if _, err := BuildBackendFor(context.Background(), BackendRequest{
		Vendor: "claude", Cmd: "claude", ModelID: "native-opus",
	}); err != nil {
		t.Fatalf("matching vendor+model rejected: %v", err)
	}
}

// fakeRemoteResolver stands in for a configured HERRSCHER_REMOTE=backend.
type fakeRemoteResolver struct{ called bool }

func (f *fakeRemoteResolver) Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, error) {
	f.called = true
	return seedBackend{}, nil
}

// A remote proxy is built from its announcement: there is no seam to hand it the
// spawn environment. Returning it would drop the gateway credentials silently.
func TestBuildBackendForRefusesGatewayModelWithRemoteBackend(t *testing.T) {
	var cfg contracts.PluginConfig
	gatewayPlugin(t, &cfg)
	savedResolver := newBackendResolver
	t.Cleanup(func() { newBackendResolver = savedResolver })
	fake := &fakeRemoteResolver{}
	newBackendResolver = func() remoteBackendResolver { return fake }

	_, err := BuildBackendFor(context.Background(), BackendRequest{Cmd: "claude", ModelID: "gw-opus"})
	if err == nil {
		t.Fatal("a gateway model resolved through the remote backend resolver must be refused, not silently stripped of its credentials")
	}
	if !strings.Contains(err.Error(), "gw-opus") {
		t.Fatalf("error does not name the model: %v", err)
	}
	if !fake.called {
		t.Fatal("the remote resolver seam was never consulted; the test proves nothing")
	}
}

// Non-regression: a session with no model still gets the remote backend.
func TestBuildBackendForKeepsRemoteBackendWithoutModel(t *testing.T) {
	savedResolver := newBackendResolver
	t.Cleanup(func() { newBackendResolver = savedResolver })
	newBackendResolver = func() remoteBackendResolver { return &fakeRemoteResolver{} }

	be, err := BuildBackendFor(context.Background(), BackendRequest{Vendor: "claude", Cmd: "claude"})
	if err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	if be == nil {
		t.Fatal("remote backend not returned")
	}
}
