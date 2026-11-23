package forgecreds

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type fakeCLI struct {
	mu    sync.Mutex
	token string
	err   error
	calls int
	hosts []string
}

func (f *fakeCLI) run(_ context.Context, host string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.hosts = append(f.hosts, host)
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
	setup := testSetup{env: make(map[string]string), cli: &fakeCLI{}, base: base}
	client := &http.Client{}
	setup.creds = &Credentials{
		store: NewStore(t.TempDir()),
		flow: Flow{
			http:     client,
			clientID: ClientID,
			wait:     func(context.Context, time.Duration) error { return nil },
			now:      fixedNow,
		},
		base: func(contracts.ForgeHost) string { return base },
		env: func(name string) (string, bool) {
			value, ok := setup.env[name]
			return value, ok
		},
		gitHub: setup.cli.run,
		now:    fixedNow,
		cache:  make(map[string]contracts.ForgeToken),
	}
	return setup
}

func tokenSource(t *testing.T, creds *Credentials, host string) (contracts.ForgeToken, error) {
	t.Helper()
	return creds.Token(t.Context(), mustHost(t, host))
}

func TestSavedTokenWinsOverEnvironmentAndCLI(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.env["GH_TOKEN"] = "ghp_env"
	setup.cli.token = "gho_cli"
	mustPut(t, setup.creds, Record{AccessToken: "ghu_saved", Login: "octo"})

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Secret() != "ghu_saved" || token.Source() != contracts.TokenSourceSaved {
		t.Fatalf("Token = source %v, want the saved token", token.Source())
	}
	if setup.cli.callCount() != 0 {
		t.Fatalf("gh was run although a saved token exists")
	}
}

func TestGHTokenIsReadBeforeGitHubTokenForGitHubCom(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.env["GH_TOKEN"] = "  "
	setup.env["GITHUB_TOKEN"] = "ghp_github"

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Secret() != "ghp_github" || token.Source() != contracts.TokenSourceEnv {
		t.Fatalf("Token = %v from %v, want GITHUB_TOKEN from the environment", token.Secret(), token.Source())
	}
	setup.env["GH_TOKEN"] = "ghp_gh"
	setup.creds.cache = make(map[string]contracts.ForgeToken)
	token, err = tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Secret() != "ghp_gh" {
		t.Fatalf("Token = %q, want GH_TOKEN ahead of GITHUB_TOKEN", token.Secret())
	}
}

func TestEnvironmentTokenIsPreferredOverGHCLI(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.env["GH_TOKEN"] = "ghp_env"
	setup.cli.token = "gho_cli"

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceEnv || token.Secret() != "ghp_env" {
		t.Fatalf("Token = source %v, want GH_TOKEN ahead of the gh login", token.Source())
	}
	if setup.cli.callCount() != 0 {
		t.Fatalf("gh was run although GH_TOKEN is set")
	}
}

func TestEnvironmentTokenIsNotSentToAnotherHostUnlessNamedByGHHost(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.env["GH_TOKEN"] = "ghp_env"
	setup.cli.token = "gho_cli"

	token, err := tokenSource(t, setup.creds, "ghe.example.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceCLI || token.Secret() != "gho_cli" {
		t.Fatalf("Token for another host = source %v, want the gh token and not GH_TOKEN", token.Source())
	}

	setup.env["GH_HOST"] = "ghe.example.com"
	setup.creds.cache = make(map[string]contracts.ForgeToken)
	token, err = tokenSource(t, setup.creds, "ghe.example.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceEnv || token.Secret() != "ghp_env" {
		t.Fatalf("Token for the host of GH_HOST = source %v, want GH_TOKEN", token.Source())
	}
}

func TestGHCLIIsTheLastResort(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.cli.token = "gho_cli"

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceCLI || token.Secret() != "gho_cli" {
		t.Fatalf("Token = source %v, want the gh token", token.Source())
	}
	if got := setup.cli.hosts[0]; got != "github.com" {
		t.Fatalf("gh asked for host %q, want github.com", got)
	}
}

func TestNotConnectedWhenNoSourceHoldsAToken(t *testing.T) {
	setup := newTestCredentials(t, "")

	_, err := tokenSource(t, setup.creds, "github.com")
	if !errors.Is(err, contracts.ErrForgeNotConnected) {
		t.Fatalf("Token with no source = %v, want ErrForgeNotConnected", err)
	}
}

func TestRememberedTokenIsReusedUntilInvalidated(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.cli.token = "gho_cli"
	host := mustHost(t, "github.com")

	for range 2 {
		if _, err := setup.creds.Token(t.Context(), host); err != nil {
			t.Fatalf("Token: %v", err)
		}
	}
	if setup.cli.callCount() != 1 {
		t.Fatalf("gh ran %d times for two reads, want 1", setup.cli.callCount())
	}
	setup.creds.Invalidate(t.Context(), host)
	if _, err := setup.creds.Token(t.Context(), host); err != nil {
		t.Fatalf("Token after Invalidate: %v", err)
	}
	if setup.cli.callCount() != 2 {
		t.Fatalf("gh ran %d times after Invalidate, want 2", setup.cli.callCount())
	}
}

