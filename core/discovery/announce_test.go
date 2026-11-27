package discovery

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestAnAnnouncementPublishesOnlyPresenceAndFingerprint(t *testing.T) {
	a := Announcement{Fingerprint: mustFingerprint(t, fakeFingerprint), Port: 7000, Addresses: []net.IP{net.ParseIP("192.0.2.10")}}
	zone, err := a.zone()
	if err != nil {
		t.Fatal(err)
	}
	allowed := []string{"proto=1", "fp=" + fakeFingerprint}
	for _, question := range []string{ServiceType + ".local.", "herrscher-" + fakeFingerprint + "." + ServiceType + ".local."} {
		for _, record := range zone.Records(dns.Question{Name: question, Qtype: dns.TypeANY, Qclass: dns.ClassINET}) {
			txt, ok := record.(*dns.TXT)
			if !ok {
				continue
			}
			if !slices.Equal(txt.Txt, allowed) {
				t.Errorf("published TXT %q, want exactly %q", txt.Txt, allowed)
			}
		}
	}
}

func TestTheAnnouncementNeverNamesTheMachine(t *testing.T) {
	a := Announcement{Fingerprint: mustFingerprint(t, fakeFingerprint), Port: 7000, Addresses: []net.IP{net.ParseIP("192.0.2.10")}}
	zone, err := a.zone()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range zone.Records(dns.Question{Name: "herrscher-" + fakeFingerprint + ".local.", Qtype: dns.TypeANY, Qclass: dns.ClassINET}) {
		srv, ok := record.(*dns.SRV)
		if !ok {
			continue
		}
		if srv.Target != "herrscher-"+fakeFingerprint+".local." {
			t.Errorf("SRV target %q names something other than the fingerprint", srv.Target)
		}
	}
	for _, text := range a.TXT() {
		for _, forbidden := range []string{"user=", "session=", "version=", "host="} {
			if strings.HasPrefix(text, forbidden) {
				t.Errorf("forbidden TXT field %q published", text)
			}
		}
	}
}

func TestAdvertiseStopsAfterItsWindow(t *testing.T) {
	a := Announcement{Fingerprint: mustFingerprint(t, fakeFingerprint), Port: 7000}
	started := time.Now()
	err := Advertise(context.Background(), a, 30*time.Millisecond, func(ctx context.Context, _ Announcement) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the window to close", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the announcement outlived its window by %v", elapsed)
	}
}

func TestAdvertiseRefusesAWindowOutOfRange(t *testing.T) {
	a := Announcement{Fingerprint: mustFingerprint(t, fakeFingerprint), Port: 7000}
	serve := func(context.Context, Announcement) error {
		t.Fatal("an out of range window must not publish")
		return nil
	}
	for _, window := range []time.Duration{0, -time.Second, MaxAnnounceWindow + time.Second} {
		if err := Advertise(context.Background(), a, window, serve); !errors.Is(err, ErrWindowOutOfRange) {
			t.Errorf("window %v: got %v, want ErrWindowOutOfRange", window, err)
		}
	}
}

func TestAdvertiseRefusesAnAnnouncementWithoutAFingerprint(t *testing.T) {
	serve := func(context.Context, Announcement) error {
		t.Fatal("an announcement without a fingerprint must not publish")
		return nil
	}
	if err := Advertise(context.Background(), Announcement{Port: 7000}, time.Second, serve); !errors.Is(err, ErrFingerprintInvalid) {
		t.Fatalf("got %v, want ErrFingerprintInvalid", err)
	}
}

func mustFingerprint(t *testing.T, text string) Fingerprint {
	t.Helper()
	fingerprint, err := ParseFingerprint(text)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}
