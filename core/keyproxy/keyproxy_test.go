package keyproxy

import (
	"bufio"
	"bytes"
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
	return openProxyWithLog(t, upstreamURL, provider, slog.New(slog.DiscardHandler), keys...)
}

func openProxyWithLog(t *testing.T, upstreamURL string, provider config.ProviderKeys, log *slog.Logger, keys ...string) *Proxy {
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
	proxy, err := Open(context.Background(), keysConfig, loader, log)
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

func TestFailoverMovesToNextKeyOn401(t *testing.T) {
	upstream := &stubUpstream{respond: func(key string) (int, string) {
		if key == "first" {
			return http.StatusUnauthorized, "revoked"
		}
		return http.StatusOK, "answer"
	}}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{}, "first", "second")

	status, body := post(t, grantFor(t, proxy), `{}`)

	if status != http.StatusOK || body != "answer" {
		t.Fatalf("got %d %q, want 200 answer after a 401 failover", status, body)
	}
}

func TestAnswerIsRelayedOnceWithoutRotation(t *testing.T) {
	upstream := &stubUpstream{respond: func(string) (int, string) { return http.StatusOK, "answer" }}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{}, "first", "second")

	status, body := post(t, grantFor(t, proxy), `{}`)

	if status != http.StatusOK || body != "answer" {
		t.Fatalf("got %d %q, want 200 answer", status, body)
	}
	if got := upstream.calls(); len(got) != 1 {
		t.Fatalf("upstream saw %v, want a single call", got)
	}
}

func eventStreamUpstream(release <-chan struct{}, waitedOut chan<- struct{}) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(writer, "data: first\n\n"); err != nil {
			return
		}
		if err := http.NewResponseController(writer).Flush(); err != nil {
			return
		}
		select {
		case <-release:
		case <-time.After(5 * time.Second):
			waitedOut <- struct{}{}
		}
		if _, err := io.WriteString(writer, "data: second\n\n"); err != nil {
			return
		}
	}
}

func openEventStream(t *testing.T, grant Grant) *bufio.Reader {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, grant.BaseURL+"/v1/messages", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Api-Key", grant.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close event stream: %v", closeErr)
		}
	})
	return bufio.NewReader(response.Body)
}

func TestEventStreamIsForwardedChunkByChunk(t *testing.T) {
	release := make(chan struct{})
	waitedOut := make(chan struct{}, 1)
	server := httptest.NewServer(eventStreamUpstream(release, waitedOut))
	t.Cleanup(server.Close)
	proxy := openProxy(t, server.URL, config.ProviderKeys{}, "first")
	reader := openEventStream(t, grantFor(t, proxy))

	first, err := reader.ReadString('\n')
	if err != nil || first != "data: first\n" {
		t.Fatalf("first chunk = %q, %v", first, err)
	}
	select {
	case <-waitedOut:
		t.Fatal("the first chunk was held back until the upstream finished")
	default:
	}
	close(release)
	rest, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(rest), "data: second") {
		t.Fatalf("rest = %q, %v", rest, err)
	}
}

func TestSecretsNeverReachTheLog(t *testing.T) {
	upstream := &stubUpstream{respond: func(key string) (int, string) {
		if key == "sk-first-secret" {
			return http.StatusTooManyRequests, "limited"
		}
		return http.StatusOK, "answer"
	}}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	proxy := openProxyWithLog(t, server.URL, config.ProviderKeys{}, log, "sk-first-secret", "sk-second-secret")

	if status, _ := post(t, grantFor(t, proxy), `{}`); status != http.StatusOK {
		t.Fatalf("status %d, want the failover answer", status)
	}
	if strings.Contains(logs.String(), "sk-") {
		t.Fatalf("a key value reached the log: %s", logs.String())
	}
}

func TestKeyCommandOutputIsCapped(t *testing.T) {
	_, err := runKeyCommand(t.Context(), []string{"head", "-c", "20000", "/dev/zero"})
	if !errors.Is(err, errKeyOutputTooLarge) {
		t.Fatalf("err = %v, want the output cap", err)
	}
}

func TestKeyCommandStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runKeyCommand(ctx, []string{"sleep", "10"})
	if err == nil || time.Since(started) > 3*time.Second {
		t.Fatalf("err = %v after %s, want the command stopped early", err, time.Since(started))
	}
}

func TestKeyCommandStderrNeverReachesTheError(t *testing.T) {
	_, err := runKeyCommand(t.Context(), []string{"sh", "-c", "echo leaked-key-material 1>&2; exit 3"})
	if err == nil || strings.Contains(err.Error(), "leaked-key-material") {
		t.Fatalf("err = %v, want a failure without the stderr", err)
	}
}