func TestInvalidateMarksTheSavedTokenAndKeepsIt(t *testing.T) {
	setup := newTestCredentials(t, "")
	mustPut(t, setup.creds, Record{AccessToken: "ghu_saved"})
	host := mustHost(t, "github.com")

	setup.creds.Invalidate(t.Context(), host)

	record, found, err := setup.creds.Store().Get(host)
	if err != nil || !found {
		t.Fatalf("Get after Invalidate = found %v, err %v; want the record kept", found, err)
	}
	if !record.Rejected || record.AccessToken != "ghu_saved" {
		t.Fatalf("record after Invalidate = %+v, want Rejected and the token kept", record)
	}
	if _, err := setup.creds.Token(t.Context(), host); !errors.Is(err, contracts.ErrForgeNotConnected) {
		t.Fatalf("Token with a refused saved token and no other source = %v, want ErrForgeNotConnected", err)
	}
}

func TestSavedTokenNearExpiryIsRefreshedBeforeUse(t *testing.T) {
	var hits int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		writeReply(t, w, `{"access_token":"ghu_fresh","refresh_token":"ghr_fresh","expires_in":28800}`)
	}))
	t.Cleanup(server.Close)
	setup := newTestCredentials(t, server.URL)
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_stale",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_old",
	})

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Secret() != "ghu_fresh" {
		t.Fatalf("Token = %q, want the refreshed token", token.Secret())
	}
	record := storedRecord(t, setup.creds)
	if record.AccessToken != "ghu_fresh" || record.RefreshToken != "ghr_fresh" {
		t.Fatalf("saved record = %+v, want the refreshed tokens saved", record)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("refresh calls = %d, want 1", hits)
	}
}

func TestSavedTokenFarFromExpiryIsNotRefreshed(t *testing.T) {
	var hits int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		hits++
	}))
	t.Cleanup(server.Close)
	setup := newTestCredentials(t, server.URL)
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_fine",
		AccessExpiresAt: fixedNow().Add(time.Hour),
		RefreshToken:    "ghr_old",
	})

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Secret() != "ghu_fine" {
		t.Fatalf("Token = %q, want the saved token", token.Secret())
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("refresh calls = %d for a token far from expiry, want 0", hits)
	}
}

func TestRefusedRefreshMarksTheSavedTokenAndFallsBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeReply(t, w, `{"error":"bad_refresh_token"}`)
	}))
	t.Cleanup(server.Close)
	setup := newTestCredentials(t, server.URL)
	setup.cli.token = "gho_cli"
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_dead",
		AccessExpiresAt: fixedNow().Add(-time.Minute),
		RefreshToken:    "ghr_dead",
	})

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceCLI {
		t.Fatalf("Token after a refused refresh = source %v, want the gh fallback", token.Source())
	}
	record := storedRecord(t, setup.creds)
	if !record.Rejected {
		t.Fatalf("saved record after a refused refresh = %+v, want Rejected", record)
	}
}

func TestTokenNeverAppearsInAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream trouble", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	setup := newTestCredentials(t, server.URL)
	const access = "ghu_ACCESS_SECRET_VALUE"
	const refresh = "ghr_REFRESH_SECRET_VALUE"
	mustPut(t, setup.creds, Record{
		AccessToken:     access,
		AccessExpiresAt: fixedNow().Add(-time.Minute),
		RefreshToken:    refresh,
	})

	_, err := tokenSource(t, setup.creds, "github.com")
	if err == nil {
		t.Fatalf("Token with a failing refresh returned no error")
	}
	for _, secret := range []string{access, refresh} {
		if strings.Contains(err.Error(), secret) || strings.Contains(fmt.Sprint(err), secret) {
			t.Fatalf("the error %q carries a saved token", err)
		}
	}
}

func TestTokenRendersWithoutItsSecret(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.env["GH_TOKEN"] = "ghp_VISIBLE_IF_LEAKED"

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	for _, rendered := range []string{token.String(), fmt.Sprintf("%#v", token), fmt.Sprintf("%+v", token)} {
		if strings.Contains(rendered, "ghp_VISIBLE_IF_LEAKED") {
			t.Fatalf("token renders as %q", rendered)
		}
	}
}

func TestConnectSavesTheGrantedTokensAndShowsOnlyTheCode(t *testing.T) {
	script := &forgeScript{
		codeReply: deviceCodeReply,
		tokenReplies: []string{
			`{"error":"authorization_pending"}`,
			`{"access_token":"ghu_granted","refresh_token":"ghr_granted","expires_in":28800}`,
		},
	}
	server := newForgeServer(t, script)
	setup := newTestCredentials(t, server.URL)
	host := mustHost(t, "github.com")
	var shown DeviceCode

	err := setup.creds.Connect(t.Context(), host, func(code DeviceCode) error {
		shown = code
		return nil
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if shown.UserCode != "WDJB-MJHT" {
		t.Fatalf("shown code = %+v, want the user code", shown)
	}
	record, found, err := setup.creds.Store().Get(host)
	if err != nil || !found || record.AccessToken != "ghu_granted" {
		t.Fatalf("saved record = %+v, found %v, err %v; want the granted token", record, found, err)
	}
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
	if err := creds.Store().Put(mustHost(t, "github.com"), record); err != nil {
		t.Fatalf("Put: %v", err)
	}
}
