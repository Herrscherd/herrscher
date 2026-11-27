package forgecreds

import (
	"sync"
	"testing"
	"time"
)

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
	blockWrites(t, setup.creds.store)

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

	allowWrites(t, setup.creds.store)
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
	blockWrites(t, first.creds.store)
	held, err := tokenSource(t, first.creds, "github.com")
	if err != nil || held.Secret() != "ghu_fresh" {
		t.Fatalf("Token with the write refused = %v, %v; want the renewed token kept in memory", held.Secret(), err)
	}
	stored, _, err := first.creds.store.Get(mustHost(t, "github.com"))
	if err != nil || stored.RefreshToken != "ghr_old" {
		t.Fatalf("file after the refused write = %+v, %v; want the old pair", stored, err)
	}

	allowWrites(t, first.creds.store)
	other := NewStore(dir)
	if err = other.Put(t.Context(), mustHost(t, "github.com"), Record{
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

func TestHeldRenewalIsNotServedOverANewerPairThatNeedsItsOwnRefresh(t *testing.T) {
	server := newSequencedRefreshServer(t,
		`{"access_token":"ghu_fresh","refresh_token":"ghr_fresh","expires_in":28800}`,
		`{"access_token":"ghu_fresh2","refresh_token":"ghr_fresh2","expires_in":28800}`,
	)
	dir := t.TempDir()
	first := newCredentialsIn(t, dir, server.URL)
	mustPut(t, first.creds, Record{
		AccessToken:     "ghu_stale",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_old",
	})
	blockWrites(t, first.creds.store)
	held, err := tokenSource(t, first.creds, "github.com")
	if err != nil || held.Secret() != "ghu_fresh" {
		t.Fatalf("Token with the write refused = %v, %v; want the renewed token kept in memory", held.Secret(), err)
	}

	allowWrites(t, first.creds.store)
	other := NewStore(dir)
	if err = other.Put(t.Context(), mustHost(t, "github.com"), Record{
		AccessToken:     "ghu_other",
		AccessExpiresAt: fixedNow().Add(2 * time.Minute),
		RefreshToken:    "ghr_other",
	}); err != nil {
		t.Fatalf("Put by the other process: %v", err)
	}

	token, err := tokenSource(t, first.creds, "github.com")
	if err != nil || token.Secret() != "ghu_fresh2" {
		t.Fatalf("Token = %v, %v; want ghu_fresh2 renewed from the newer pair", token.Secret(), err)
	}
	if hits := server.hitCount(); hits != 2 {
		t.Fatalf("refresh calls = %d, want 2", hits)
	}
}
