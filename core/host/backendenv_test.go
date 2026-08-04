package host

import (
	"context"
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

func TestLoadGatewayCredsRefusesHalfConfig(t *testing.T) {
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
