package keyproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/config"
)

type stubUpstream struct {
	mu      sync.Mutex
	seen    []string
	respond func(key string) (int, string)
	retry   string
}

func (s *stubUpstream) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	key := request.Header.Get("X-Api-Key")
	s.mu.Lock()
	s.seen = append(s.seen, key)
	s.mu.Unlock()
	status, body := s.respond(key)
	if status == http.StatusTooManyRequests && s.retry != "" {
		writer.Header().Set("Retry-After", s.retry)
	}
	http.Error(writer, body, status)
}

func (s *stubUpstream) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

func openProxy(t *testing.T, upstreamURL string, provider config.ProviderKeys, keys ...string) *Proxy {
	t.Helper()
	env := make(map[string]string)
	sources := make([]config.KeySource, 0, len(keys))
	for index, value := range keys {
		name := fmt.Sprintf("K%d", index)
		env[name] = value
		sources = append(sources, config.KeySource{Name: name, KeyEnv: name})
	}
	provider.BaseURL = upstreamURL
	provider.Sources = sources
	loader := Loader{Getenv: func(name string) string { return env[name] }}
	keysConfig := &config.KeysConfig{Providers: map[string]config.ProviderKeys{config.ProviderAnthropic: provider}}
	proxy, err := Open(context.Background(), keysConfig, loader, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := proxy.Close(context.Background()); closeErr != nil {
			t.Errorf("close key proxy: %v", closeErr)
		}
	})
	return proxy
}

func grantFor(t *testing.T, proxy *Proxy) Grant {
	t.Helper()
	route, ok := proxy.Provider(config.ProviderAnthropic)
	if !ok {
		t.Fatal("anthropic route missing")
	}
	grant, err := proxy.Grant(route)
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	return grant
}

func post(t *testing.T, grant Grant, body string) (int, string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, grant.BaseURL+"/v1/messages", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Api-Key", grant.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if joined := errors.Join(readErr, closeErr); joined != nil {
		t.Fatal(joined)
	}
	return response.StatusCode, strings.TrimSpace(string(data))
}

func TestFailoverMovesToNextKeyOnRateLimit(t *testing.T) {
	upstream := &stubUpstream{respond: func(key string) (int, string) {
		if key == "first" {
			return http.StatusTooManyRequests, "limited"
		}
		return http.StatusOK, "answer"
	}}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{}, "first", "second")

	status, body := post(t, grantFor(t, proxy), `{}`)

	if status != http.StatusOK || body != "answer" {
		t.Fatalf("got %d %q, want 200 answer", status, body)
	}
	if got := upstream.calls(); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("upstream saw %v, want [first second]", got)
	}
}

func TestCooledKeyIsSkippedOnNextRequest(t *testing.T) {
	upstream := &stubUpstream{retry: "120", respond: func(key string) (int, string) {
		if key == "first" {
			return http.StatusTooManyRequests, "limited"
		}
		return http.StatusOK, "answer"
	}}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{}, "first", "second")
	grant := grantFor(t, proxy)

	post(t, grant, `{}`)
	post(t, grant, `{}`)

	if got := upstream.calls(); len(got) != 3 || got[2] != "second" {
		t.Fatalf("upstream saw %v, want the third call on second only", got)
	}
}

func TestEveryKeyLimitedRelaysTheLastAnswer(t *testing.T) {
	upstream := &stubUpstream{respond: func(string) (int, string) {
		return http.StatusTooManyRequests, "limited"
	}}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{}, "first", "second")

	status, _ := post(t, grantFor(t, proxy), `{}`)

	if status != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429 relayed", status)
	}
}

func TestClientErrorDoesNotRotate(t *testing.T) {
	upstream := &stubUpstream{respond: func(string) (int, string) {
		return http.StatusBadRequest, "bad body"
	}}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{}, "first", "second")

	status, body := post(t, grantFor(t, proxy), `{}`)

	if status != http.StatusBadRequest || body != "bad body" {
		t.Fatalf("got %d %q, want 400 relayed", status, body)
	}
	if got := upstream.calls(); len(got) != 1 {
		t.Fatalf("upstream saw %v, want a single call", got)
	}
}

func TestUnknownTokenIsRefusedWithoutUpstreamCall(t *testing.T) {
	upstream := &stubUpstream{respond: func(string) (int, string) { return http.StatusOK, "" }}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{}, "first")
	grant := grantFor(t, proxy)
	grant.Token = "hsk_forged"

	status, _ := post(t, grant, `{}`)

	if status != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", status)
	}
	if got := upstream.calls(); len(got) != 0 {
		t.Fatalf("upstream saw %v, want none", got)
	}
}

func TestRevokedTokenStopsWorking(t *testing.T) {
	upstream := &stubUpstream{respond: func(string) (int, string) { return http.StatusOK, "" }}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{}, "first")
	grant := grantFor(t, proxy)

	proxy.Revoke(grant.Token)
	status, _ := post(t, grant, `{}`)

	if status != http.StatusUnauthorized {
		t.Fatalf("status %d after revoke, want 401", status)
	}
}

func TestOversizedBodyIsRefused(t *testing.T) {
	upstream := &stubUpstream{respond: func(string) (int, string) { return http.StatusOK, "" }}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{MaxBodyBytes: 8}, "first")

	status, _ := post(t, grantFor(t, proxy), strings.Repeat("x", 9))

	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", status)
	}
	if got := upstream.calls(); len(got) != 0 {
		t.Fatalf("upstream saw %v, want none", got)
	}
}

func TestSecretNeverPrintsItsValue(t *testing.T) {
	secret := Secret{name: "K0", value: "sk-live-very-secret"}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if printed := fmt.Sprintf(verb, secret); strings.Contains(printed, "sk-live") {
			t.Fatalf("%s printed the value: %q", verb, printed)
		}
	}
}

func TestPoolRetryAfterOverridesCooldown(t *testing.T) {
	pool := NewPool("anthropic", []Secret{{name: "a"}, {name: "b"}}, PoolOptions{
		Strategy: config.StrategyFailover,
		Cooldown: time.Hour,
	})
	now := time.Now()
	pool.cool(0, now, 2*time.Second)

	index, ok := pool.pick(now.Add(3*time.Second), make(map[int]bool))
	if !ok || index != 0 {
		t.Fatalf("pick after Retry-After elapsed = %d %v, want key 0 back", index, ok)
	}
}
