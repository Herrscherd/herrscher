package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/termpaint"
	usagecore "github.com/Herrscherd/herrscher/core/usage"
)

const (
	usageVerb         = "usage"
	usageViewLimits   = "limits"
	usageViewCost     = "cost"
	usageViewTokens   = "tokens"
	usageViewCompact  = "compaction"
	usageParamRange   = "--range"
	usageParamAccount = "--account"
	usageParamVendor  = "--vendor"
	usageExitDown     = 1
)

type usageOptions struct {
	view    string
	rng     string
	account string
	vendor  string
	asJSON  bool
}

func runUsage(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, args []string) error {
	lang := clitext.ClientLanguage()
	opts, err := parseUsageArgs(messages, lang, args)
	if err != nil {
		return err
	}
	instance := host.ServedInstanceID(host.DefaultStatePath(), "")
	snapshot, err := fetchUsage(ctx, layout, instance, opts)
	if host.IsDaemonNotRunning(err) {
		return usageDaemonDown(layout, messages, lang, instance)
	}
	if err != nil {
		return err
	}
	return writeUsage(os.Stdout, snapshot, opts, lang, outputPalette(os.Stdout))
}

func outputPalette(out *os.File) termpaint.Palette {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || !term.IsTerminal(int(out.Fd())) {
		return termpaint.Palette{}
	}
	return termpaint.ANSIPalette()
}

func usageDaemonDownText(messages *clitext.Messages, lang i18n.Language, socket string) (string, error) {
	return messages.Text(lang, clitext.SpendDaemonDown, clitext.Params{clitext.ParamSocket: termpaint.Escape(socket).String()})
}

func usageDaemonDown(layout host.RuntimeLayout, messages *clitext.Messages, lang i18n.Language, instance string) error {
	text, err := usageDaemonDownText(messages, lang, host.CommandSocketPathIn(layout, instance))
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(os.Stderr, text); err != nil {
		return err
	}
	return exitStatusError{code: usageExitDown}
}

func parseUsageArgs(messages *clitext.Messages, lang i18n.Language, args []string) (usageOptions, error) {
	opts := usageOptions{view: usageViewLimits}
	for index := 0; index < len(args); index++ {
		name, value, inline := strings.Cut(args[index], "=")
		switch name {
		case argJSON:
			opts.asJSON = true
		case usageParamRange, usageParamAccount, usageParamVendor:
			var err error
			value, index, err = usageFlagValue(messages, lang, args, index, name, value, inline)
			if err != nil {
				return opts, err
			}
			if value == "" {
				return opts, renderError(messages, lang, usagecore.ErrEmptyValue, clitext.FlagNeedsValue, clitext.Params{clitext.ParamCommand: usageVerb, clitext.ParamName: strings.TrimPrefix(name, "--")})
			}
			setUsageParam(&opts, name, value)
		default:
			if err := applyUsageArg(messages, lang, &opts, name); err != nil {
				return opts, err
			}
		}
	}
	return opts, validateUsageOptions(messages, lang, opts)
}

func usageFlagValue(messages *clitext.Messages, lang i18n.Language, args []string, index int, name, value string, inline bool) (string, int, error) {
	if inline {
		return value, index, nil
	}
	if index+1 >= len(args) {
		return "", index, renderError(messages, lang, errUnknownArgument, clitext.FlagNeedsValue, clitext.Params{clitext.ParamCommand: usageVerb, clitext.ParamName: strings.TrimPrefix(name, "--")})
	}
	return args[index+1], index + 1, nil
}

func setUsageParam(opts *usageOptions, name, value string) {
	switch name {
	case usageParamRange:
		opts.rng = value
	case usageParamVendor:
		opts.vendor = value
	default:
		opts.account = value
	}
}

func applyUsageArg(messages *clitext.Messages, lang i18n.Language, opts *usageOptions, name string) error {
	if strings.HasPrefix(name, "--") {
		return renderError(messages, lang, errUnknownArgument, clitext.FlagUnknown, clitext.Params{clitext.ParamCommand: usageVerb, clitext.ParamName: strconv.Quote(name)})
	}
	if !isUsageView(name) {
		return renderError(messages, lang, errUnknownArgument, clitext.SpendModeUnknown, clitext.Params{clitext.ParamName: strconv.Quote(name)})
	}
	opts.view = name
	return nil
}

func validateUsageOptions(messages *clitext.Messages, lang i18n.Language, opts usageOptions) error {
	if opts.rng != "" {
		if _, err := usagecore.ParseRange(opts.rng); err != nil {
			return renderError(messages, lang, err, clitext.SpendRangeUnknown, clitext.Params{clitext.ParamName: strconv.Quote(opts.rng)})
		}
	}
	if _, err := usagecore.ParseAccountKey(opts.account); err != nil {
		return renderError(messages, lang, err, clitext.SpendAccountInvalid, clitext.Params{clitext.ParamName: strconv.Quote(opts.account)})
	}
	return nil
}

func isUsageView(name string) bool {
	switch name {
	case usageViewLimits, usageViewCost, usageViewTokens, usageViewCompact:
		return true
	}
	return false
}

func fetchUsage(ctx context.Context, layout host.RuntimeLayout, instance string, opts usageOptions) (usagecore.Snapshot, error) {
	argv := []string{usageVerb}
	if opts.rng != "" {
		argv = append(argv, usageParamRange, opts.rng)
	}
	if opts.account != "" {
		argv = append(argv, usageParamAccount, opts.account)
	}
	if opts.vendor != "" {
		argv = append(argv, usageParamVendor, opts.vendor)
	}
	out, err := host.DaemonDispatch(ctx, layout, instance, argv)
	if err != nil {
		return usagecore.Snapshot{}, err
	}
	return usagecore.Decode([]byte(out))
}

func writeUsage(writer io.Writer, snapshot usagecore.Snapshot, opts usageOptions, lang i18n.Language, pal termpaint.Palette) error {
	if opts.asJSON {
		raw, err := usagecore.Encode(snapshot)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(writer, string(raw))
		return err
	}
	lines, err := usageLines(snapshot, opts.view, lang, pal)
	if err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}
	return nil
}

func usageLines(snapshot usagecore.Snapshot, view string, lang i18n.Language, pal termpaint.Palette) ([]string, error) {
	switch view {
	case usageViewCost:
		return usagecore.CostLines(snapshot, lang, time.Local, pal)
	case usageViewTokens:
		return usagecore.TokenLines(snapshot, lang, pal)
	case usageViewCompact:
		return usagecore.CompactionLines(snapshot, lang, pal)
	default:
		return usagecore.LimitLines(snapshot, lang, pal)
	}
}
