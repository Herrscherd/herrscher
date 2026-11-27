package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/discovery"
)

func hostileNetworkNames() []string {
	return []string{"x`id`", "$(id)", "a;id", "a|id", "a&id", "a id", "a'b", "-oProxyCommand=id", "a\x1b]0;pwn\x07"}
}

func TestTheDiscoveryProposesAnAddCommandForEachCandidate(t *testing.T) {
	report := discovery.Report{Candidates: []discovery.Candidate{
		{Name: "build1", Address: "build1", Route: discovery.RouteSSH, SSHTarget: "build1"},
		{Name: "herrscher-0123456789abcdef", Address: "192.0.2.10", Route: discovery.RouteLAN, Fingerprint: mustParseFingerprint(t)},
	}}
	text := formatDiscovery(replyText(clitext.NewMessages(), i18n.English), report)

	for _, want := range []string{
		"nothing is written",
		"build1 at build1, route ssh, unverified, fingerprint unknown",
		"add it with: herrscher host add build1 --ssh build1 --workspace <path>",
		"herrscher-0123456789abcdef at 192.0.2.10, route lan, unverified, fingerprint 0123456789abcdef",
		"add it with: herrscher host add <name> --ssh <user>@192.0.2.10 --workspace <path>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
}

func TestAnEmptyDiscoveryReportsNothingFoundAndUnavailableSources(t *testing.T) {
	report := discovery.Report{Unavailable: []string{"dns-sd"}}
	text := formatDiscovery(replyText(clitext.NewMessages(), i18n.French), report)
	for _, want := range []string{"aucun hôte trouvé", "source dns-sd indisponible"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
}

func mustParseFingerprint(t *testing.T) discovery.Fingerprint {
	t.Helper()
	fingerprint, err := discovery.ParseFingerprint("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func TestTheAddCommandRefusesANetworkNameThatIsNotAPlainHostName(t *testing.T) {
	for _, hostile := range hostileNetworkNames() {
		sshAlias := discovery.Candidate{Name: hostile, Address: "build1", Route: discovery.RouteSSH, SSHTarget: hostile}
		if command, ok := addCommand(sshAlias); ok {
			t.Errorf("ssh alias %q proposed %q", hostile, command)
		}
		tailnetPeer := discovery.Candidate{Name: hostile, Address: "build1.tailnet.example.ts.net", Route: discovery.RouteTailnet}
		if command, ok := addCommand(tailnetPeer); ok {
			t.Errorf("tailnet name %q proposed %q", hostile, command)
		}
		lanPeer := discovery.Candidate{Name: hostile, Address: hostile, Route: discovery.RouteLAN}
		if command, ok := addCommand(lanPeer); ok {
			t.Errorf("lan address %q proposed %q", hostile, command)
		}
	}
}

func TestTheAddCommandKeepsPlainHostNames(t *testing.T) {
	candidate := discovery.Candidate{Name: "build-1.example_net", Address: "fd7a:115c::1", Route: discovery.RouteSSH, SSHTarget: "build-1.example_net"}
	command, ok := addCommand(candidate)
	if !ok || command != "herrscher host add build-1.example_net --ssh build-1.example_net --workspace <path>" {
		t.Fatalf("got %q, ok %v", command, ok)
	}
}

func TestAHostileSSHAliasNeverPutsAControlOnTheTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Host a\x1b]0;pwn\x07\n    HostName b\x1b[2J.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := discovery.Discover(context.Background(), []discovery.Source{discovery.SSHConfig{Path: path}})
	text := formatDiscovery(replyText(clitext.NewMessages(), i18n.English), report)
	if strings.ContainsAny(text, "\x1b\x07") {
		t.Fatalf("a control reached the terminal:\n%q", text)
	}
	if strings.Contains(text, "herrscher host add a") {
		t.Fatalf("a hostile alias was proposed as a command:\n%s", text)
	}
}
