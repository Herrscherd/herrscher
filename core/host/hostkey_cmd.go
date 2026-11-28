package host

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	paramHostKey      = "host-key"
	paramAllowProject = "allow-project"
	paramAllowAll     = "allow-all-projects"
	paramMaxSessions  = "max-sessions"
	paramWhenFull     = "when-full"
)

func hostTrustVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("host", "trust").
		Help(clitext.CmdHelpHostTrust).
		ValueParam(paramName, clitext.CmdParamHostTrustName, false).
		ValueParam(paramHostKey, clitext.CmdParamHostTrustKey, false).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(trustHost(ctx, cliMessages, lang, store, input))
		})
}

func hostSetVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("host", "set").
		Help(clitext.CmdHelpHostSet).
		ValueParam(paramName, clitext.CmdParamHostSetName, false).
		ValueParam(paramAllowProject, clitext.CmdParamHostSetProjects, false).
		Param(paramAllowAll, clitext.CmdParamHostSetAllowAll, false).
		ValueParam(paramMaxSessions, clitext.CmdParamHostSetMax, false).
		ValueParam(paramWhenFull, clitext.CmdParamHostSetWhenFull, false).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(setHost(cliMessages, lang, store, input))
		})
}

func trustHost(ctx context.Context, cliMessages *clitext.Messages, lang languageSource, store *state.State, input contracts.Input) (string, error) {
	name := hostNameOf(input)
	host, ok := store.FindHost(name)
	if !ok {
		return "", unknownHost(store, name)
	}
	key, err := runner.ScanHostKey(ctx, host.SSH)
	if err != nil {
		return "", err
	}
	given := strings.TrimSpace(input.Get(paramHostKey))
	if given == "" {
		return presentedKey(cliMessages, lang(), name, key), nil
	}
	if given != key.Fingerprint {
		return "", errHostKeyMismatch
	}
	host.HostKey, host.KnownHosts, host.KeyChanged = key.Fingerprint, store.KnownHostsFile(name), ""
	if err := runner.PinHostKey(host.KnownHosts, key); err != nil {
		return "", err
	}
	if err := store.PutHost(host); err != nil {
		return "", err
	}
	return cliMessages.Text(lang(), clitext.HostKeyTrusted, clitext.Params{clitext.ParamHost: name, clitext.ParamFingerprint: key.Fingerprint})
}

func setHost(cliMessages *clitext.Messages, lang languageSource, store *state.State, input contracts.Input) (string, error) {
	name := hostNameOf(input)
	host, ok := store.FindHost(name)
	if !ok {
		return "", unknownHost(store, name)
	}
	if !settingsRequested(input) {
		return "", errSetNothing
	}
	if err := applySettings(&host, input); err != nil {
		return "", err
	}
	if err := store.PutHost(host); err != nil {
		return "", err
	}
	return cliMessages.Text(lang(), clitext.HostSetDone, clitext.Params{clitext.ParamHost: name})
}

func presentedKey(cliMessages *clitext.Messages, lang i18n.Language, name string, key runner.HostKey) string {
	text, err := cliMessages.Text(lang, clitext.HostKeyPresented, clitext.Params{clitext.ParamHost: name, clitext.ParamFingerprint: key.Fingerprint})
	if err != nil {
		return err.Error()
	}
	return text
}

func settingsRequested(input contracts.Input) bool {
	for _, name := range []string{paramAllowProject, paramMaxSessions, paramWhenFull} {
		if _, ok := input.Lookup(name); ok {
			return true
		}
	}
	return input.Bool(paramAllowAll)
}

func applySettings(host *state.Host, input contracts.Input) error {
	projects, hasProjects := input.Lookup(paramAllowProject)
	if hasProjects && input.Bool(paramAllowAll) {
		return errSetConflict
	}
	if hasProjects {
		host.Projects = splitProjects(projects)
	}
	if input.Bool(paramAllowAll) {
		host.Projects = nil
	}
	if raw, ok := input.Lookup(paramMaxSessions); ok {
		limit, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || limit < 0 {
			return errMaxSessionsInvalid
		}
		host.MaxSessions = limit
	}
	if raw, ok := input.Lookup(paramWhenFull); ok {
		policy, err := state.ParseFullPolicy(strings.TrimSpace(raw))
		if err != nil {
			return err
		}
		host.WhenFull = policy
	}
	return nil
}

func splitProjects(raw string) []string {
	var projects []string
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			projects = append(projects, name)
		}
	}
	return projects
}

func hostKeyStateKey(host state.Host) clitext.Key {
	switch {
	case host.KeyChanged != "":
		return clitext.HostKeyStateBlocked
	case host.HostKey == "":
		return clitext.HostKeyStateNotPinned
	default:
		return clitext.HostKeyStatePinned
	}
}

func hostKeyLine(cliMessages *clitext.Messages, lang i18n.Language, host state.Host, running int) string {
	keyState, err := cliMessages.Text(lang, hostKeyStateKey(host), nil)
	if err != nil {
		return err.Error()
	}
	sessions, err := sessionsText(cliMessages, lang, host, running)
	if err != nil {
		return err.Error()
	}
	load, err := loadText(cliMessages, lang, host)
	if err != nil {
		return err.Error()
	}
	text, err := cliMessages.Text(lang, clitext.HostListDetail, clitext.Params{clitext.ParamKey: keyState, clitext.ParamSessions: sessions, clitext.ParamLoad: load})
	if err != nil {
		return err.Error()
	}
	return text
}

func sessionsText(cliMessages *clitext.Messages, lang i18n.Language, host state.Host, running int) (string, error) {
	maxText := strconv.Itoa(host.MaxSessions)
	if host.MaxSessions == 0 {
		text, err := cliMessages.Text(lang, clitext.HostSessionsUnlimited, nil)
		if err != nil {
			return "", err
		}
		maxText = text
	}
	return cliMessages.Text(lang, clitext.HostSessionsLine, clitext.Params{clitext.ParamSessions: running, clitext.ParamMax: maxText})
}

func loadText(cliMessages *clitext.Messages, lang i18n.Language, host state.Host) (string, error) {
	if host.Load == nil {
		return cliMessages.Text(lang, clitext.HostLoadUnknown, nil)
	}
	return cliMessages.Text(lang, clitext.HostLoadLine, clitext.Params{clitext.ParamLoad: fmt.Sprintf("%.2f", *host.Load)})
}
