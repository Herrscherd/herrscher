package discovery

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
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

type silentSource struct {
	name string
}

func (s silentSource) Name() string { return s.name }

func (s silentSource) Find(ctx context.Context) ([]Candidate, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDiscoverSortsTheSourcesAndDropsRepeatedCandidates(t *testing.T) {
	sshAlias := Candidate{Name: "build1", Address: "build1", Route: RouteSSH, SSHTarget: "build1"}
	tailnetPeer := Candidate{Name: "build1", Address: "build1.tailnet.example.ts.net", Route: RouteTailnet}
	sameTailnetPeer := Candidate{Name: "build1", Address: "BUILD1.tailnet.example.ts.net.", Route: RouteTailnet}
	report := Discover(context.Background(), []Source{
		fakeSource{name: "ssh-config", found: []Candidate{sshAlias, sshAlias}},
		fakeSource{name: "tailscale", found: []Candidate{tailnetPeer, sameTailnetPeer}},
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

func TestASourceThatNeverAnswersEndsAtTheDeadline(t *testing.T) {
	started := time.Now()
	report := Discover(context.Background(), []Source{silentSource{name: "stuck"}})
	if !slices.Equal(report.Unavailable, []string{"stuck"}) {
		t.Fatalf("unavailable %v, want [stuck]", report.Unavailable)
	}
	if elapsed := time.Since(started); elapsed > 10*SourceDeadline {
		t.Fatalf("discovery waited %v for a silent source", elapsed)
	}
}

func TestACancelledDiscoveryReturnsAtOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report := Discover(ctx, []Source{silentSource{name: "stuck"}, fakeSource{name: "ssh-config"}})
	if !slices.Equal(report.Unavailable, []string{"stuck"}) {
		t.Fatalf("unavailable %v, want [stuck]", report.Unavailable)
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

func TestPlainTextRemovesWhatATerminalWouldObey(t *testing.T) {
	hostile := "a\x1b]0;pwn\x07b\u009bc\u202ed\u2066e\tf\x7f"
	got := plainText(hostile)
	for _, forbidden := range []rune{'\x1b', '\x07', '\u009b', '\u202e', '\u2066', '\x7f', '\t'} {
		for _, char := range got {
			if char == forbidden {
				t.Fatalf("plainText kept %U in %q", forbidden, got)
			}
		}
	}
	if got != "a]0;pwnbcdef" {
		t.Fatalf("plainText(%q) = %q", hostile, got)
	}
}

func TestPlainTextRemovesEveryFormatAndSeparatorRune(t *testing.T) {
	for _, char := range []rune{
		'\u200b', '\u200e', '\u200f', '\u061c', '\u2028', '\u2029', '\ufeff', '\u00ad', '\u2060', '\U000e0001',
		'\x1b', '\x07', '\u0085', '\u202e', '\u2067',
	} {
		text := "a" + string(char) + "b"
		if got := plainText(text); got != "ab" {
			t.Errorf("%U: plainText(%q) = %q, want %q", char, text, got, "ab")
		}
	}
}

func TestPlainTextKeepsPrintableText(t *testing.T) {
	const text = "héllo wörld 日本-1.example"
	if got := plainText(text); got != text {
		t.Fatalf("plainText(%q) = %q", text, got)
	}
}

func TestAnOverflowingSourceKeepsItsCandidatesAndCountsTheDropped(t *testing.T) {
	peer := Candidate{Name: "build1", Address: "192.0.2.5", Route: RouteLAN}
	report := Discover(context.Background(), []Source{
		fakeSource{name: "dns-sd", found: []Candidate{peer}, err: OverflowError{Dropped: 44}},
	})
	if report.Dropped != 44 {
		t.Fatalf("dropped %d, want 44", report.Dropped)
	}
	if !slices.Equal(report.Candidates, []Candidate{peer}) {
		t.Fatalf("the kept candidates were lost: %+v", report.Candidates)
	}
	if len(report.Unavailable) != 0 {
		t.Fatalf("an overflow is not an unavailable source: %v", report.Unavailable)
	}
}
