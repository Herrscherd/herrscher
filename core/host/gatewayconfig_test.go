package host

import (
	"context"
	"os"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func resetGatewayConfigCapture(t *testing.T) {
	t.Helper()
	clear := func() {
		capturedConfig.mu.Lock()
		capturedConfig.vals = nil
		capturedConfig.mu.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

func gatewayDeclaring(kind string, env ...string) contracts.Plugin {
	var cfg []contracts.Setting
	for _, e := range env {
		cfg = append(cfg, contracts.Setting{Key: strings.ToLower(e), Env: e, Required: true})
	}
	return contracts.Plugin{
		Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryGateway, Config: cfg},
		Gateway: func(context.Context, contracts.PluginConfig) (contracts.GatewaySet, error) {
			return contracts.GatewaySet{Gateway: fakeGateway{}}, nil
		},
	}
}

func envHas(key string) bool {
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, key+"=") {
			return true
		}
	}
	return false
}

func TestADeclaredGatewayKeyLeavesNothingInTheEnvironmentABridgeInherits(t *testing.T) {
	resetGatewayConfigCapture(t)
	t.Setenv("TEST_GATEWAY_TOKEN", "s3cret")

	CaptureGatewayConfig([]contracts.Plugin{gatewayDeclaring("chat", "TEST_GATEWAY_TOKEN")})

	if envHas("TEST_GATEWAY_TOKEN") {
		t.Fatal("the key is still in os.Environ(), which is exactly what the supervisor hands every bridge child")
	}
	if got := gatewayConfigGetenv(os.Getenv)("TEST_GATEWAY_TOKEN"); got != "s3cret" {
		t.Fatalf("the capture lost the value: got %q, want %q", got, "s3cret")
	}
}

func TestAKeyNoManifestDeclaresSurvivesTheCapture(t *testing.T) {
	resetGatewayConfigCapture(t)
	t.Setenv("TEST_GATEWAY_TOKEN", "s3cret")
	t.Setenv("ANTHROPIC_API_KEY", "operator-key")

	CaptureGatewayConfig([]contracts.Plugin{gatewayDeclaring("chat", "TEST_GATEWAY_TOKEN")})

	if !envHas("ANTHROPIC_API_KEY") {
		t.Fatal("the capture took a key no gateway manifest declares")
	}
}

func TestARealEnvironmentValueSurvivesAnEnvFileThatDisagrees(t *testing.T) {
	resetGatewayConfigCapture(t)
	t.Setenv("TEST_GATEWAY_TOKEN", "from-the-environment")
	plugins := []contracts.Plugin{gatewayDeclaring("chat", "TEST_GATEWAY_TOKEN")}

	CaptureGatewayConfig(plugins)
	os.Setenv("TEST_GATEWAY_TOKEN", "from-the-file")
	CaptureGatewayConfig(plugins)

	if envHas("TEST_GATEWAY_TOKEN") {
		t.Fatal("a value that reappeared was left in the environment")
	}
	if got := gatewayConfigGetenv(os.Getenv)("TEST_GATEWAY_TOKEN"); got != "from-the-environment" {
		t.Fatalf("the file outranked the real environment, which reverses the loader's own precedence: got %q", got)
	}
}

func TestAValueTheEnvFileBringsAfterAnEmptyCaptureIsStillTaken(t *testing.T) {
	resetGatewayConfigCapture(t)
	plugins := []contracts.Plugin{gatewayDeclaring("chat", "TEST_GATEWAY_TOKEN")}

	CaptureGatewayConfig(plugins)
	t.Setenv("TEST_GATEWAY_TOKEN", "from-the-file")
	CaptureGatewayConfig(plugins)

	if envHas("TEST_GATEWAY_TOKEN") {
		t.Fatal("the value the env file brought was left in the environment")
	}
	if got := gatewayConfigGetenv(os.Getenv)("TEST_GATEWAY_TOKEN"); got != "from-the-file" {
		t.Fatalf("the first, empty capture wiped what serve --env-file loaded next: got %q", got)
	}
}

func TestABackendPluginsSettingsAreNotTheCapturesBusiness(t *testing.T) {
	resetGatewayConfigCapture(t)
	t.Setenv("TEST_BACKEND_KEY", "operator-key")
	backend := contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "claude",
			Category: contracts.CategoryBackend,
			Config:   []contracts.Setting{{Key: "key", Env: "TEST_BACKEND_KEY"}},
		},
	}

	CaptureGatewayConfig([]contracts.Plugin{backend})

	if !envHas("TEST_BACKEND_KEY") {
		t.Fatal("the capture took a backend's setting, which the vendor CLI needs to run")
	}
}

func TestBuildHubStillBuildsAGatewayWhoseKeyTheCaptureEmptied(t *testing.T) {
	resetGatewayConfigCapture(t)
	t.Setenv("TEST_GATEWAY_TOKEN", "s3cret")
	plugins := []contracts.Plugin{gatewayDeclaring("chat", "TEST_GATEWAY_TOKEN")}

	CaptureGatewayConfig(plugins)

	hub, err := BuildHub(context.Background(), plugins, os.Getenv)
	if err != nil {
		t.Fatalf("BuildHub failed on an environment its own capture emptied: %v", err)
	}
	if _, ok := hub.Get("chat"); !ok {
		t.Fatal("the gateway did not build")
	}
}
