package discovery

import (
	"net"
	"testing"

	"github.com/hashicorp/mdns"
)

const fakeFingerprint = "0123456789abcdef"

func TestAnAnnouncedDaemonBecomesALANCandidate(t *testing.T) {
	entry := &mdns.ServiceEntry{
		Name:       "herrscher-" + fakeFingerprint + "._herrscher._tcp.local.",
		Host:       "herrscher-" + fakeFingerprint + ".local.",
		AddrV4:     net.ParseIP("192.0.2.10"),
		Port:       7000,
		InfoFields: []string{"proto=1", "fp=" + fakeFingerprint},
	}
	candidate, ok := candidateFromEntry(entry)
	if !ok {
		t.Fatal("an announced daemon with an address must be a candidate")
	}
	if candidate.Name != "herrscher-"+fakeFingerprint {
		t.Errorf("name %q", candidate.Name)
	}
	if candidate.Address != "192.0.2.10" || candidate.Route != RouteLAN {
		t.Errorf("got %+v", candidate)
	}
	if candidate.Fingerprint.String() != fakeFingerprint {
		t.Errorf("fingerprint %q, want %q", candidate.Fingerprint, fakeFingerprint)
	}
}

func TestAnAnnouncementWithoutAFingerprintStillCountsAsAPresence(t *testing.T) {
	entry := &mdns.ServiceEntry{
		Name:       "herrscher-x._herrscher._tcp.local.",
		AddrV4:     net.ParseIP("192.0.2.11"),
		InfoFields: []string{"fp=not-hex"},
	}
	candidate, ok := candidateFromEntry(entry)
	if !ok {
		t.Fatal("a presence without a valid fingerprint is still a candidate")
	}
	if !candidate.Fingerprint.IsZero() {
		t.Errorf("an invalid fingerprint must be dropped, got %q", candidate.Fingerprint)
	}
}

func TestAnEntryWithoutAnyAddressIsDropped(t *testing.T) {
	if _, ok := candidateFromEntry(&mdns.ServiceEntry{Name: "herrscher-x._herrscher._tcp.local."}); ok {
		t.Fatal("an entry without any address cannot be proposed")
	}
}

func TestAnEntryWithOnlyAHostNameUsesIt(t *testing.T) {
	candidate, ok := candidateFromEntry(&mdns.ServiceEntry{
		Name: "herrscher-x._herrscher._tcp.local.",
		Host: "herrscher-x.local.",
	})
	if !ok || candidate.Address != "herrscher-x.local" {
		t.Fatalf("got %+v, ok %v", candidate, ok)
	}
}

func TestAnEntryFromTheNetworkCannotInjectControls(t *testing.T) {
	candidate, ok := candidateFromEntry(&mdns.ServiceEntry{
		Name:   "a\x1b]0;pwn\x07._herrscher._tcp.local.",
		Host:   "b\u202ec.local.",
		AddrV4: net.ParseIP("192.0.2.12"),
	})
	if !ok {
		t.Fatal("the entry has an address and must be a candidate")
	}
	if candidate.Name != "a]0;pwn" || candidate.Address != "192.0.2.12" {
		t.Fatalf("got name %q, address %q", candidate.Name, candidate.Address)
	}
}
