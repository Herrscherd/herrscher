package forgecreds

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

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

	allowWrites(t, setup.creds.store)
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
	blockWrites(t, setup.creds.store)
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
