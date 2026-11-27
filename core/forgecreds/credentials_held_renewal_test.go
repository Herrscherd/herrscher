package forgecreds

import (
	"bytes"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestUnfinishedRenewalWarnsOncePerEpisode(t *testing.T) {
	setup, _ := renewWithFailingSave(t)
	host := mustHost(t, "github.com")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	tokenWhileRenewalLockHeld(t, setup.creds, host, 2)
	if n := countWarnings(logs.String(), "forge renewal not finished"); n != 1 {
		t.Fatalf("unfinished warnings for two calls in one episode = %d, want 1: %s", n, logs.String())
	}

	if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("Token with the file still failing to save: %v", err)
	}
	tokenWhileRenewalLockHeld(t, setup.creds, host, 1)
	if n := countWarnings(logs.String(), "forge renewal not finished"); n != 1 {
		t.Fatalf("unfinished warnings after the held renewal was replaced in the same episode = %d, want 1: %s", n, logs.String())
	}

	allowWrites(t, setup.creds.store)
	if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("Token once the file is writable: %v", err)
	}
	if stored := storedRecord(t, setup.creds); stored.RefreshToken != "ghr_fresh" {
		t.Fatalf("renewed pair not saved once writable: %+v", stored)
	}
	if _, found := setup.creds.heldFor(host); found {
		t.Fatalf("held renewal kept after a successful save")
	}

	mustPut(t, setup.creds, Record{AccessToken: "ghu_near", AccessExpiresAt: fixedNow().Add(2 * time.Minute), RefreshToken: "ghr_next"})
	blockWrites(t, setup.creds.store)
	if _, err := tokenSource(t, setup.creds, "github.com"); err != nil {
		t.Fatalf("Token with a new failed write: %v", err)
	}
	tokenWhileRenewalLockHeld(t, setup.creds, host, 2)
	if n := countWarnings(logs.String(), "forge renewal not finished"); n != 2 {
		t.Fatalf("unfinished warnings after a new episode = %d, want 2: %s", n, logs.String())
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

func TestHeldRenewalIsSavedOnceTheFileIsReadableAgain(t *testing.T) {
	requireEnforcedModes(t)
	setup, server := renewWithFailingSave(t)
	allowWrites(t, setup.creds.store)
	setFileMode(t, setup.creds.store.Path(), 0o000)

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil || token.Secret() != "ghu_fresh" {
		t.Fatalf("Token with an unreadable file = %v, %v; want the renewed token", token.Secret(), err)
	}
	setFileMode(t, setup.creds.store.Path(), 0o600)
	token, err = tokenSource(t, setup.creds, "github.com")
	if err != nil || token.Secret() != "ghu_fresh" {
		t.Fatalf("Token once the file is readable = %v, %v; want the renewed token", token.Secret(), err)
	}
	if stored := storedRecord(t, setup.creds); stored.RefreshToken != "ghr_fresh" {
		t.Fatalf("the held pair was not saved again under the lock: %+v", stored)
	}
	if hits := server.hitCount(); hits != 1 {
		t.Fatalf("refresh calls = %d, want 1", hits)
	}
}

func TestExpiredHeldRenewalIsNotServedWhenTheFileCannotBeRead(t *testing.T) {
	requireEnforcedModes(t)
	setup, _ := renewWithFailingSave(t)
	setFileMode(t, setup.creds.store.Path(), 0o000)
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
