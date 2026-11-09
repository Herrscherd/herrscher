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
	usagecore "github.com/Herrscherd/herrscher/core/usage"
)

const (
	usageVerb         = "usage"
	usageViewLimits   = "limits"
	usageViewCost     = "cost"
	usageViewTokens   = "tokens"
	usageParamRange   = "--range"
	usageParamAccount = "--account"
	usageExitDown     = 1
)

type usageOptions struct {
	view    string
	rng     string
	account string
	asJSON  bool
}

func runUsage(ctx context.Context, args []string) error {
	lang := clitext.ClientLanguage()
	opts, err := parseUsageArgs(lang, args)
	if err != nil {
		return err
	}
	instance := host.ServedInstanceID(host.DefaultStatePath(), "")
	snapshot, err := fetchUsage(ctx, instance, opts)
	if host.IsDaemonNotRunning(err) {
		return usageDaemonDown(lang, instance)
	}
	if err != nil {
		return err
	}
	return writeUsage(os.Stdout, snapshot, opts, lang, usagePalette(os.Stdout))
}

func usagePalette(out *os.File) usagecore.Palette {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || !term.IsTerminal(int(out.Fd())) {
		return usagecore.Palette{}
	}
	return usagecore.ANSIPalette()
}

func usageDaemonDown(lang i18n.Language, instance string) error {
	text, err := clitext.Text(lang, clitext.SpendDaemonDown, clitext.Params{clitext.ParamSocket: host.CommandSocketPath(instance)})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(os.Stderr, text); err != nil {
		return err
	}
	return exitStatusError{code: usageExitDown}
}

func parseUsageArgs(lang i18n.Language, args []string) (usageOptions, error) {
	opts := usageOptions{view: usageViewLimits}
	for index := 0; index < len(args); index++ {
		name, value, inline := strings.Cut(args[index], "=")
		switch name {
		case argJSON:
			opts.asJSON = true
		case usageParamRange, usageParamAccount:
			var err error
			value, index, err = usageFlagValue(lang, args, index, name, value, inline)
			if err != nil {
				return opts, err
			}
			if value == "" {
				return opts, renderError(lang, usagecore.ErrEmptyValue, clitext.FlagNeedsValue, clitext.Params{clitext.ParamCommand: usageVerb, clitext.ParamName: strings.TrimPrefix(name, "--")})
			}
			setUsageParam(&opts, name, value)
		default:
			if err := applyUsageArg(lang, &opts, name); err != nil {
				return opts, err
			}
		}
	}
	return opts, validateUsageOptions(lang, opts)
}

func usageFlagValue(lang i18n.Language, args []string, index int, name, value string, inline bool) (string, int, error) {
	if inline {
		return value, index, nil
	}
	if index+1 >= len(args) {
		return "", index, renderError(lang, errUnknownArgument, clitext.FlagNeedsValue, clitext.Params{clitext.ParamCommand: usageVerb, clitext.ParamName: strings.TrimPrefix(name, "--")})
	}
	return args[index+1], index + 1, nil
}

func setUsageParam(opts *usageOptions, name, value string) {
	if name == usageParamRange {
		opts.rng = value
		return
	}
	opts.account = value
}

func applyUsageArg(lang i18n.Language, opts *usageOptions, name string) error {
	if strings.HasPrefix(name, "--") {
		return renderError(lang, errUnknownArgument, clitext.FlagUnknown, clitext.Params{clitext.ParamCommand: usageVerb, clitext.ParamName: strconv.Quote(name)})
	}
	if !isUsageView(name) {
		return renderError(lang, errUnknownArgument, clitext.SpendModeUnknown, clitext.Params{clitext.ParamName: strconv.Quote(name)})
	}
	opts.view = name
	return nil
}

func validateUsageOptions(lang i18n.Language, opts usageOptions) error {
	if opts.rng != "" {
		if _, err := usagecore.ParseRange(opts.rng); err != nil {
			return renderError(lang, err, clitext.SpendRangeUnknown, clitext.Params{clitext.ParamName: strconv.Quote(opts.rng)})
		}
	}
	if _, err := usagecore.ParseAccountKey(opts.account); err != nil {
		return renderError(lang, err, clitext.SpendAccountInvalid, clitext.Params{clitext.ParamName: strconv.Quote(opts.account)})
	}
	return nil
}

func isUsageView(name string) bool {
	switch name {
	case usageViewLimits, usageViewCost, usageViewTokens:
		return true
	}
	return false
}

func fetchUsage(ctx context.Context, instance string, opts usageOptions) (usagecore.Snapshot, error) {
	argv := []string{usageVerb}
	if opts.rng != "" {
		argv = append(argv, usageParamRange, opts.rng)
	}
	if opts.account != "" {
		argv = append(argv, usageParamAccount, opts.account)
	}
	out, err := host.DaemonDispatch(ctx, instance, argv)
	if err != nil {
		return usagecore.Snapshot{}, err
	}
	return usagecore.Decode([]byte(out))
}

func writeUsage(writer io.Writer, snapshot usagecore.Snapshot, opts usageOptions, lang i18n.Language, pal usagecore.Palette) error {
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

func usageLines(snapshot usagecore.Snapshot, view string, lang i18n.Language, pal usagecore.Palette) ([]string, error) {
	switch view {
	case usageViewCost:
		return usagecore.CostLines(snapshot, lang, time.Local, pal)
	case usageViewTokens:
		return usagecore.TokenLines(snapshot, lang, pal)
	default:
		return usagecore.LimitLines(snapshot, lang, pal)
	}
}
