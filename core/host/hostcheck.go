package host

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func warnCatalog(out io.Writer, cliMessages *clitext.Messages, lang languageSource, key clitext.Key, params clitext.Params) {
	text, err := cliMessages.Text(lang(), key, params)
	if err != nil {
		text = err.Error()
	}
	logWriteFailure(fmt.Fprintln(warningWriter(out), text))
}

func inspectHost(ctx context.Context, messages *hostMessages, cliMessages *clitext.Messages, lang i18n.Language, host state.Host, run hostRunner, running int) (state.Host, []string) {
	var notes []string
	note := func(key clitext.Key, params clitext.Params) {
		text, err := cliMessages.Text(lang, key, params)
		if err != nil {
			text = err.Error()
		}
		notes = append(notes, text)
	}
	if host.SSH != "" {
		key, err := runner.ScanHostKey(ctx, host.SSH)
		switch {
		case err != nil:
			notes = append(notes, localizeHostError(messages, cliMessages, lang, err).Error())
		case host.HostKey == "":
			note(clitext.HostKeyUnpinned, clitext.Params{clitext.ParamHost: host.Name, clitext.ParamFingerprint: key.Fingerprint})
		case key.Fingerprint != host.HostKey:
			host.KeyChanged = key.Fingerprint
			note(clitext.HostKeyChangedNotice, clitext.Params{clitext.ParamHost: host.Name, clitext.ParamPinned: host.HostKey, clitext.ParamFingerprint: key.Fingerprint})
		default:
			host.KeyChanged = ""
		}
	}
	if host.Bin != "" {
		notes = append(notes, binaryNotes(ctx, cliMessages, lang, host, run)...)
	}
	if load, err := measureLoad(ctx, run); err == nil {
		now := time.Now()
		host.Load, host.LoadAt = &load, &now
	}
	if sessions, err := sessionsText(cliMessages, lang, host, running); err == nil {
		notes = append(notes, sessions)
	}
	if loadLine, err := loadText(cliMessages, lang, host); err == nil {
		notes = append(notes, loadLine)
	}
	return host, notes
}

func binaryNotes(ctx context.Context, cliMessages *clitext.Messages, lang i18n.Language, host state.Host, run hostRunner) []string {
	if host.BinSHA256 == "" {
		text, err := cliMessages.Text(lang, clitext.HostBinaryUnrecorded, clitext.Params{clitext.ParamHost: host.Name})
		return []string{textOrError(text, err)}
	}
	out, err := capture(ctx, run, runner.BinaryDigestArgv(host.Bin)...)
	if err != nil {
		return nil
	}
	got, err := runner.ParseDigest(out)
	if err != nil || got == host.BinSHA256 {
		return nil
	}
	text, textErr := cliMessages.Text(lang, clitext.HostFaultBinaryChanged, nil)
	return []string{textOrError(text, textErr)}
}

func measureLoad(ctx context.Context, run hostRunner) (float64, error) {
	out, err := capture(ctx, run, runner.LoadArgv()...)
	if err != nil {
		return 0, err
	}
	return runner.ParseLoad(out)
}

func textOrError(text string, err error) string {
	if err != nil {
		return err.Error()
	}
	return text
}
