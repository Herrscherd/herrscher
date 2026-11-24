package forgecreds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestInvalidateMarksTheServedSavedTokenAndKeepsIt(t *testing.T) {
	setup := newTestCredentials(t, "")
	mustPut(t, setup.creds, Record{AccessToken: "ghu_saved"})
	host := mustHost(t, "github.com")
	if _, err := setup.creds.Token(t.Context(), host); err != nil {
		t.Fatalf("Token: %v", err)
	}

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

func TestInvalidateLeavesAnotherHostsSavedRecordAlone(t *testing.T) {
	setup := newTestCredentials(t, "")
	mustPut(t, setup.creds, Record{AccessToken: "ghu_saved"})
	setup.cli.token = "gho_cli"
	host := mustHost(t, "ghe.example.com")
	if _, err := setup.creds.Token(t.Context(), host); err != nil {
		t.Fatalf("Token: %v", err)
	}

	setup.creds.Invalidate(t.Context(), host)

	if _, found, err := setup.creds.Store().Get(mustHost(t, "github.com")); err != nil || !found {
		t.Fatalf("saved record of another host lost: found %v, err %v", found, err)
	}
	if storedRecord(t, setup.creds).Rejected {
		t.Fatalf("a token that was never served from the file was marked refused")
	}
}

func TestInvalidateDoesNotMarkATokenThatWasReplacedAfterServing(t *testing.T) {
	setup := newTestCredentials(t, "")
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_served",
		AccessExpiresAt: fixedNow().Add(time.Hour),
		RefreshToken:    "ghr_old",
	})
	host := mustHost(t, "github.com")
	if _, err := setup.creds.Token(t.Context(), host); err != nil {
		t.Fatalf("Token: %v", err)
	}
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_newer",
		AccessExpiresAt: fixedNow().Add(time.Hour),
		RefreshToken:    "ghr_newer",
	})

	setup.creds.Invalidate(t.Context(), host)

	if storedRecord(t, setup.creds).Rejected {
		t.Fatalf("the newer saved token was marked refused for an older one")
	}
}

func TestSavedTokenNearExpiryIsRefreshedBeforeUse(t *testing.T) {
	server := newRefreshServer(t, `{"access_token":"ghu_fresh","refresh_token":"ghr_fresh","expires_in":28800}`)
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
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls = %d, want 1", hits)
	}
}

func TestSavedTokenFarFromExpiryIsNotRefreshed(t *testing.T) {
	server := newRefreshServer(t, `{"access_token":"ghu_other"}`)
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
	if hits := server.hitCount(); hits != 0 {
		t.Fatalf("refresh calls = %d for a token far from expiry, want 0", hits)
	}
}

func TestTwoCredentialsSharingTheDirectorySpendTheRefreshTokenOnce(t *testing.T) {
	server := newRefreshServer(t, `{"access_token":"ghu_fresh","refresh_token":"ghr_fresh","expires_in":28800}`)
	dir := t.TempDir()
	first := newCredentialsIn(t, dir, server.URL)
	second := newCredentialsIn(t, dir, server.URL)
	mustPut(t, first.creds, Record{
		AccessToken:     "ghu_stale",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_old",
	})

	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, setup := range []testSetup{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := setup.creds.Token(t.Context(), mustHost(t, "github.com"))
			if err != nil {
				results <- "error: " + err.Error()
				return
			}
			results <- token.Secret()
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got != "ghu_fresh" {
			t.Fatalf("a credential served %q, want ghu_fresh", got)
		}
	}
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls = %d for two processes, want 1", hits)
	}
}

func TestRenewalIsKeptInMemoryWhenTheFileCannotBeWritten(t *testing.T) {
	server := newRefreshServer(t, `{"access_token":"ghu_fresh","refresh_token":"ghr_fresh","expires_in":28800}`)
	setup := newTestCredentials(t, server.URL)
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_stale",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_old",
	})
	setup.creds.store.rename = func(string, string) error { return errors.New("injected rename failure") }

	for range 2 {
		token, err := tokenSource(t, setup.creds, "github.com")
		if err != nil {
			t.Fatalf("Token while the file cannot be written: %v", err)
		}
		if token.Secret() != "ghu_fresh" {
			t.Fatalf("Token = %q, want the renewed token kept in memory", token.Secret())
		}
	}
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls = %d, want 1: the refresh token was spent again", hits)
	}
	if stored := storedRecord(t, setup.creds); stored.RefreshToken != "ghr_old" {
		t.Fatalf("file changed by a failed write: %+v", stored)
	}

	setup.creds.store.rename = os.Rename
	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil || token.Secret() != "ghu_fresh" {
		t.Fatalf("Token after the file is writable again = %v, %v", token.Secret(), err)
	}
	if stored := storedRecord(t, setup.creds); stored.RefreshToken != "ghr_fresh" {
		t.Fatalf("renewed pair not saved once writable: %+v", stored)
	}
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls = %d after saving, want 1", hits)
	}
}

