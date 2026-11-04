package host

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/keyproxy"
)

func profileRoot(t *testing.T, vendor string, names ...string) (string, accounts.Store) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "accounts")
	store := accounts.Store{Root: root}
	for _, raw := range names {
		name, err := contracts.ParseProfileName(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Create(vendor, name); err != nil {
			t.Fatal(err)
		}
	}
	return root, store
}

func keysWith(t *testing.T, provider string, env map[string]string) *keyproxy.Proxy {
	t.Helper()
	keys := &config.KeysConfig{Providers: map[string]config.ProviderKeys{
		provider: {Sources: []config.KeySource{{Name: "main", KeyEnv: "KEY_MAIN"}}},
	}}
	loader := keyproxy.Loader{Getenv: func(name string) string { return env[name] }}
	proxy, err := keyproxy.Open(context.Background(), keys, loader, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("keyproxy.Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := proxy.Close(context.Background()); closeErr != nil {
			t.Errorf("close key proxy: %v", closeErr)
		}
	})
	return proxy
}

func TestNativeClaudeSpawnCarriesNoProfileVariable(t *testing.T) {
	root, _ := profileRoot(t, contracts.VendorClaude, "perso")
	env, err := sessionEnv(BackendRequest{AccountRoot: root}, contracts.VendorClaude, modelRoute{})
	if err != nil {
		t.Fatalf("sessionEnv: %v", err)
	}
	if len(env) != 0 {
		t.Fatalf("native spawn env = %v, want none", env)
	}
}

func TestSessionAccountWinsOverTheVendorDefault(t *testing.T) {
	root, store := profileRoot(t, contracts.VendorClaude, "perso", "boulot")
	perso, err := contracts.ParseProfileName("perso")
	if err != nil {
		t.Fatal(err)
	}
	if setErr := store.SetDefault(contracts.VendorClaude, perso); setErr != nil {
		t.Fatal(setErr)
	}
	env, err := sessionEnv(BackendRequest{AccountRoot: root, Account: "boulot"}, contracts.VendorClaude, modelRoute{})
	if err != nil {
		t.Fatalf("sessionEnv: %v", err)
	}
	want := store.Dir(contracts.VendorClaude, mustProfile(t, "boulot"))
	if env[contracts.EnvClaudeConfigDir] != want {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, want the session profile %q", env[contracts.EnvClaudeConfigDir], want)
	}
}

func TestDefaultProfileAppliesWhenTheSessionChoosesNone(t *testing.T) {
	root, store := profileRoot(t, contracts.VendorCodex, "perso")
	if err := store.SetDefault(contracts.VendorCodex, mustProfile(t, "perso")); err != nil {
		t.Fatal(err)
	}
	env, err := sessionEnv(BackendRequest{AccountRoot: root}, contracts.VendorCodex, modelRoute{})
	if err != nil {
		t.Fatalf("sessionEnv: %v", err)
	}
	if env[contracts.EnvCodexHome] != store.Dir(contracts.VendorCodex, mustProfile(t, "perso")) {
		t.Fatalf("CODEX_HOME = %q, want the vendor default", env[contracts.EnvCodexHome])
	}
}

func TestGatewayRouteIgnoresTheProfile(t *testing.T) {
	root, _ := profileRoot(t, contracts.VendorClaude, "perso")
	gateway := modelRoute{env: map[string]string{contracts.EnvAnthropicBaseURL: "https://gw.example"}}
	env, err := sessionEnv(BackendRequest{AccountRoot: root, Account: "perso"}, contracts.VendorClaude, gateway)
	if err != nil {
		t.Fatalf("sessionEnv: %v", err)
	}
	if _, ok := env[contracts.EnvClaudeConfigDir]; ok || env[contracts.EnvAnthropicBaseURL] != "https://gw.example" {
		t.Fatalf("gateway env = %v, want the gateway pair only", env)
	}
}

func TestCodexProfileIsRefusedThroughTheProxy(t *testing.T) {
	root, _ := profileRoot(t, contracts.VendorCodex, "perso")
	keys := keysWith(t, "openai", map[string]string{"KEY_MAIN": "sk-real"})
	_, err := sessionEnv(BackendRequest{AccountRoot: root, Account: "perso", Keys: keys}, contracts.VendorCodex, modelRoute{})
	if !errors.Is(err, ErrAccountProxyCodex) {
		t.Fatalf("sessionEnv = %v, want ErrAccountProxyCodex", err)
	}
}

func TestClaudeProxySpawnHoldsOnlyTheGrant(t *testing.T) {
	root, _ := profileRoot(t, contracts.VendorClaude)
	keys := keysWith(t, "anthropic", map[string]string{"KEY_MAIN": "sk-ant-real-value"})
	env, err := sessionEnv(BackendRequest{AccountRoot: root, Keys: keys}, contracts.VendorClaude, modelRoute{})
	if err != nil {
		t.Fatalf("sessionEnv: %v", err)
	}
	if !strings.HasPrefix(env[contracts.EnvAnthropicBaseURL], "http://127.0.0.1:") {
		t.Fatalf("base URL = %q, want the loopback proxy", env[contracts.EnvAnthropicBaseURL])
	}
	if !strings.HasPrefix(env[contracts.EnvAnthropicAPIKey], "hsk_") {
		t.Fatalf("key = %q, want an ephemeral proxy token", env[contracts.EnvAnthropicAPIKey])
	}
	for name, value := range env {
		if strings.Contains(value, "sk-ant-real-value") {
			t.Fatalf("%s carries the real key", name)
		}
	}
}

func TestCursorWithoutProfileNeedsNoEnv(t *testing.T) {
	env, err := sessionEnv(BackendRequest{AccountRoot: t.TempDir()}, contracts.VendorCursor, modelRoute{})
	if err != nil || len(env) != 0 {
		t.Fatalf("cursor sessionEnv = %v %v, want an empty env and no error", env, err)
	}
}

func mustProfile(t *testing.T, raw string) contracts.ProfileName {
	t.Helper()
	name, err := contracts.ParseProfileName(raw)
	if err != nil {
		t.Fatal(err)
	}
	return name
}
