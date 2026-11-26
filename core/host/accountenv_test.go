package host

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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
	env, _, err := sessionEnv(newHostMessages(), BackendRequest{AccountRoot: root}, contracts.VendorClaude, modelRoute{})
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
	env, _, err := sessionEnv(newHostMessages(), BackendRequest{AccountRoot: root, Account: "boulot"}, contracts.VendorClaude, modelRoute{})
	if err != nil {
		t.Fatalf("sessionEnv: %v", err)
	}
	want := dirOf(t, store, contracts.VendorClaude, mustProfile(t, "boulot"))
	if env[contracts.EnvClaudeConfigDir] != want {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, want the session profile %q", env[contracts.EnvClaudeConfigDir], want)
	}
}

func TestDefaultProfileAppliesWhenTheSessionChoosesNone(t *testing.T) {
	root, store := profileRoot(t, contracts.VendorCodex, "perso")
	if err := store.SetDefault(contracts.VendorCodex, mustProfile(t, "perso")); err != nil {
		t.Fatal(err)
	}
	env, _, err := sessionEnv(newHostMessages(), BackendRequest{AccountRoot: root}, contracts.VendorCodex, modelRoute{})
	if err != nil {
		t.Fatalf("sessionEnv: %v", err)
	}
	if env[contracts.EnvCodexHome] != dirOf(t, store, contracts.VendorCodex, mustProfile(t, "perso")) {
		t.Fatalf("CODEX_HOME = %q, want the vendor default", env[contracts.EnvCodexHome])
	}
}

