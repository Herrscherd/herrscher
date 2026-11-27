package forgecreds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

type fakeCLI struct {
	mu    sync.Mutex
	token string
	err   error
	calls int
	hosts []string
}

func (f *fakeCLI) run(_ context.Context, host contracts.ForgeHost) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.hosts = append(f.hosts, host.String())
	return f.token, f.err
}

func (f *fakeCLI) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type testSetup struct {
	creds *Credentials
	env   map[string]string
	cli   *fakeCLI
	base  string
}

func newTestCredentials(t *testing.T, base string) testSetup {
	t.Helper()
	return newCredentialsIn(t, t.TempDir(), base)
}

func newCredentialsIn(t *testing.T, dir, base string) testSetup {
	t.Helper()
	setup := testSetup{env: make(map[string]string), cli: &fakeCLI{}, base: base}
	creds := New(filepath.Join(dir, storeFileName))
	creds.flow.wait = func(context.Context, time.Duration) error { return nil }
	creds.flow.now = fixedNow
	creds.base = func(contracts.ForgeHost) string { return base }
	creds.env = func(name string) (string, bool) {
		value, ok := setup.env[name]
		return value, ok
	}
	creds.gitHub = setup.cli.run
	creds.now = fixedNow
	setup.creds = creds
	return setup
}

func tokenSource(t *testing.T, creds *Credentials, host string) (contracts.ForgeToken, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return creds.Token(ctx, mustHost(t, host))
}

func storedRecord(t *testing.T, creds *Credentials) Record {
	t.Helper()
	record, found, err := creds.Store().Get(mustHost(t, "github.com"))
	if err != nil || !found {
		t.Fatalf("Get: found %v, err %v", found, err)
	}
	return record
}

func mustPut(t *testing.T, creds *Credentials, record Record) {
	t.Helper()
	if err := creds.Store().Put(t.Context(), mustHost(t, "github.com"), record); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

type refreshServer struct {
	*httptest.Server

	mu   sync.Mutex
	hits int
}

func newRefreshServer(t *testing.T, reply string) *refreshServer {
	t.Helper()
	return newSequencedRefreshServer(t, reply)
}

func newSequencedRefreshServer(t *testing.T, replies ...string) *refreshServer {
	t.Helper()
	server := &refreshServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.mu.Lock()
		reply := replies[min(server.hits, len(replies)-1)]
		server.hits++
		server.mu.Unlock()
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		writeReply(t, w, reply)
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *refreshServer) hitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

const (
	slowHostName = "ghe.example.com"
	fastHostName = "ghe.example.org"
)

type gatedServer struct {
	*httptest.Server

	entered     chan struct{}
	release     chan struct{}
	hits        atomic.Int64
	enteredOnce sync.Once
	releaseOnce sync.Once
}

func newGatedRefreshServer(t *testing.T, reply string) *gatedServer {
	t.Helper()
	gate := &gatedServer{entered: make(chan struct{}), release: make(chan struct{})}
	gate.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gate.hits.Add(1)
		gate.enteredOnce.Do(func() { close(gate.entered) })
		select {
		case <-gate.release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeReply(t, w, reply)
	}))
	t.Cleanup(gate.Close)
	t.Cleanup(gate.open)
	return gate
}

func (g *gatedServer) hitCount() int64 {
	return g.hits.Load()
}

func (g *gatedServer) open() {
	g.releaseOnce.Do(func() { close(g.release) })
}

func (g *gatedServer) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the renewal never reached the forge")
	}
}

func putRenewable(t *testing.T, store *Store, host string, record Record) {
	t.Helper()
	if err := store.Put(t.Context(), mustHost(t, host), record); err != nil {
		t.Fatalf("Put %s: %v", host, err)
	}
}

func renewWithFailingSave(t *testing.T) (testSetup, *refreshServer) {
	t.Helper()
	server := newRefreshServer(t, `{"access_token":"ghu_fresh","refresh_token":"ghr_fresh","expires_in":28800}`)
	setup := newTestCredentials(t, server.URL)
	mustPut(t, setup.creds, Record{AccessToken: "ghu_stale", AccessExpiresAt: fixedNow().Add(2 * time.Minute), RefreshToken: "ghr_old"})
	blockWrites(t, setup.creds.store)
	if token, err := tokenSource(t, setup.creds, "github.com"); err != nil || token.Secret() != "ghu_fresh" {
		t.Fatalf("first Token = %v, %v; want the renewed token", token.Secret(), err)
	}
	return setup, server
}

type movingClock struct {
	now time.Time
}

func (c *movingClock) read() time.Time {
	return c.now
}

func countWarnings(logs, fragment string) int {
	count := 0
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "level=WARN") && strings.Contains(line, fragment) {
			count++
		}
	}
	return count
}

func tokenWhileRenewalLockHeld(t *testing.T, creds *Credentials, host contracts.ForgeHost, calls int) {
	t.Helper()
	release, err := filelock.Acquire(t.Context(), creds.store.renewLockPath(host))
	if err != nil {
		t.Fatalf("hold the renewal lock: %v", err)
	}
	defer release()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for range calls {
		token, tokenErr := creds.Token(cancelled, host)
		if tokenErr != nil || token.Secret() != "ghu_fresh" {
			t.Fatalf("Token with the renewal lock not taken = %v, %v; want the renewed token kept in memory", token.Secret(), tokenErr)
		}
	}
}
