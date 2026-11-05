package host

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/keyproxy"
)

func keysWithUpstream(t *testing.T, provider, upstream string, env map[string]string) *keyproxy.Proxy {
	t.Helper()
	keys := &config.KeysConfig{Providers: map[string]config.ProviderKeys{
		provider: {BaseURL: upstream, Sources: []config.KeySource{{Name: "main", KeyEnv: "KEY_MAIN"}}},
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

func askProxy(t *testing.T, base, token string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/v1/messages", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Api-Key", token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	return response.StatusCode
}

func registerClaudeWithSettings(t *testing.T, build func() (contracts.Backend, error)) func() map[string]string {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	var captured map[string]string
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: contracts.VendorClaude, Category: contracts.CategoryBackend},
		Backend: func(_ context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			captured = cfg.Settings
			return build()
		},
	})
	return func() map[string]string { return captured }
}

func TestClosingTheBackendEndsItsKeyGrant(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	settings := registerClaudeWithSettings(t, func() (contracts.Backend, error) { return seedBackend{}, nil })
	keys := keysWithUpstream(t, "anthropic", upstream.URL, map[string]string{"KEY_MAIN": "sk-main"})

	backend, err := BuildBackendFor(context.Background(), BackendRequest{Vendor: contracts.VendorClaude, AccountRoot: t.TempDir(), Keys: keys})
	if err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	env := contracts.ParseEnvSetting(settings()["env"])
	token, base := env[contracts.EnvAnthropicAPIKey], env[contracts.EnvAnthropicBaseURL]
	if status := askProxy(t, base, token); status != http.StatusOK {
		t.Fatalf("while the backend lives: status %d, want 200", status)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	calls := upstreamCalls.Load()
	if status := askProxy(t, base, token); status != http.StatusUnauthorized {
		t.Fatalf("after Close: status %d, want 401", status)
	}
	if got := upstreamCalls.Load(); got != calls {
		t.Fatalf("upstream reached %d more times with a revoked token", got-calls)
	}
}

func TestFailedBackendConstructionEndsItsKeyGrant(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	settings := registerClaudeWithSettings(t, func() (contracts.Backend, error) { return nil, errors.New("boom") })
	keys := keysWithUpstream(t, "anthropic", upstream.URL, map[string]string{"KEY_MAIN": "sk-main"})

	if _, err := BuildBackendFor(context.Background(), BackendRequest{Vendor: contracts.VendorClaude, AccountRoot: t.TempDir(), Keys: keys}); err == nil {
		t.Fatal("BuildBackendFor error = nil, want the plugin error")
	}
	env := contracts.ParseEnvSetting(settings()["env"])
	if status := askProxy(t, env[contracts.EnvAnthropicBaseURL], env[contracts.EnvAnthropicAPIKey]); status != http.StatusUnauthorized {
		t.Fatalf("token of a failed build: status %d, want 401", status)
	}
	if got := upstreamCalls.Load(); got != 0 {
		t.Fatalf("upstream reached %d times with a grant of a failed build", got)
	}
}
