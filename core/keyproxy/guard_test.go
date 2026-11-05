package keyproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/config"
)

func grantCount(proxy *Proxy) int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return len(proxy.grants)
}

func TestReleasedGrantIsRefusedAndForgotten(t *testing.T) {
	upstream := httptest.NewServer(&stubUpstream{respond: func(string) (int, string) { return http.StatusOK, "ok" }})
	t.Cleanup(upstream.Close)
	proxy := openProxy(t, upstream.URL, config.ProviderKeys{}, "only")
	grant := grantFor(t, proxy)
	if status, _ := post(t, grant, "{}"); status != http.StatusOK {
		t.Fatalf("before release: status %d, want 200", status)
	}
	grant.Release()
	if status, _ := post(t, grant, "{}"); status != http.StatusUnauthorized {
		t.Fatalf("after release: status %d, want 401", status)
	}
	if got := grantCount(proxy); got != 0 {
		t.Fatalf("grants held after release = %d, want 0", got)
	}
}

func TestGrantsDoNotAccumulateAcrossSpawns(t *testing.T) {
	proxy := openProxy(t, "http://127.0.0.1:1", config.ProviderKeys{}, "only")
	for range 200 {
		grantFor(t, proxy).Release()
	}
	if got := grantCount(proxy); got != 0 {
		t.Fatalf("grants held = %d, want 0", got)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	now := time.Now()
	seconds := http.Header{"Retry-After": {"99999999999"}}
	if got := retryAfter(seconds, now); got != maxRetryAfter {
		t.Fatalf("delta-seconds retry-after = %v, want %v", got, maxRetryAfter)
	}
	later := http.Header{"Retry-After": {now.Add(100 * time.Hour).UTC().Format(http.TimeFormat)}}
	if got := retryAfter(later, now); got != maxRetryAfter {
		t.Fatalf("date retry-after = %v, want %v", got, maxRetryAfter)
	}
}

func TestUpstreamNeverSeesTheClientCredentials(t *testing.T) {
	seen := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen <- request.Header.Clone()
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	proxy := openProxy(t, upstream.URL, config.ProviderKeys{}, "pool-key")
	grant := grantFor(t, proxy)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, grant.BaseURL+"/v1/messages", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Api-Key", grant.Token)
	request.Header.Set("Authorization", "Bearer client-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	header := <-seen
	if got := header.Get("Authorization"); got != "" {
		t.Fatalf("upstream got Authorization %q from the client, want none", got)
	}
	if got := header.Get("X-Api-Key"); got != "pool-key" {
		t.Fatalf("upstream X-Api-Key = %q, want the pool key", got)
	}
}

func TestStreamCutAfterFirstChunkIsNotReplayed(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		keys = append(keys, request.Header.Get("X-Api-Key"))
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(writer, "data: first\n\n"); err != nil {
			t.Error(err)
		}
		if err := http.NewResponseController(writer).Flush(); err != nil {
			t.Error(err)
		}
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(upstream.Close)
	proxy := openProxy(t, upstream.URL, config.ProviderKeys{}, "first", "second")
	grant := grantFor(t, proxy)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, grant.BaseURL+"/v1/messages", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Api-Key", grant.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("read stream: %v", readErr)
	}
	if !strings.Contains(string(data), "data: first") {
		t.Fatalf("client got %q, want the first chunk", data)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 1 || keys[0] != "first" {
		t.Fatalf("upstream calls with keys %v, want exactly one call with the first key", keys)
	}
}

func TestBufferedBodiesShareOneBudget(t *testing.T) {
	upstream := httptest.NewServer(&stubUpstream{respond: func(string) (int, string) { return http.StatusOK, "ok" }})
	t.Cleanup(upstream.Close)
	proxy := openProxy(t, upstream.URL, config.ProviderKeys{}, "only")
	proxy.budget.limit = 100
	grant := grantFor(t, proxy)
	if status, _ := post(t, grant, strings.Repeat("x", 150)); status != http.StatusServiceUnavailable {
		t.Fatalf("body over the budget: status %d, want 503", status)
	}
	if status, _ := post(t, grant, strings.Repeat("x", 50)); status != http.StatusOK {
		t.Fatalf("body within the budget: status %d, want 200", status)
	}
	waitForReleasedBudget(t, proxy)
}

func waitForReleasedBudget(t *testing.T, proxy *Proxy) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		proxy.budget.mu.Lock()
		used := proxy.budget.used
		proxy.budget.mu.Unlock()
		if used == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("budget still holds %d bytes after the requests ended", used)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
