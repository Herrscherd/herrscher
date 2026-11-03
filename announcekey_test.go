package main

import (
	"errors"
	"log/slog"
	"testing"

	transport "github.com/Herrscherd/herrscher-transport"
)

func TestAnnounceKeyFromEnvRefusesUnset(t *testing.T) {
	t.Setenv("HERRSCHER_ANNOUNCE_KEY", "")
	if _, err := announceKeyFromEnv(); !errors.Is(err, errAnnounceKeyUnset) {
		t.Fatalf("unset key = %v, want errAnnounceKeyUnset", err)
	}
}

func TestAnnounceKeyFromEnvRefusesShortSecret(t *testing.T) {
	t.Setenv("HERRSCHER_ANNOUNCE_KEY", "too-short")
	if _, err := announceKeyFromEnv(); !errors.Is(err, transport.ErrWeakAnnounceKey) {
		t.Fatalf("short key = %v, want transport.ErrWeakAnnounceKey", err)
	}
}

func TestAnnounceKeyFromEnvAcceptsLongSecret(t *testing.T) {
	t.Setenv("HERRSCHER_ANNOUNCE_KEY", "0123456789abcdef0123456789abcdef")
	if _, err := announceKeyFromEnv(); err != nil {
		t.Fatalf("valid key = %v, want nil", err)
	}
}

func TestNewResolverRequiresAnnounceKeyWhenRemote(t *testing.T) {
	t.Setenv("HERRSCHER_REMOTE", "memory")
	t.Setenv("HERRSCHER_ANNOUNCE_KEY", "")
	t.Setenv("HERRSCHER_TLS_CA", "")
	t.Setenv("HERRSCHER_TLS_CERT", "")
	t.Setenv("HERRSCHER_TLS_KEY", "")
	if _, err := newResolver(slog.New(slog.DiscardHandler)); !errors.Is(err, errAnnounceKeyUnset) {
		t.Fatalf("remote without key = %v, want errAnnounceKeyUnset", err)
	}
}

func TestNewResolverNeedsNoAnnounceKeyWhenLocal(t *testing.T) {
	t.Setenv("HERRSCHER_REMOTE", "")
	t.Setenv("HERRSCHER_ANNOUNCE_KEY", "")
	t.Setenv("HERRSCHER_TLS_CA", "")
	t.Setenv("HERRSCHER_TLS_CERT", "")
	t.Setenv("HERRSCHER_TLS_KEY", "")
	if _, err := newResolver(slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("local resolver = %v, want nil", err)
	}
}

func TestNewResolverRefusesAWeakAnnounceKeyWhenRemote(t *testing.T) {
	t.Setenv("HERRSCHER_REMOTE", "memory")
	t.Setenv("HERRSCHER_ANNOUNCE_KEY", "too-short")
	t.Setenv("HERRSCHER_TLS_CA", "")
	t.Setenv("HERRSCHER_TLS_CERT", "")
	t.Setenv("HERRSCHER_TLS_KEY", "")
	if _, err := newResolver(slog.New(slog.DiscardHandler)); !errors.Is(err, transport.ErrWeakAnnounceKey) {
		t.Fatalf("remote with a weak key = %v, want transport.ErrWeakAnnounceKey", err)
	}
}
