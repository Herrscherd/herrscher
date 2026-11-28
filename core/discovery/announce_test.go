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
	a := Announcement{Fingerprint: mustFingerprint(t), Port: 7000, Addresses: []net.IP{net.ParseIP("192.0.2.10")}}
	zone, err := a.zone()
	if err != nil {
		t.Fatal(err)
	}
	allowed := []string{"proto=1", "fp=" + fakeFingerprint}
	seenTXT := 0
	seenSRV := 0
	for _, question := range []string{ServiceType + ".local.", "herrscher-" + fakeFingerprint + "." + ServiceType + ".local."} {
		for _, record := range zone.Records(dns.Question{Name: question, Qtype: dns.TypeANY, Qclass: dns.ClassINET}) {
			switch typed := record.(type) {
			case *dns.TXT:
				seenTXT++
				if !slices.Equal(typed.Txt, allowed) {
					t.Errorf("published TXT %q, want exactly %q", typed.Txt, allowed)
				}
			case *dns.SRV:
				seenSRV++
			}
		}
	}
	if seenTXT == 0 || seenSRV == 0 {
		t.Fatalf("the zone published %d TXT and %d SRV records, want at least one of each", seenTXT, seenSRV)
	}
}

func TestTheAnnouncementNeverNamesTheMachine(t *testing.T) {
	a := Announcement{Fingerprint: mustFingerprint(t), Port: 7000, Addresses: []net.IP{net.ParseIP("192.0.2.10")}}
	zone, err := a.zone()
	if err != nil {
		t.Fatal(err)
	}
	seenSRV := 0
	for _, record := range zone.Records(dns.Question{Name: "herrscher-" + fakeFingerprint + "." + ServiceType + ".local.", Qtype: dns.TypeANY, Qclass: dns.ClassINET}) {
		srv, ok := record.(*dns.SRV)
		if !ok {
			continue
		}
		seenSRV++
		if srv.Target != "herrscher-"+fakeFingerprint+".local." {
			t.Errorf("SRV target %q names something other than the fingerprint", srv.Target)
		}
	}
	if seenSRV == 0 {
		t.Fatal("the zone published no SRV record, the target rule was not checked")
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
	a := Announcement{Fingerprint: mustFingerprint(t), Port: 7000, Addresses: []net.IP{net.ParseIP("192.0.2.10")}}
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
	a := Announcement{Fingerprint: mustFingerprint(t), Port: 7000, Addresses: []net.IP{net.ParseIP("192.0.2.10")}}
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
	if err := Advertise(context.Background(), Announcement{Port: 7000, Addresses: []net.IP{net.ParseIP("192.0.2.10")}}, time.Second, serve); !errors.Is(err, ErrFingerprintInvalid) {
		t.Fatalf("got %v, want ErrFingerprintInvalid", err)
	}
}

func TestAdvertiseRefusesAnAnnouncementWithoutAnAddress(t *testing.T) {
	serve := func(context.Context, Announcement) error {
		t.Fatal("an announcement without an address must not publish")
		return nil
	}
	if err := Advertise(context.Background(), Announcement{Fingerprint: mustFingerprint(t), Port: 7000}, time.Second, serve); !errors.Is(err, ErrNoAddress) {
		t.Fatalf("got %v, want ErrNoAddress", err)
	}
}

func mustFingerprint(t *testing.T) Fingerprint {
	t.Helper()
	fingerprint, err := ParseFingerprint(fakeFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}