func TestHeldRenewalIsDroppedWhenAnotherProcessSavedANewerPair(t *testing.T) {
	server := newRefreshServer(t, `{"access_token":"ghu_fresh","refresh_token":"ghr_fresh","expires_in":28800}`)
	dir := t.TempDir()
	first := newCredentialsIn(t, dir, server.URL)
	mustPut(t, first.creds, Record{
		AccessToken:     "ghu_stale",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_old",
	})
	first.creds.store.rename = func(string, string) error { return errors.New("injected rename failure") }
	if _, err := tokenSource(t, first.creds, "github.com"); err != nil {
		t.Fatalf("Token: %v", err)
	}

	other := NewStore(dir)
	if err := other.Put(t.Context(), mustHost(t, "github.com"), Record{
		AccessToken:     "ghu_other",
		AccessExpiresAt: fixedNow().Add(time.Hour),
		RefreshToken:    "ghr_other",
	}); err != nil {
		t.Fatalf("Put by the other process: %v", err)
	}

	token, err := tokenSource(t, first.creds, "github.com")
	if err != nil || token.Secret() != "ghu_other" {
		t.Fatalf("Token = %v, %v; want the newer pair saved by the other process", token.Secret(), err)
	}
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls = %d, want 1", hits)
	}
}

func TestWarningsNeverCarryTheTokens(t *testing.T) {
	server := newRefreshServer(t, `{"access_token":"ghu_ACCESS_LOG_SECRET","refresh_token":"ghr_REFRESH_LOG_SECRET","expires_in":28800}`)
	setup := newTestCredentials(t, server.URL)
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_stale",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_old",
	})
	setup.creds.store.rename = func(string, string) error { return errors.New("injected rename failure") }
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("Token: %v", err)
	}
	if logs.Len() == 0 {
		t.Fatalf("no warning was logged for the file that could not be written")
	}
	for _, secret := range []string{"ghu_ACCESS_LOG_SECRET", "ghr_REFRESH_LOG_SECRET"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("the log carries a token: %s", logs.String())
		}
	}
}

func TestFailedRefreshWhileTokenStillValidLogsNoToken(t *testing.T) {
	forge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(forge.Close)
	setup := newTestCredentials(t, forge.URL)
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_STILL_VALID_LOG_SECRET",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_LOG_SECRET",
	})
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Secret() != "ghu_STILL_VALID_LOG_SECRET" {
		t.Fatalf("Token = %q, want the saved token that is still valid", token.Secret())
	}
	if !strings.Contains(logs.String(), "refresh the saved forge token") {
		t.Fatalf("no warning was logged for the failed refresh: %s", logs.String())
	}
	for _, secret := range []string{"ghu_STILL_VALID_LOG_SECRET", "ghr_LOG_SECRET"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("the log carries a token: %s", logs.String())
		}
	}
}

func TestRefusedRefreshMarksTheSavedTokenAndFallsBack(t *testing.T) {
	server := newRefreshServer(t, `{"error":"bad_refresh_token"}`)
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
	if record := storedRecord(t, setup.creds); !record.Rejected {
		t.Fatalf("saved record after a refused refresh = %+v, want Rejected", record)
	}
}

func TestMisconfiguredAppIsNotAMarkedRefusal(t *testing.T) {
	server := newRefreshServer(t, `{"error":"incorrect_client_credentials"}`)
	setup := newTestCredentials(t, server.URL)
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_stale",
		AccessExpiresAt: fixedNow().Add(-time.Minute),
		RefreshToken:    "ghr_fine",
	})

	_, err := tokenSource(t, setup.creds, "github.com")
	if !errors.Is(err, ErrAppMisconfigured) {
		t.Fatalf("Token with a misconfigured app = %v, want ErrAppMisconfigured", err)
	}
	if record := storedRecord(t, setup.creds); record.Rejected {
		t.Fatalf("the token of the user was marked refused for an app error: %+v", record)
	}
}

