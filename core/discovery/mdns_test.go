package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

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

func TestTheLANSourceKeepsAtMostMaxLANCandidatesAndCountsTheRest(t *testing.T) {
	const extra = 10
	entries := make(chan *mdns.ServiceEntry, maxLANCandidates+extra)
	for index := range maxLANCandidates + extra {
		entries <- &mdns.ServiceEntry{
			Name:   fmt.Sprintf("herrscher-%d._herrscher._tcp.local.", index),
			AddrV4: net.IPv4(192, 0, 2, 1),
		}
	}
	close(entries)
	result := collectEntries(entries)
	if len(result.found) != maxLANCandidates {
		t.Fatalf("kept %d candidates, want %d", len(result.found), maxLANCandidates)
	}
	if result.dropped != extra {
		t.Fatalf("dropped %d, want %d", result.dropped, extra)
	}
}

func TestAnAlreadyCancelledBrowseReturnsAtOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err := MDNSSource{Window: time.Minute}.Find(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("a cancelled browse ran for %v", elapsed)
	}
}

func TestABrowseEndsAtTheDeadlineOfItsContextEvenWithALongerWindow(t *testing.T) {
	const deadline = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	started := time.Now()
	_, err := MDNSSource{Window: time.Minute}.Find(ctx)
	elapsed := time.Since(started)
	if err != nil {
		t.Skipf("multicast is not available here: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("the browse ran %v past a %v deadline", elapsed, deadline)
	}
}
