package host

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/discovery"
)

func TestTheDiscoveryProposesAnAddCommandForEachCandidate(t *testing.T) {
	report := discovery.Report{Candidates: []discovery.Candidate{
		{Name: "build1", Address: "build1", Route: discovery.RouteSSH, SSHTarget: "build1"},
		{Name: "herrscher-0123456789abcdef", Address: "192.0.2.10", Route: discovery.RouteLAN, Fingerprint: mustParseFingerprint(t, "0123456789abcdef")},
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

func mustParseFingerprint(t *testing.T, text string) discovery.Fingerprint {
	t.Helper()
	fingerprint, err := discovery.ParseFingerprint(text)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}