func TestExpiredRefreshTokenIsNotSentToTheForge(t *testing.T) {
	server := newRefreshServer(t, `{"access_token":"ghu_never"}`)
	setup := newTestCredentials(t, server.URL)
	setup.cli.token = "gho_cli"
	mustPut(t, setup.creds, Record{
		AccessToken:      "ghu_old",
		AccessExpiresAt:  fixedNow().Add(-time.Hour),
		RefreshToken:     "ghr_old",
		RefreshExpiresAt: fixedNow().Add(-time.Minute),
	})

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceCLI {
		t.Fatalf("Token with an expired refresh token = source %v, want the gh fallback", token.Source())
	}
	if hits := server.hitCount(); hits != 0 {
		t.Fatalf("the forge was asked %d times with an expired refresh token, want 0", hits)
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
	server := &refreshServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.mu.Lock()
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

func TestSlowRenewalOfOneHostDoesNotHoldAnotherHost(t *testing.T) {
	slow := newGatedRefreshServer(t, `{"access_token":"ghu_slow","refresh_token":"ghr_slow","expires_in":28800}`)
	fast := newRefreshServer(t, `{"access_token":"ghu_fast","refresh_token":"ghr_fast","expires_in":28800}`)
	setup := newTestCredentials(t, fast.URL)
	setup.creds.base = func(host contracts.ForgeHost) string {
		if host.String() == slowHostName {
			return slow.URL
		}
		return fast.URL
	}
	nearExpiry := Record{AccessToken: "ghu_old", AccessExpiresAt: fixedNow().Add(2 * time.Minute), RefreshToken: "ghr_old"}
	putRenewable(t, setup.creds.Store(), slowHostName, nearExpiry)
	putRenewable(t, setup.creds.Store(), fastHostName, nearExpiry)

	slowResult := make(chan error, 1)
	go func() {
		_, err := setup.creds.Token(t.Context(), mustHost(t, slowHostName))
		slowResult <- err
	}()
	slow.waitEntered(t)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	token, err := setup.creds.Token(ctx, mustHost(t, fastHostName))
	if err != nil {
		t.Fatalf("Token of another host while one renewal is stuck: %v", err)
	}
	if token.Secret() != "ghu_fast" {
		t.Fatalf("Token of the other host = %q, want ghu_fast", token.Secret())
	}

	slow.open()
	if err := <-slowResult; err != nil {
		t.Fatalf("the stuck renewal, once released: %v", err)
	}
}

func TestRenewalLockNameIsPerHostAndPathFree(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	withPort := store.renewLockPath(mustHost(t, "ghe.example.com:8443"))
	plain := store.renewLockPath(mustHost(t, "ghe.example.com"))
	if withPort == plain {
		t.Fatalf("two hosts share the renewal lock %q", plain)
	}
	for _, path := range []string{withPort, plain} {
		if filepath.Dir(path) != dir {
			t.Fatalf("renewal lock %q is not beside the connections file", path)
		}
		if strings.ContainsAny(filepath.Base(path), `:/\`) {
			t.Fatalf("renewal lock name %q carries a path or port character", filepath.Base(path))
		}
		if path == store.lockPath() {
			t.Fatalf("renewal lock %q is the shared lock", path)
		}
	}
}

func TestRenewalDoesNotOverwritePairSavedMeanwhile(t *testing.T) {
	gate := newGatedRefreshServer(t, `{"access_token":"ghu_renewed","refresh_token":"ghr_renewed","expires_in":28800}`)
	dir := t.TempDir()
	setup := newCredentialsIn(t, dir, gate.URL)
	mustPut(t, setup.creds, Record{AccessToken: "ghu_stale", AccessExpiresAt: fixedNow().Add(2 * time.Minute), RefreshToken: "ghr_old"})

	result := make(chan string, 1)
	go func() {
		token, err := setup.creds.Token(t.Context(), mustHost(t, "github.com"))
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		result <- token.Secret()
	}()
	gate.waitEntered(t)
	connected := Record{AccessToken: "ghu_connected", AccessExpiresAt: fixedNow().Add(time.Hour), RefreshToken: "ghr_connected"}
	if err := NewStore(dir).Put(t.Context(), mustHost(t, "github.com"), connected); err != nil {
		t.Fatalf("Put by a connect in the meantime: %v", err)
	}
	gate.open()

	if got := <-result; got != "ghu_connected" {
		t.Fatalf("Token = %q, want the pair saved meanwhile", got)
	}
	if stored := storedRecord(t, setup.creds); stored.RefreshToken != "ghr_connected" {
		t.Fatalf("the renewal overwrote the pair saved meanwhile: %+v", stored)
	}
}

func TestLogoutDuringRenewalIsNotUndone(t *testing.T) {
	gate := newGatedRefreshServer(t, `{"access_token":"ghu_renewed","refresh_token":"ghr_renewed","expires_in":28800}`)
	dir := t.TempDir()
	setup := newCredentialsIn(t, dir, gate.URL)
	mustPut(t, setup.creds, Record{AccessToken: "ghu_stale", AccessExpiresAt: fixedNow().Add(2 * time.Minute), RefreshToken: "ghr_old"})

	result := make(chan error, 1)
	go func() {
		_, err := setup.creds.Token(t.Context(), mustHost(t, "github.com"))
		result <- err
	}()
	gate.waitEntered(t)
	removed, err := NewStore(dir).Delete(t.Context(), mustHost(t, "github.com"))
	if err != nil || !removed {
		t.Fatalf("logout during the renewal: removed %v, err %v", removed, err)
	}
	gate.open()

	if err := <-result; !errors.Is(err, contracts.ErrForgeNotConnected) {
		t.Fatalf("Token after logout = %v, want ErrForgeNotConnected", err)
	}
	if _, found, err := NewStore(dir).Get(mustHost(t, "github.com")); err != nil || found {
		t.Fatalf("logout undone by the renewal: found %v, err %v", found, err)
	}
}

func TestMisconfiguredAppKeepsServingAValidTokenWithAWarning(t *testing.T) {
	server := newRefreshServer(t, `{"error":"incorrect_client_credentials"}`)
	setup := newTestCredentials(t, server.URL)
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_still",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_fine",
	})
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil || token.Secret() != "ghu_still" {
		t.Fatalf("Token with a misconfigured app and a valid token = %v, %v; want the valid token", token.Secret(), err)
	}
	if !setup.creds.AppMisconfigured(mustHost(t, "github.com")) {
		t.Fatalf("AppMisconfigured = false after the forge refused the app credentials")
	}
	if !strings.Contains(logs.String(), "refresh the saved forge token") {
		t.Fatalf("no warning was logged for the misconfigured app: %s", logs.String())
	}
	for _, secret := range []string{"ghu_still", "ghr_fine"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("the log carries a token: %s", logs.String())
		}
	}
	if stored := storedRecord(t, setup.creds); stored.Rejected || stored.RefreshToken != "ghr_fine" {
		t.Fatalf("the record changed for an app error: %+v", stored)
	}
}

func failOnce(read func(string) ([]byte, error)) func(string) ([]byte, error) {
	var failed bool
	return func(path string) ([]byte, error) {
		if !failed {
			failed = true
			return nil, errors.New("injected read failure")
		}
		return read(path)
	}
}

func renewWithFailingSave(t *testing.T) (testSetup, *refreshServer) {
	t.Helper()
	server := newRefreshServer(t, `{"access_token":"ghu_fresh","refresh_token":"ghr_fresh","expires_in":28800}`)
	setup := newTestCredentials(t, server.URL)
	mustPut(t, setup.creds, Record{AccessToken: "ghu_stale", AccessExpiresAt: fixedNow().Add(2 * time.Minute), RefreshToken: "ghr_old"})
	setup.creds.store.rename = func(string, string) error { return errors.New("injected rename failure") }
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

func TestMisconfiguredAppIsNotAskedAgainWithinTheRetryDelay(t *testing.T) {
	server := newRefreshServer(t, `{"error":"incorrect_client_credentials"}`)
	setup := newTestCredentials(t, server.URL)
	clock := &movingClock{now: fixedNow()}
	setup.creds.now = clock.read
	setup.creds.flow.now = clock.read
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_still",
		AccessExpiresAt: clock.now.Add(2 * time.Minute),
		RefreshToken:    "ghr_fine",
	})

	for range 2 {
		token, err := tokenSource(t, setup.creds, "github.com")
		if err != nil || token.Secret() != "ghu_still" {
			t.Fatalf("Token within the retry delay = %v, %v; want the valid token served", token.Secret(), err)
		}
		clock.now = clock.now.Add(10 * time.Second)
	}
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls within the retry delay = %d, want 1", hits)
	}

	clock.now = clock.now.Add(time.Minute)
	if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("Token after the retry delay: %v", err)
	}
	if hits := server.hitCount(); hits != 2 {
		t.Fatalf("refresh calls after the retry delay = %d, want 2", hits)
	}
}

func TestMisconfiguredAppWithinTheRetryDelayFailsOnceTheTokenExpired(t *testing.T) {
	server := newRefreshServer(t, `{"error":"incorrect_client_credentials"}`)
	setup := newTestCredentials(t, server.URL)
	clock := &movingClock{now: fixedNow()}
	setup.creds.now = clock.read
	setup.creds.flow.now = clock.read
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_short",
		AccessExpiresAt: clock.now.Add(30 * time.Second),
		RefreshToken:    "ghr_fine",
	})

	if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("first Token with a valid token: %v", err)
	}
	clock.now = clock.now.Add(40 * time.Second)
	_, err := tokenSource(t, setup.creds, "github.com")
	if !errors.Is(err, ErrAppMisconfigured) {
		t.Fatalf("Token within the retry delay after expiry = %v, want ErrAppMisconfigured", err)
	}
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls within the retry delay = %d, want 1", hits)
	}
}

func TestUnsavedRenewalWarnsOncePerEpisode(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	setup, server := renewWithFailingSave(t)
	for range 2 {
		token, err := tokenSource(t, setup.creds, "github.com")
		if err != nil || token.Secret() != "ghu_fresh" {
			t.Fatalf("Token with a failed write = %v, %v; want the renewed token kept in memory", token.Secret(), err)
		}
	}
	if n := countWarnings(logs.String(), "forge connections not saved"); n != 1 {
		t.Fatalf("not saved warnings for failed writes in one episode = %d, want 1: %s", n, logs.String())
	}

	setup.creds.store.rename = os.Rename
	if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("Token once the file is writable: %v", err)
	}
	if stored := storedRecord(t, setup.creds); stored.RefreshToken != "ghr_fresh" {
		t.Fatalf("renewed pair not saved once writable: %+v", stored)
	}

	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_near",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_next",
	})
	setup.creds.store.rename = func(string, string) error { return errors.New("injected rename failure") }
	if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("Token with a new failed write: %v", err)
	}
	if n := countWarnings(logs.String(), "forge connections not saved"); n != 2 {
		t.Fatalf("not saved warnings after a successful write and a new failure = %d, want 2: %s", n, logs.String())
	}
	if hits := server.hitCount(); hits != 2 {
		t.Fatalf("refresh calls = %d, want 2 (one per renewal, none spent twice)", hits)
	}
}

func TestMisconfiguredAppWarnsOncePerRetryWindow(t *testing.T) {
	server := newRefreshServer(t, `{"error":"incorrect_client_credentials"}`)
	setup := newTestCredentials(t, server.URL)
	clock := &movingClock{now: fixedNow()}
	setup.creds.now = clock.read
	setup.creds.flow.now = clock.read
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_still",
		AccessExpiresAt: clock.now.Add(2 * time.Minute),
		RefreshToken:    "ghr_fine",
	})
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	for range 2 {
		if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
			t.Fatalf("Token within the retry delay: %v", err)
		}
		clock.now = clock.now.Add(10 * time.Second)
	}
	if n := countWarnings(logs.String(), "refresh the saved forge token"); n != 1 {
		t.Fatalf("refresh warnings within one retry window = %d, want 1: %s", n, logs.String())
	}

	clock.now = clock.now.Add(time.Minute)
	if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("Token after the retry delay: %v", err)
	}
	if n := countWarnings(logs.String(), "refresh the saved forge token"); n != 2 {
		t.Fatalf("refresh warnings after a new failed attempt = %d, want 2: %s", n, logs.String())
	}
}

func TestUnreadableFileWarnsOnceWhileTheKeptPairIsServed(t *testing.T) {
	setup, _ := renewWithFailingSave(t)
	path := setup.creds.store.Path()
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read connections file: %v", err)
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if err = os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		token, tokenErr := tokenSource(t, setup.creds, "github.com")
		if tokenErr != nil || token.Secret() != "ghu_fresh" {
			t.Fatalf("Token with a corrupt file = %v, %v; want the renewed token kept in memory", token.Secret(), tokenErr)
		}
	}
	if n := countWarnings(logs.String(), "not readable"); n != 1 {
		t.Fatalf("unreadable warnings for three calls = %d, want 1: %s", n, logs.String())
	}

	if err = os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	host := mustHost(t, "github.com")
	release, err := filelock.Acquire(t.Context(), setup.creds.store.renewLockPath(host))
	if err != nil {
		t.Fatalf("hold the renewal lock: %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	token, err := setup.creds.Token(cancelled, host)
	release()
	if err != nil || token.Secret() != "ghu_fresh" {
		t.Fatalf("Token with the file readable and the renewal lock not taken = %v, %v; want the renewed token kept in memory", token.Secret(), err)
	}
	if err = os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("Token with the file corrupt again: %v", err)
	}
	if n := countWarnings(logs.String(), "not readable"); n != 2 {
		t.Fatalf("unreadable warnings after a new episode = %d, want 2: %s", n, logs.String())
	}
}

func TestHeldRenewalIsServedWhenTheFileBecomesCorrupt(t *testing.T) {
	setup, server := renewWithFailingSave(t)
	if err := os.WriteFile(setup.creds.store.Path(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil || token.Secret() != "ghu_fresh" {
		t.Fatalf("Token with a corrupt file = %v, %v; want the renewed token kept in memory", token.Secret(), err)
	}
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls = %d, want 1: the refresh token was spent again", hits)
	}
}

func TestHeldRenewalIsSavedAgainWhenTheFileCannotBeReadOnce(t *testing.T) {
	setup, server := renewWithFailingSave(t)
	setup.creds.store.rename = os.Rename
	setup.creds.store.read = failOnce(os.ReadFile)

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil || token.Secret() != "ghu_fresh" {
		t.Fatalf("Token with one unreadable read = %v, %v; want the renewed token", token.Secret(), err)
	}
	if stored := storedRecord(t, setup.creds); stored.RefreshToken != "ghr_fresh" {
		t.Fatalf("the held pair was not saved again under the lock: %+v", stored)
	}
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls = %d, want 1", hits)
	}
}

func TestExpiredHeldRenewalIsNotServedWhenTheFileCannotBeRead(t *testing.T) {
	setup, _ := renewWithFailingSave(t)
	setup.creds.store.read = func(string) ([]byte, error) { return nil, errors.New("injected read failure") }
	setup.creds.now = func() time.Time { return fixedNow().Add(9 * time.Hour) }

	token, err := tokenSource(t, setup.creds, "github.com")
	if err == nil {
		t.Fatalf("Token with an expired renewed token and an unreadable file = %q, want an error", token.Secret())
	}
}

func TestSecondProcessWaitsForTheRenewalInsteadOfSpendingTheTokenTwice(t *testing.T) {
	gate := newGatedRefreshServer(t, `{"access_token":"ghu_renewed","refresh_token":"ghr_renewed","expires_in":28800}`)
	dir := t.TempDir()
	first := newCredentialsIn(t, dir, gate.URL)
	second := newCredentialsIn(t, dir, gate.URL)
	mustPut(t, first.creds, Record{AccessToken: "ghu_stale", AccessExpiresAt: fixedNow().Add(2 * time.Minute), RefreshToken: "ghr_old"})

	results := make(chan string, 2)
	serve := func(creds *Credentials) {
		token, err := creds.Token(t.Context(), mustHost(t, "github.com"))
		if err != nil {
			results <- "error: " + err.Error()
			return
		}
		results <- token.Secret()
	}
	go serve(first.creds)
	gate.waitEntered(t)
	go serve(second.creds)
	select {
	case <-time.After(200 * time.Millisecond):
	case got := <-results:
		t.Fatalf("a credential answered %q while the renewal was in flight", got)
	}
	if hits := gate.hitCount(); hits != 1 {
		t.Fatalf("refresh requests while the first renewal is in flight = %d, want 1", hits)
	}

	gate.open()
	for range 2 {
		if got := <-results; got != "ghu_renewed" {
			t.Fatalf("a credential served %q, want ghu_renewed", got)
		}
	}
	if hits := gate.hitCount(); hits != 1 {
		t.Fatalf("refresh calls = %d for two processes, want 1", hits)
	}
}
