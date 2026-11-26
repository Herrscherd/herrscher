package manage

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/internal/errwriter"
)

const setupCheckVerb = "check"

type checkTone string

const (
	checkOK      checkTone = "ok"
	checkWarning checkTone = "warning"
	checkBad     checkTone = "bad"
	checkOff     checkTone = "off"
)

// SectionCheck is the state of one section of the setup, as the hub screen shows it.
type SectionCheck struct {
	Section string    `json:"section"`
	Tone    checkTone `json:"tone"`
	State   string    `json:"state"`
}

// SetupReport is every section of the setup and whether one of them needs fixing.
type SetupReport struct {
	Sections []SectionCheck `json:"sections"`
}

// Broken reports whether a section is red, the state that stops the daemon from serving it.
func (r SetupReport) Broken() bool {
	for _, section := range r.Sections {
		if section.Tone == checkBad {
			return true
		}
	}
	return false
}

// Text renders the report one section per line.
func (r SetupReport) Text() string {
	var out strings.Builder
	for _, section := range r.Sections {
		fmt.Fprintf(&out, "%s\t%s\t%s\n", section.Tone, section.Section, section.State)
	}
	return strings.TrimRight(out.String(), "\n")
}

// JSON renders the report as JSON.
func (r SetupReport) JSON() (string, error) {
	raw, err := json.Marshal(r)
	return string(raw), err
}

// CheckSetup reads the settings the way `herrscher setup` does and reports each section,
// running the account checks too when probe is set. It writes nothing and opens no sign-in.
func CheckSetup(ctx context.Context, messages *clitext.Messages, hostMessages *host.Messages, probe bool) SetupReport {
	deps := liveDeps(messages, hostMessages, "")
	_, statErr := os.Stat(deps.envPath)
	model := newSetupModel(messages, deps, setupStart{ctx: ctx, getenv: envx.Getenv, screen: screenHub, envExists: statErr == nil})
	model.readGitHubStatus(ctx)
	if probe {
		model.probeQuietly(ctx)
	}
	var report SetupReport
	for _, section := range model.sections() {
		report.Sections = append(report.Sections, SectionCheck{Section: section.name, Tone: toneWord(section.tone), State: section.state})
	}
	return report
}

func toneWord(tone lineTone) checkTone {
	switch tone {
	case toneOK:
		return checkOK
	case toneWarn:
		return checkWarning
	case toneBad:
		return checkBad
	case toneText, toneDim, toneBold, toneGroup, toneInfo:
	}
	return checkOff
}

func (model *setupModel) readGitHubStatus(ctx context.Context) {
	link := model.deps.github
	if link == nil {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	status, err := link.status(bounded)
	model.gitHubStatusDone(gitHubStatusMsg{status: status, err: err})
}

func (model *setupModel) probeQuietly(ctx context.Context) {
	for _, kind := range []probeKind{probeDiscord, probeTelegram, probeClaude, probeCodex, probeCursor, probeVault} {
		check, wanted := model.quietProbe(kind)
		if !wanted {
			continue
		}
		bounded, cancel := context.WithTimeout(ctx, probeTimeout)
		model.probes[kind] = check(bounded)
		cancel()
	}
}

func (model *setupModel) quietProbe(kind probeKind) (func(context.Context) probeResult, bool) {
	prober := model.deps.prober
	switch kind {
	case probeDiscord:
		token := model.draft.value(envDiscordToken)
		return func(bounded context.Context) probeResult { return prober.discord(bounded, token) }, model.modules[moduleDiscord] && token != ""
	case probeTelegram:
		token := model.draft.value(envTelegramBot)
		return func(bounded context.Context) probeResult { return prober.telegram(bounded, token) }, model.modules[moduleTelegram] && token != ""
	case probeClaude:
		if model.claudeKey {
			key := model.draft.value(envAnthropicKey)
			return func(bounded context.Context) probeResult { return prober.anthropicKey(bounded, key) }, model.modules[moduleClaude]
		}
		return func(bounded context.Context) probeResult { return prober.claude(bounded, false) }, model.modules[moduleClaude]
	case probeCodex:
		return func(bounded context.Context) probeResult { return prober.codex(bounded, false) }, model.modules[moduleCodex] && !model.codexKey
	case probeCursor, probeVault, probeGrok:
	}
	return nil, false
}

func setupCheckCmd(ctx context.Context, messages *clitext.Messages, hostMessages *host.Messages, args []string) int {
	out, errOut := errwriter.New(os.Stdout), errwriter.New(os.Stderr)
	flags := flag.NewFlagSet("setup check", flag.ContinueOnError)
	probe := flags.Bool("probe", false, msg(messages, clitext.ManageFlagSetupProbe, nil))
	asJSON := flags.Bool("json", false, msg(messages, clitext.ManageFlagJSON, nil))
	if err := flags.Parse(args); err != nil {
		return parseExit(err)
	}
	report := CheckSetup(ctx, messages, hostMessages, *probe)
	text := report.Text()
	if *asJSON {
		encoded, err := report.JSON()
		if err != nil {
			errOut.Println(err)
			return 1
		}
		text = encoded
	}
	out.Println(text)
	code := 0
	if report.Broken() {
		code = 1
	}
	return errwriter.Finish(code, out, errOut)
}
