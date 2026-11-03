package host

import (
	"context"
	"os"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
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
	if env["ANTHROPIC_API_KEY"] != "tok-123" {
		t.Errorf("ANTHROPIC_API_KEY = %q", env["ANTHROPIC_API_KEY"])
	}
}

func TestGatewayEnvSetsAPIKeyAndNeverBothVariables(t *testing.T) {
	env := gatewayEnvFor("claude", creds(t), "claude-opus-5")

	if strings.TrimSpace(env[contracts.EnvAnthropicAPIKey]) == "" {
		t.Error("ANTHROPIC_API_KEY doit être posé avec une valeur non vide")
	}
	if _, hasOld := env["ANTHROPIC_AUTH_TOKEN"]; hasOld {
		t.Error("ANTHROPIC_AUTH_TOKEN ne doit plus être posé : les deux ensemble donnent un 401")
	}
}

func TestGatewayEnvNeverHalfSet(t *testing.T) {
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
	if env := gatewayEnvFor("cursor", creds(t), "m"); len(env) != 0 {
		t.Fatalf("gatewayEnvFor(cursor) = %+v, want empty", env)
	}
}

func resetGatewayCapture(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		capturedGateway.mu.Lock()
		capturedGateway.url, capturedGateway.token, capturedGateway.captured = "", "", false
		capturedGateway.mu.Unlock()
	})
}

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
	creds, err := loadGatewayCreds(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadGatewayCreds after capture: %v", err)
	}
	if creds.BaseURL() != "https://gw" || creds.Token() != "tok" {
		t.Fatalf("captured creds = %q %q", creds.BaseURL(), creds.Token())
	}
}

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
			t.Fatalf("a native-route child would be spawned with %q: the product credential leaks to the vendor CLI", kv)
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
	_, err := spawnEnvFor(CatalogEntry{
		Vendor:    "claude",
		ModelSpec: contracts.ModelSpec{ID: "g", Label: "G", Arg: "g", Route: contracts.RouteGateway},
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("spawnEnvFor(gateway) succeeded with no credentials. It must fail closed")
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

func TestBuildBackendForRefusesAModellessSpawnUnderGatewayOnly(t *testing.T) {
	t.Setenv("HERRSCHER_ROUTE_POLICY", "gateway-only")
	_, err := BuildBackendFor(context.Background(), BackendRequest{Vendor: "claude", Cmd: "claude"})
	if err == nil {
		t.Fatal("BuildBackendFor spawned with no model under gateway-only. It would run on the local login")
	}
	if !strings.Contains(err.Error(), "gateway-only") {
		t.Fatalf("error does not name the policy that refused it: %v", err)
	}
}

func TestBuildBackendForWithoutModelIDKeepsLegacyPath(t *testing.T) {
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
	if env["ANTHROPIC_API_KEY"] != "tok-123" {
		t.Errorf("ANTHROPIC_API_KEY = %q", env["ANTHROPIC_API_KEY"])
	}
	if env["ANTHROPIC_MODEL"] != "opus" {
		t.Errorf("ANTHROPIC_MODEL = %q", env["ANTHROPIC_MODEL"])
	}
}

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

func TestBuildBackendForSetsModelSettingOnNativeRoute(t *testing.T) {
	var cfg contracts.PluginConfig
	nativePlugin(t, &cfg)

	if _, err := BuildBackendFor(context.Background(), BackendRequest{
		Cmd: "claude", ModelID: "native-opus",
	}); err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	if got := cfg.Settings["model"]; got != "opus" {
		t.Fatalf(`cfg.Settings["model"] = %q, want "opus": the resolved model never reached the backend`, got)
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

func TestBuildBackendForRejectsVendorModelMismatch(t *testing.T) {
	var cfg contracts.PluginConfig
	gatewayPlugin(t, &cfg)
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

func TestBuildBackendForAcceptsMatchingVendor(t *testing.T) {
	var cfg contracts.PluginConfig
	nativePlugin(t, &cfg)
	if _, err := BuildBackendFor(context.Background(), BackendRequest{
		Vendor: "claude", Cmd: "claude", ModelID: "native-opus",
	}); err != nil {
		t.Fatalf("matching vendor+model rejected: %v", err)
	}
}

type fakeRemoteResolver struct{ called bool }

func (f *fakeRemoteResolver) Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, bool, error) {
	f.called = true
	return seedBackend{}, true, nil
}

func TestBuildBackendForRefusesGatewayModelWithRemoteBackend(t *testing.T) {
	var cfg contracts.PluginConfig
	gatewayPlugin(t, &cfg)
	fake := &fakeRemoteResolver{}

	_, err := BuildBackendFor(context.Background(), BackendRequest{Cmd: "claude", ModelID: "gw-opus", resolver: fake})
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

func TestBuildBackendForKeepsRemoteBackendWithoutModel(t *testing.T) {
	be, err := BuildBackendFor(context.Background(), BackendRequest{Vendor: "claude", Cmd: "claude", resolver: &fakeRemoteResolver{}})
	if err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	if be == nil {
		t.Fatal("remote backend not returned")
	}
}