func TestGatewayRouteIgnoresTheProfile(t *testing.T) {
	root, _ := profileRoot(t, contracts.VendorClaude, "perso")
	gateway := modelRoute{env: map[string]string{contracts.EnvAnthropicBaseURL: "https://gw.example"}}
	env, _, err := sessionEnv(newHostMessages(), BackendRequest{AccountRoot: root, Account: "perso"}, contracts.VendorClaude, gateway)
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
	env, _, err := sessionEnv(newHostMessages(), BackendRequest{AccountRoot: root, Account: "perso", Keys: keys}, contracts.VendorCodex, modelRoute{})
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
	env, _, err := spawnEnvVia(newHostMessages(), entry, getenv, keys)
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
	env, _, err := spawnEnvVia(newHostMessages(), entry, getenv, nil)
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
	env, _, err := sessionEnv(newHostMessages(), BackendRequest{AccountRoot: root, Keys: keys}, contracts.VendorClaude, modelRoute{})
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
	env, _, err := sessionEnv(newHostMessages(), BackendRequest{AccountRoot: t.TempDir()}, contracts.VendorCursor, modelRoute{})
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

func dirOf(t *testing.T, store accounts.Store, vendor string, name contracts.ProfileName) string {
	t.Helper()
	dir, err := store.Dir(vendor, name)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProfileUsersCountsSessionsFollowingTheDefault(t *testing.T) {
	sessions := []state.Session{
		{Name: "explicite", Vendor: contracts.VendorClaude, Account: "perso"},
		{Name: "defaut", Vendor: contracts.VendorClaude},
		{Name: "autre", Vendor: contracts.VendorClaude, Account: "boulot"},
		{Name: "codex", Vendor: contracts.VendorCodex},
	}
	if got := profileUsers(sessions, contracts.VendorClaude, "perso", false); !slices.Equal(got, []string{"explicite"}) {
		t.Fatalf("users of a non-default profile = %v, want the session that chose it", got)
	}
	if got := profileUsers(sessions, contracts.VendorClaude, "perso", true); !slices.Equal(got, []string{"explicite", "defaut"}) {
		t.Fatalf("users of the default profile = %v, want the chooser and the follower", got)
	}
	if got := profileUsers(sessions, contracts.VendorClaude, "boulot", false); !slices.Equal(got, []string{"autre"}) {
		t.Fatalf("users of boulot = %v, want autre only", got)
	}
}

func TestGrokProfileGoesThroughTheXAIProxy(t *testing.T) {
	root, store := profileRoot(t, contracts.VendorGrok, "perso")
	keys := keysWith(t, config.ProviderXAI, map[string]string{"KEY_MAIN": "xai-real-value"})
	env, release, err := sessionEnv(newHostMessages(), BackendRequest{AccountRoot: root, Account: "perso", Keys: keys}, contracts.VendorGrok, modelRoute{})
	if err != nil {
		t.Fatalf("sessionEnv: %v", err)
	}
	defer release()
	if env[contracts.EnvGrokHome] != dirOf(t, store, contracts.VendorGrok, mustProfile(t, "perso")) {
		t.Fatalf("GROK_HOME = %q, want the profile directory", env[contracts.EnvGrokHome])
	}
	if base := env[grokAPIBaseURLEnv]; !strings.HasPrefix(base, "http://127.0.0.1:") || !strings.HasSuffix(base, "/v1") {
		t.Fatalf("base URL = %q, want the loopback proxy under /v1", base)
	}
	if !strings.HasPrefix(env[grokKeyVariable], "hsk_") {
		t.Fatalf("key = %q, want an ephemeral proxy token", env[grokKeyVariable])
	}
	for name, value := range env {
		if strings.Contains(value, "xai-real-value") {
			t.Fatalf("%s carries the real key", name)
		}
	}
}

func TestGrokSpawnGetsTheProfileTheProxyAndItsOwnCommand(t *testing.T) {
	var got contracts.PluginConfig
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	for _, kind := range []string{contracts.VendorClaude, contracts.VendorGrok} {
		contracts.Default.Register(contracts.Plugin{
			Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryBackend, Config: []contracts.Setting{{Key: "cmd", Default: kind}}},
			Backend: func(_ context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
				got = cfg
				return seedBackend{}, nil
			},
		})
	}
	root, store := profileRoot(t, contracts.VendorGrok, "perso")
	keys := keysWith(t, config.ProviderXAI, map[string]string{"KEY_MAIN": "xai-real-value"})
	backend, err := BuildBackendFor(context.Background(), NewMessages(), BackendRequest{
		Vendor: contracts.VendorGrok, Cmd: contracts.VendorClaude, AccountRoot: root, Account: "perso", Keys: keys,
	})
	if err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := backend.Close(); closeErr != nil {
			t.Errorf("close: %v", closeErr)
		}
	})
	if cmd := got.Get("cmd"); cmd != contracts.VendorGrok {
		t.Fatalf("cmd = %q, want the grok default instead of the claude command", cmd)
	}
	env := contracts.ParseEnvSetting(got.Get("env"))
	if env[contracts.EnvGrokHome] != dirOf(t, store, contracts.VendorGrok, mustProfile(t, "perso")) {
		t.Fatalf("GROK_HOME = %q, want the profile directory", env[contracts.EnvGrokHome])
	}
	if !strings.HasPrefix(env[grokKeyVariable], "hsk_") || !strings.HasSuffix(env[grokAPIBaseURLEnv], "/v1") {
		t.Fatalf("env = %v, want the proxy token and base URL", env)
	}
}

func TestACommandOfTheSessionVendorIsKept(t *testing.T) {
	plugins := []contracts.Plugin{testBackendPlugin(contracts.VendorClaude), testBackendPlugin(contracts.VendorGrok)}
	cases := []struct{ vendor, cmd, want string }{
		{contracts.VendorGrok, "claude --model x", ""},
		{contracts.VendorGrok, "grok --debug", "grok --debug"},
		{contracts.VendorGrok, "/opt/wrapper.sh", "/opt/wrapper.sh"},
		{contracts.VendorClaude, "claude", "claude"},
	}
	for _, c := range cases {
		if got := commandFor(c.vendor, c.cmd, plugins); got != c.want {
			t.Errorf("commandFor(%q, %q) = %q, want %q", c.vendor, c.cmd, got, c.want)
		}
	}
}
