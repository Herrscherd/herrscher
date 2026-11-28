package runner

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

const edKeyLine = "build1 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIETJO4ixlZaB5DAsiVA8/GpxeR4uWaMomqe3J4Dor91L"

func TestParseHostKeyFingerprintMatchesSshKeygen(t *testing.T) {
	key, err := ParseHostKey(edKeyLine)
	if err != nil {
		t.Fatal(err)
	}
	if key.Fingerprint != "SHA256:dg7SaFkFiaJ+ASySMVsadsrJT5niXhy+dK2Ly2dF/To" {
		t.Fatalf("the fingerprint must match ssh-keygen -lf, got %q", key.Fingerprint)
	}
	if key.Line != "build1 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIETJO4ixlZaB5DAsiVA8/GpxeR4uWaMomqe3J4Dor91L" {
		t.Fatalf("the pinned line must keep host, type and key, got %q", key.Line)
	}
}

func TestParseHostKeyRefusesAnUnreadableLine(t *testing.T) {
	for _, line := range []string{"", "build1 ssh-ed25519", "build1 ssh-ed25519 %%%"} {
		if _, err := ParseHostKey(line); err == nil {
			t.Fatalf("%q must not parse as a host key", line)
		}
	}
}

func TestHostKeyPolicyPinsTheConnection(t *testing.T) {
	pinned := SSH{Target: "me@build1", KnownHosts: "/state/known_hosts/abc.known_hosts"}.Command(context.Background(), "", "true")
	for _, want := range []string{"StrictHostKeyChecking=yes", `UserKnownHostsFile="/state/known_hosts/abc.known_hosts"`, "GlobalKnownHostsFile=" + os.DevNull} {
		if !slices.Contains(pinned.Args, want) {
			t.Fatalf("a pinned connection must carry %q, args %q", want, pinned.Args)
		}
	}
	legacy := SSH{Target: "me@build1"}.Command(context.Background(), "", "true")
	if slices.ContainsFunc(legacy.Args, func(arg string) bool { return strings.Contains(arg, "StrictHostKeyChecking") }) {
		t.Fatalf("a host without a pinned key keeps the ssh defaults, args %q", legacy.Args)
	}
}

func TestPinnedCopyUsesTheSameKnownHosts(t *testing.T) {
	copied := SSH{Target: "me@build1", KnownHosts: "/state/k"}.Copy(context.Background(), "/tmp/a", "/home/me/bin/herrscher")
	if !slices.Contains(copied.Args, "StrictHostKeyChecking=yes") {
		t.Fatalf("scp must refuse an unpinned host too, args %q", copied.Args)
	}
}

func TestCheckKnownHostsPathRefusesQuotes(t *testing.T) {
	for _, path := range []string{`/x"y`, "/x\ny"} {
		if err := CheckKnownHostsPath(path); !errors.Is(err, ErrKnownHostsPath) {
			t.Fatalf("%q cannot be quoted for ssh and must be refused, got %v", path, err)
		}
	}
	if err := CheckKnownHostsPath("/home/me/.config/herrscher/known_hosts/abc.known_hosts"); err != nil {
		t.Fatalf("a plain path must pass, got %v", err)
	}
}

func TestParseDigestReadsSha256sumAndShasumOutput(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	for _, out := range []string{sum + "  /home/me/.herrscher/bin/herrscher\n", strings.ToUpper(sum) + " *herrscher\n"} {
		got, err := ParseDigest(out)
		if err != nil {
			t.Fatalf("%q must parse, got %v", out, err)
		}
		if got != sum {
			t.Fatalf("the digest must be lowercase hex, got %q", got)
		}
	}
}

func TestParseDigestRefusesWhatIsNotASha256(t *testing.T) {
	for _, out := range []string{"", "sha256sum: not found", strings.Repeat("g", 64) + " x", strings.Repeat("a", 40) + " x"} {
		if _, err := ParseDigest(out); !errors.Is(err, ErrDigestUnreadable) {
			t.Fatalf("%q is not a digest and must be refused, got %v", out, err)
		}
	}
}

func TestParseLoadReadsLinuxAndMacOSOutput(t *testing.T) {
	cases := map[string]float64{
		"0.52 0.58 0.59 1/1234 5678\n": 0.52,
		"{ 1.20 1.10 0.99 }\n":         1.20,
	}
	for out, want := range cases {
		got, err := ParseLoad(out)
		if err != nil || got != want {
			t.Fatalf("%q: want %v, got %v (%v)", out, want, got, err)
		}
	}
	if _, err := ParseLoad("no load here"); !errors.Is(err, ErrLoadUnreadable) {
		t.Fatalf("text without a load must be refused, got %v", err)
	}
}
