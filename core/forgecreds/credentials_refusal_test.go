package forgecreds

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestWarningsNeverCarryTheTokens(t *testing.T) {
	server := newRefreshServer(t, `{"access_token":"ghu_ACCESS_LOG_SECRET","refresh_token":"ghr_REFRESH_LOG_SECRET","expires_in":28800}`)
	setup := newTestCredentials(t, server.URL)
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_stale",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_old",
	})
	blockWrites(t, setup.creds.store)
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
