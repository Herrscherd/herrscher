package forgecreds

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

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
