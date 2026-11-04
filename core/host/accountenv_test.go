package host

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/internal/state"
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

func TestCodexProfileGoesThroughTheProxyInItsOwnHome(t *testing.T) {
	root, _ := profileRoot(t, contracts.VendorCodex, "perso")
	keys := keysWith(t, "openai", map[string]string{"KEY_MAIN": "sk-real"})
	env, err := sessionEnv(BackendRequest{AccountRoot: root, Account: "perso", Keys: keys}, contracts.VendorCodex, modelRoute{})
	if err != nil {
		t.Fatalf("sessionEnv: %v", err)
	}
	home := filepath.Join(root, contracts.VendorCodex, "perso", codexProxyHomeDir)
	if env[contracts.EnvCodexHome] != home {
		t.Fatalf("CODEX_HOME = %q, want %q", env[contracts.EnvCodexHome], home)
	}
	if _, ok := env[contracts.EnvOpenAIBaseURL]; ok {
		t.Fatal("OPENAI_BASE_URL set: the codex backend would replace the profile home")
	}
	if !strings.HasPrefix(env[codexProxyGrantEnv], "hsk_") {
		t.Fatalf("%s = %q, want a proxy grant", codexProxyGrantEnv, env[codexProxyGrantEnv])
	}
	written, err := os.ReadFile(filepath.Join(home, codexProxyConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `base_url = "http://127.0.0.1:`) || !strings.Contains(string(written), `env_key = "HERRSCHER_KEY_PROXY_GRANT"`) {
		t.Fatalf("codex proxy config = %s", written)
	}
}

func TestGatewayRouteGoesThroughTheProxyWhenEnabled(t *testing.T) {
	keys := keysWith(t, "anthropic", map[string]string{"KEY_MAIN": "sk-ant-real"})
	getenv := func(name string) string {
		return map[string]string{EnvGatewayURL: "https://gw.example/anthropic", EnvGatewayToken: "nbx-secret-token"}[name]
	}
	entry := CatalogEntry{ID: "neublox-claude", Vendor: contracts.VendorClaude, Route: contracts.RouteGateway}
	env, err := spawnEnvVia(entry, getenv, keys)
	if err != nil {
		t.Fatalf("spawnEnvVia: %v", err)
	}
	if !strings.HasPrefix(env[contracts.EnvAnthropicBaseURL], "http://127.0.0.1:") {
		t.Fatalf("base URL = %q, want the loopback proxy", env[contracts.EnvAnthropicBaseURL])
	}
	if !strings.HasPrefix(env[contracts.EnvAnthropicAPIKey], "hsk_") {
		t.Fatalf("API key = %q, want a proxy grant", env[contracts.EnvAnthropicAPIKey])
	}
	for _, value := range env {
		if strings.Contains(value, "nbx-secret-token") {
			t.Fatal("the Neublox token reached the vendor child env")
		}
	}
}

func TestGatewayRouteWithoutProxyKeepsTheGatewayPair(t *testing.T) {
	getenv := func(name string) string {
		return map[string]string{EnvGatewayURL: "https://gw.example/anthropic", EnvGatewayToken: "nbx-secret-token"}[name]
	}
	entry := CatalogEntry{ID: "neublox-claude", Vendor: contracts.VendorClaude, Route: contracts.RouteGateway}
	env, err := spawnEnvVia(entry, getenv, nil)
	if err != nil {
		t.Fatalf("spawnEnvVia: %v", err)
	}
	if env[contracts.EnvAnthropicBaseURL] != "https://gw.example/anthropic" || env[contracts.EnvAnthropicAPIKey] != "nbx-secret-token" {
		t.Fatalf("gateway env = %v, want the gateway pair", env)
	}
}

func TestAccountChangeIsBlockedOnlyWithAConversationUnderAnotherAccount(t *testing.T) {
	cases := []struct {
		name    string
		sess    state.Session
		profile string
		fresh   bool
		blocked bool
	}{
		{"no conversation yet", state.Session{Account: "perso"}, "pro", false, false},
		{"same account", state.Session{Account: "perso", ResumeToken: "t"}, "perso", false, false},
		{"conversation, other account", state.Session{Account: "perso", ResumeToken: "t"}, "pro", false, true},
		{"conversation, fresh", state.Session{Account: "perso", ResumeToken: "t"}, "pro", true, false},
	}
	for _, tc := range cases {
		if got := accountChangeBlocked(tc.sess, tc.profile, tc.fresh); got != tc.blocked {
			t.Fatalf("%s: blocked = %v, want %v", tc.name, got, tc.blocked)
		}
	}
}

func TestResumingUnderFollowsOnlySessionsWithoutAnAccount(t *testing.T) {
	sessions := []state.Session{
		{Name: "a", Vendor: contracts.VendorClaude, ResumeToken: "t"},
		{Name: "b", Vendor: contracts.VendorClaude, Account: "perso", ResumeToken: "t"},
		{Name: "c", Vendor: contracts.VendorClaude},
		{Name: "d", Vendor: contracts.VendorCodex, ResumeToken: "t"},
	}
	got := resumingUnder(sessions, contracts.VendorClaude)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("resumingUnder = %v, want [a]", got)
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
