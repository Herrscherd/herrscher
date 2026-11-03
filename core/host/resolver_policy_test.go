package host

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"

	transport "github.com/Herrscherd/herrscher-transport"
)

func TestRejectedAnnouncementsAreLoggedOncePerReasonWithinTheInterval(t *testing.T) {
	var logs bytes.Buffer
	r := NewResolver(nil, "")
	r.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	clock := r.now()
	r.now = func() time.Time { return clock }

	r.warnRejectedAnnouncement(transport.ErrAddrRefused)
	r.warnRejectedAnnouncement(transport.ErrAddrRefused)
	r.warnRejectedAnnouncement(transport.ErrUnusableAnnouncement)
	if got := strings.Count(logs.String(), "ignoring a rejected announcement"); got != 2 {
		t.Fatalf("two reasons within the interval must log twice, logged %d times", got)
	}

	clock = clock.Add(rejectionLogInterval)
	r.warnRejectedAnnouncement(transport.ErrAddrRefused)
	if got := strings.Count(logs.String(), "ignoring a rejected announcement"); got != 3 {
		t.Fatalf("a reason must log again once the interval has elapsed, logged %d times", got)
	}
}

func TestPlaintextRemoteAddressesAreRefusedWithoutCredentials(t *testing.T) {
	r := NewResolver(nil, "")
	for addr, want := range map[string]bool{
		"127.0.0.1:4000":   true,
		"[::1]:4000":       true,
		"localhost:4000":   true,
		"10.0.0.5:4000":    false,
		"example.com:4000": false,
		"no-port-here":     false,
	} {
		if got := r.addrAllowed(addr); got != want {
			t.Errorf("addrAllowed(%q) without credentials = %v, want %v", addr, got, want)
		}
	}
}

func TestAnyRemoteAddressIsAdmittedOnceCredentialsAreSet(t *testing.T) {
	r := NewResolver(nil, "")
	r.SetCredentials(insecure.NewCredentials())
	if !r.addrAllowed("10.0.0.5:4000") {
		t.Fatal("mTLS credentials authenticate the peer, so a non-loopback address must be admitted")
	}
}

func TestRejectionsOfOneClassShareOneLogLine(t *testing.T) {
	var logs bytes.Buffer
	r := NewResolver(nil, "")
	r.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	clock := r.now()
	r.now = func() time.Time { return clock }

	r.warnRejectedAnnouncement(fmt.Errorf("first %w", transport.ErrAddrRefused))
	r.warnRejectedAnnouncement(fmt.Errorf("second %w", transport.ErrAddrRefused))
	if got := strings.Count(logs.String(), "ignoring a rejected announcement"); got != 1 {
		t.Fatalf("one class of rejection must log once within the interval, logged %d times", got)
	}
}
