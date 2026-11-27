package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/discovery"
)

const addHintPlaceholder = "<name>"

func hostDiscoverVerb(cliMessages *clitext.Messages, lang languageSource) contracts.Cmd {
	return contracts.New("host", "discover").Reads().
		Help(clitext.CmdHelpHostDiscover).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(discoverHosts(ctx, cliMessages, lang, in.JSON))
		})
}

func discoverHosts(ctx context.Context, cliMessages *clitext.Messages, lang languageSource, asJSON bool) (string, error) {
	report := discovery.Discover(ctx, discovery.DefaultSources(sshConfigPath()))
	if asJSON {
		body, err := json.Marshal(report)
		return string(body), err
	}
	return formatDiscovery(replyText(cliMessages, lang()), report), nil
}

func sshConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

func formatDiscovery(t *clitext.Translator, report discovery.Report) string {
	var body strings.Builder
	if len(report.Candidates) == 0 {
		body.WriteString(t.Text(clitext.DiscoverNone, nil))
	} else {
		body.WriteString(t.Text(clitext.DiscoverIntro, nil))
	}
	for _, candidate := range report.Candidates {
		fmt.Fprintf(&body, "\n%s", t.Text(clitext.DiscoverCandidate, clitext.Params{
			clitext.ParamName:        candidate.Name,
			clitext.ParamAddress:     candidate.Address,
			clitext.ParamRoute:       candidate.Route.String(),
			clitext.ParamTrust:       t.Text(clitext.DiscoverUnverified, nil),
			clitext.ParamFingerprint: fingerprintText(t, candidate.Fingerprint),
		}))
		fmt.Fprintf(&body, "\n  %s", t.Text(clitext.DiscoverAddHint, clitext.Params{
			clitext.ParamCommand: addCommand(candidate),
		}))
	}
	for _, source := range report.Unavailable {
		fmt.Fprintf(&body, "\n%s", t.Text(clitext.DiscoverSourceUnavailable, clitext.Params{clitext.ParamSource: source}))
	}
	return body.String()
}

func fingerprintText(t *clitext.Translator, fingerprint discovery.Fingerprint) string {
	if fingerprint.IsZero() {
		return t.Text(clitext.DiscoverFingerprintUnknown, nil)
	}
	return t.Text(clitext.DiscoverFingerprint, clitext.Params{clitext.ParamFingerprint: fingerprint.String()})
}

func addCommand(candidate discovery.Candidate) string {
	name := addHintPlaceholder
	target := candidate.SSHTarget
	if candidate.Route != discovery.RouteLAN {
		name = candidate.Name
	}
	if target == "" {
		target = "<user>@" + candidate.Address
	}
	return fmt.Sprintf("herrscher host add %s --ssh %s --workspace <path>", name, target)
}
