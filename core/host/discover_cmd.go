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

const (
	addHintPlaceholder = "<name>"
	hostTokenSymbols   = ".-_@:[]"
)

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

func formatDiscovery(translator *clitext.Translator, report discovery.Report) string {
	var body strings.Builder
	if len(report.Candidates) == 0 {
		body.WriteString(translator.Text(clitext.DiscoverNone, nil))
	} else {
		body.WriteString(translator.Text(clitext.DiscoverIntro, nil))
	}
	for _, candidate := range report.Candidates {
		fmt.Fprintf(&body, "\n%s", translator.Text(clitext.DiscoverCandidate, clitext.Params{
			clitext.ParamName:        candidate.Name,
			clitext.ParamAddress:     candidate.Address,
			clitext.ParamRoute:       candidate.Route.String(),
			clitext.ParamTrust:       translator.Text(clitext.DiscoverUnverified, nil),
			clitext.ParamFingerprint: fingerprintText(translator, candidate.Fingerprint),
		}))
		fmt.Fprintf(&body, "\n  %s", addHint(translator, candidate))
	}
	for _, source := range report.Unavailable {
		fmt.Fprintf(&body, "\n%s", translator.Text(clitext.DiscoverSourceUnavailable, clitext.Params{clitext.ParamSource: source}))
	}
	return body.String()
}

func addHint(translator *clitext.Translator, candidate discovery.Candidate) string {
	command, ok := addCommand(candidate)
	if !ok {
		return translator.Text(clitext.DiscoverAddUnsafe, nil)
	}
	return translator.Text(clitext.DiscoverAddHint, clitext.Params{clitext.ParamCommand: command})
}

func fingerprintText(translator *clitext.Translator, fingerprint discovery.Fingerprint) string {
	if fingerprint.IsZero() {
		return translator.Text(clitext.DiscoverFingerprintUnknown, nil)
	}
	return translator.Text(clitext.DiscoverFingerprint, clitext.Params{clitext.ParamFingerprint: fingerprint.String()})
}

func addCommand(candidate discovery.Candidate) (string, bool) {
	name := addHintPlaceholder
	if candidate.Route != discovery.RouteLAN {
		if !isHostToken(candidate.Name) {
			return "", false
		}
		name = candidate.Name
	}
	target := candidate.SSHTarget
	if target == "" {
		if !isHostToken(candidate.Address) {
			return "", false
		}
		target = "<user>@" + candidate.Address
	} else if !isHostToken(target) {
		return "", false
	}
	return fmt.Sprintf("herrscher host add %s --ssh %s --workspace <path>", name, target), true
}

func isHostToken(text string) bool {
	if text == "" {
		return false
	}
	for index, char := range text {
		if index == 0 && !isASCIIAlphaNumeric(char) && char != '[' {
			return false
		}
		if !isASCIIAlphaNumeric(char) && !strings.ContainsRune(hostTokenSymbols, char) {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
}
