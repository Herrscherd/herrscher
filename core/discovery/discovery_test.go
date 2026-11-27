package discovery

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeSource struct {
	name  string
	found []Candidate
	err   error
}

func (s fakeSource) Name() string { return s.name }

func (s fakeSource) Find(context.Context) ([]Candidate, error) {
	return s.found, s.err
}

func TestDiscoverMergesSortsAndDeduplicatesTheSources(t *testing.T) {
	sshAlias := Candidate{Name: "build1", Address: "build1", Route: RouteSSH, SSHTarget: "build1"}
	tailnetPeer := Candidate{Name: "build1", Address: "build1.tailnet.example.ts.net", Route: RouteTailnet}
	report := Discover(context.Background(), []Source{
		fakeSource{name: "ssh-config", found: []Candidate{sshAlias, sshAlias}},
		fakeSource{name: "tailscale", found: []Candidate{tailnetPeer}},
	})
	want := []Candidate{tailnetPeer, sshAlias}
	if !slices.Equal(report.Candidates, want) {
		t.Fatalf("got %+v, want %+v", report.Candidates, want)
	}
}

func TestAFailingSourceIsReportedAndDoesNotStopTheOthers(t *testing.T) {
	peer := Candidate{Name: "build1", Address: "192.0.2.5", Route: RouteLAN}
	report := Discover(context.Background(), []Source{
		fakeSource{name: "dns-sd", err: errors.New("no multicast interface")},
		fakeSource{name: "ssh-config", found: []Candidate{peer}},
	})
	if !slices.Equal(report.Unavailable, []string{"dns-sd"}) {
		t.Fatalf("unavailable %v, want [dns-sd]", report.Unavailable)
	}
	if len(report.Candidates) != 1 || report.Candidates[0] != peer {
		t.Fatalf("the healthy source was lost: %+v", report.Candidates)
	}
}

func TestParseFingerprintAcceptsOnlySixteenLowercaseHexDigits(t *testing.T) {
	for _, text := range []string{"", "0123", "0123456789ABCDEF", "0123456789abcdeg", "0123456789abcdef0"} {
		if _, err := ParseFingerprint(text); !errors.Is(err, ErrFingerprintInvalid) {
			t.Errorf("%q: got %v, want ErrFingerprintInvalid", text, err)
		}
	}
	if _, err := ParseFingerprint("0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
}

func TestRouteNamesAreTheOperatorWords(t *testing.T) {
	for route, want := range map[Route]string{RouteLAN: "lan", RouteTailnet: "tailnet", RouteSSH: "ssh"} {
		if route.String() != want {
			t.Errorf("route %d: got %q, want %q", route, route.String(), want)
		}
	}
}

func TestDefaultSourcesCoverTheThreeRoutes(t *testing.T) {
	names := make([]string, 0, 3)
	for _, source := range DefaultSources("/nonexistent/config") {
		names = append(names, source.Name())
	}
	if !slices.Equal(names, []string{"dns-sd", "tailscale", "ssh-config"}) {
		t.Fatalf("got sources %v", names)
	}
}
