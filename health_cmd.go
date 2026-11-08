package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/host"
)

const (
	healthVerb          = "health"
	healthWatchInterval = 15 * time.Second
	healthParamDeep     = "--deep"
	healthParamWatch    = "--watch"
	healthParamQuiet    = "--quiet"
)

type healthOptions struct {
	service string
	deep    bool
	watch   bool
	asJSON  bool
	quiet   bool
}

func runHealth(ctx context.Context, args []string) error {
	lang := clitext.ClientLanguage()
	opts, unknown := parseHealthArgs(args)
	if unknown != "" {
		return renderError(lang, errUnknownArgument, clitext.FlagUnknown, clitext.Params{clitext.ParamCommand: healthVerb, clitext.ParamName: strconv.Quote(unknown)})
	}
	instance := host.ServedInstanceID(host.DefaultStatePath(), "")
	if opts.watch {
		return watchHealth(ctx, instance, opts, lang)
	}
	code, err := checkHealth(ctx, instance, opts, lang, os.Stdout)
	if err != nil {
		return err
	}
	if code != int(health.ExitHealthy) {
		return exitStatusError{code: code}
	}
	return nil
}

func watchHealth(ctx context.Context, instance string, opts healthOptions, lang i18n.Language) error {
	ticker := time.NewTicker(healthWatchInterval)
	defer ticker.Stop()
	for {
		if _, err := checkHealth(ctx, instance, opts, lang, os.Stdout); err != nil {
			return err
		}
		if err := writeRefreshed(os.Stdout, lang); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func writeRefreshed(writer io.Writer, lang i18n.Language) error {
	text, err := clitext.Text(lang, clitext.HealthWatchRefreshedAt, clitext.Params{clitext.ParamTime: time.Now().Format(time.TimeOnly)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(writer, text)
	return err
}

func checkHealth(ctx context.Context, instance string, opts healthOptions, lang i18n.Language, writer io.Writer) (int, error) {
	snapshot, err := fetchHealth(ctx, instance, opts.deep)
	if host.IsDaemonNotRunning(err) {
		text, textErr := clitext.Text(lang, clitext.HealthDaemonDown, clitext.Params{clitext.ParamSocket: host.CommandSocketPath(instance)})
		if textErr != nil {
			return 0, textErr
		}
		if _, writeErr := fmt.Fprintln(os.Stderr, text); writeErr != nil {
			return 0, writeErr
		}
		return int(health.ExitDown), nil
	}
	if err != nil {
		return 0, err
	}
	checks := snapshot.Checks
	if opts.service != "" {
		checks = snapshot.Find(opts.service)
		if len(checks) == 0 {
			return 0, renderError(lang, errUnknownArgument, clitext.HealthUnknownService, clitext.Params{clitext.ParamName: strconv.Quote(opts.service)})
		}
	}
	code := int(health.Summarize(checks).ExitCode())
	if opts.quiet {
		return code, nil
	}
	return code, writeHealth(writer, snapshot, checks, opts, lang)
}

func writeHealth(writer io.Writer, snapshot health.Snapshot, checks []health.Check, opts healthOptions, lang i18n.Language) error {
	if opts.asJSON {
		snapshot.Checks = checks
		raw, err := health.Encode(snapshot)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(writer, string(raw))
		return err
	}
	if opts.service != "" {
		return snapshot.Details(writer, checks, lang)
	}
	return snapshot.Table(writer, lang)
}

func fetchHealth(ctx context.Context, instance string, deep bool) (health.Snapshot, error) {
	argv := []string{healthVerb}
	if deep {
		argv = append(argv, healthParamDeep)
	}
	out, err := host.DaemonDispatch(ctx, instance, argv)
	if err != nil {
		return health.Snapshot{}, err
	}
	return health.Decode([]byte(out))
}

func parseHealthArgs(args []string) (healthOptions, string) {
	var opts healthOptions
	for _, arg := range args {
		switch arg {
		case healthParamDeep:
			opts.deep = true
		case healthParamWatch:
			opts.watch = true
		case argJSON:
			opts.asJSON = true
		case healthParamQuiet:
			opts.quiet = true
		default:
			if opts.service != "" || len(arg) > 1 && arg[:2] == "--" {
				return opts, arg
			}
			opts.service = arg
		}
	}
	return opts, ""
}
