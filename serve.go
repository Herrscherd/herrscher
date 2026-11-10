package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/language"
	"github.com/Herrscherd/herrscher/core/scope"
	"github.com/Herrscherd/herrscher/platformtext"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

var errNoGateway = errors.New("no gateway built")

const hubStopTimeout = 15 * time.Second

func or(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

func withCompactionConfig(opts *host.Options, cfg config.Config) {
	opts.CompactPercent = cfg.CompactAt
	opts.CompactStack = cfg.CompactStack
	opts.ContextLimits = cfg.ContextLimits
}

type serveFunc func(ctx context.Context, args []string, cfg config.Config, langs language.Languages) error

func runServe(ctx context.Context, args []string, serve serveFunc) error {
	cfg, err := config.Load(scanFlag(args, "config", config.DefaultPath()))
	if err != nil {
		return err
	}
	langs, err := serveLanguages(slog.Default(), cfg, args)
	if err != nil {
		return err
	}
	return serve(ctx, args, cfg, langs)
}

func serveDaemon(ctx context.Context, args []string, cfg config.Config, langs language.Languages) error {
	return serveWith(ctx, args, nil, cfg, langs)
}

func serveLanguages(log *slog.Logger, cfg config.Config, args []string) (language.Languages, error) {
	return prepareLanguages(log, cfg, stateFlagPath(args))
}

func prepareLanguages(log *slog.Logger, cfg config.Config, statePath string) (language.Languages, error) {
	stored, err := host.StoredOperatorLanguage(statePath)
	if err != nil {
		log.Warn("operator language setting unreadable, using the configured one", "err", err)
	}
	langs := language.Resolve(log, cfg.Language, stored)
	if !language.Explicit(cfg.Language) && stored == "" {
		return langs, nil
	}
	if err := language.Export(langs.Operator); err != nil {
		return language.Languages{}, err
	}
	return langs, nil
}

func serveWith(ctx context.Context, args []string, open *openWindow, cfg config.Config, langs language.Languages) error {
	translator := clitext.For(langs.Operator)
	platform := platformtext.For(langs.Operator)
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.String("config", config.DefaultPath(), translator.Text(clitext.ServeFlagConfig, nil))

	statePath := flags.String("state", host.DefaultStatePath(), translator.Text(clitext.ServeFlagState, nil))
	defaultCmd := flags.String("cmd", or(cfg.Cmd, "claude"), translator.Text(clitext.ServeFlagCmd, nil))
	healthAddr := flags.String("health-addr", cfg.HealthAddr, translator.Text(clitext.ServeFlagHealthAddr, nil))
	statusChannel := flags.String("status-channel", cfg.StatusChannel, translator.Text(clitext.ServeFlagStatusChannel, nil))
	instanceID := flags.String("instance", or(envx.Get("INSTANCE_ID"), cfg.Instance), platform.Text(platformtext.ServeFlagInstance))
	envFile := flags.String("env-file", "", platform.Text(platformtext.ServeFlagEnvFile))
	if err := errors.Join(translator.Err(), platform.Err()); err != nil {
		return err
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *envFile != "" {
		if loadErr := loadEnvFile(*envFile, langs.Operator); loadErr != nil {
			return loadErr
		}
	}
	models := host.ModelDiscoveryFor(*statePath)
	owner := or(envx.Get("OWNER_ID"), cfg.Owner)
	var home *host.HomeRef
	if cfg.Home != nil && cfg.Home.ID != "" {
		home = &host.HomeRef{ID: cfg.Home.ID, Type: cfg.Home.Type}
	}

	unlock, attached, err := lockOrAttach(ctx, langs.Operator, *statePath, *instanceID, open)
	if attached || err != nil {
		return err
	}
	defer unlock()

	if skillsErr := installShippedSkills(ctx, langs.Operator); skillsErr != nil {
		return skillsErr
	}

	gws, closers, foreground, extra, err := startGateways(ctx, langs.Operator)
	if err != nil {
		return err
	}

	opts := host.Options{
		StatePath:           *statePath,
		Language:            langs.Operator,
		DefaultLanguage:     langs.Default,
		Models:              models,
		DefaultCmd:          *defaultCmd,
		HealthAddr:          *healthAddr,
		StatusChannel:       *statusChannel,
		InstanceID:          *instanceID,
		Owner:               owner,
		Home:                home,
		Workspace:           cfg.Workspace,
		Source:              cfg.Source,
		RemoteCategories:    remoteCategories(),
		ForegroundBound:     foreground != nil,
		ContributedCommands: extra,
	}

	withCompactionConfig(&opts, cfg)

	openOnForeground(open, foreground)
	models.StartBackground(ctx, contracts.Default.Backends())
	return runHubForeground(ctx, *statePath, gws, closers, foreground, opts)
}

func openOnForeground(open *openWindow, foreground contracts.Foreground) {
	if open == nil || foreground == nil {
		return
	}
	if o, ok := foreground.(foregroundOpener); ok {
		o.OpenOn(open.session, open.text)
	}
}

func lockOrAttach(ctx context.Context, lang i18n.Language, statePath, instanceID string, open *openWindow) (func(), bool, error) {
	unlock, err := host.LockState(statePath)
	if err == nil {
		return unlock, false, nil
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		var opts []tui.Option
		if open != nil {
			opts = append(opts, tui.OpenOn(open.session, open.text))
		}
		aerr := newAttachRunner(slog.Default()).runAttached(ctx, host.ServedInstanceID(statePath, instanceID), statePath, opts...)
		if aerr == nil {
			return nil, true, nil
		}
		fmt.Fprintln(os.Stderr, attachFailureText(lang, aerr))
	}
	return nil, false, host.LocalizeError(lang, err)
}

func attachFailureText(lang i18n.Language, err error) string {
	return render(lang, clitext.ServeAttachFailed, clitext.Params{clitext.ParamErr: host.LocalizeError(lang, err).Error()})
}

func startGateways(ctx context.Context, lang i18n.Language) ([]host.Deps, []contracts.Gateway, contracts.Foreground, []contracts.Cmd, error) {
	hub, err := host.BuildHub(ctx, contracts.Default.Gateways(), os.Getenv)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for _, f := range hub.Unconfigured() {
		fmt.Fprintln(os.Stderr, render(lang, clitext.ServeGatewaySkipped, clitext.Params{clitext.ParamFailure: f}))
	}
	hub.AwaitPending(ctx, host.GatewayRetryWindow, func(failures []string, wait time.Duration) {
		for _, f := range failures {
			fmt.Fprintln(os.Stderr, render(lang, clitext.ServeGatewayRetrying, clitext.Params{clitext.ParamFailure: f, clitext.ParamWait: wait.String()}))
		}
	})
	for _, f := range hub.Failures() {
		fmt.Fprintln(os.Stderr, render(lang, clitext.ServeGatewayUnavailable, clitext.Params{clitext.ParamFailure: f}))
	}
	var raw []host.Deps
	for _, kind := range hub.Kinds() {
		if set, ok := hub.Get(kind); ok {
			raw = append(raw, set)
		}
	}
	return bindGateways(raw, lang)
}

func runHubForeground(ctx context.Context, statePath string, gws []host.Deps, closers []contracts.Gateway, foreground contracts.Foreground, opts host.Options) error {
	if foreground == nil || !term.IsTerminal(int(os.Stdout.Fd())) {
		return runHub(ctx, gws, closers, opts)
	}
	if restore, rerr := redirectStderrCapped(filepath.Join(filepath.Dir(statePath), "serve.log"), maxLogFileBytes); rerr == nil {
		defer func() {
			if err := restore(); err != nil {
				slog.Error("restore stderr", "err", err)
			}
		}()
	}
	hubCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	return superviseForeground(hubCtx, cancel, foreground, func(ctx context.Context) error {
		return runHub(ctx, gws, closers, opts)
	})
}

func superviseForeground(ctx context.Context, cancel context.CancelFunc, foreground contracts.Foreground, hub func(context.Context) error) error {
	hubDone := make(chan error, 1)
	go func() {
		err := hub(ctx)
		cancel()
		hubDone <- err
	}()
	foregroundErr := foreground.RunForeground(ctx, cancel)
	cancel()
	select {
	case err := <-hubDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("hub stopped", "err", err)
			return errors.Join(foregroundErr, renderError(clitext.ClientLanguage(), err, clitext.ServeHubStopped, clitext.Params{clitext.ParamErr: err.Error()}))
		}
	case <-time.After(hubStopTimeout):
		slog.Error("hub did not stop in time", "timeout", hubStopTimeout)
	}
	return foregroundErr
}

func runHub(ctx context.Context, gws []host.Deps, closers []contracts.Gateway, opts host.Options) error {
	return host.RunHub(ctx, gws, closers, opts)
}

func bindGateways(sets []host.Deps, lang i18n.Language) ([]host.Deps, []contracts.Gateway, contracts.Foreground, []contracts.Cmd, error) {
	extra, err := host.CollectCommands(sets)
	if err != nil {
		return nil, nil, nil, nil, renderError(lang, err, clitext.ServeGatewayCommands, clitext.Params{clitext.ParamErr: err.Error()})
	}
	var foreground contracts.Foreground
	gws := make([]host.Deps, 0, len(sets))
	closers := make([]contracts.Gateway, 0, len(sets))
	for _, set := range sets {
		if f, ok := set.Gateway.(contracts.Foreground); ok && foreground == nil {
			foreground = f
		}
		closers = append(closers, set.Gateway)
		set.Gateway = contracts.Degrade(set.Gateway)
		gws = append(gws, set)
	}
	return gws, closers, foreground, extra, nil
}

func runSession(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, verbSession, args)
}

func runAgent(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "agent", args)
}

func runHost(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "host", args)
}

func runApprove(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "hook" {
		host.RunApprovalHook(ctx, os.Stdin, os.Stdout, os.Stderr)
		return nil
	}
	return runRegistryVerb(ctx, "approve", args)
}

func runRole(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "role", args)
}

func runMemory(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "memory", args)
}

func runModels(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "models", args)
}

func loadOperator() (config.Config, language.Languages, error) {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return config.Config{}, language.Languages{}, err
	}
	langs, err := prepareLanguages(slog.Default(), cfg, host.DefaultStatePath())
	return cfg, langs, err
}

func newOperatorRegistry(ctx context.Context, cfg config.Config, lang i18n.Language) (*cli.Registry, error) {
	gws, err := buildGateways(ctx, lang)
	if err != nil {
		return nil, err
	}
	var home *host.HomeRef
	if cfg.Home != nil && cfg.Home.ID != "" {
		home = &host.HomeRef{ID: cfg.Home.ID, Type: cfg.Home.Type}
	}
	return host.NewRegistry(ctx, gws, host.Options{
		StatePath:  host.DefaultStatePath(),
		DefaultCmd: or(cfg.Cmd, "claude"),
		InstanceID: or(envx.Get("INSTANCE_ID"), cfg.Instance),
		Owner:      or(envx.Get("OWNER_ID"), cfg.Owner),
		Home:       home,
		Workspace:  cfg.Workspace,
		Source:     cfg.Source,
	})
}

func runRegistryVerb(ctx context.Context, verb string, args []string) error {
	cfg, langs, err := loadOperator()
	if err != nil {
		return err
	}
	reg, err := newOperatorRegistry(ctx, cfg, langs.Client)
	if err != nil {
		return err
	}
	res, dispatchErr := reg.Dispatch(ctx, append([]string{verb}, args...))
	out, err := cli.Render(res, dispatchErr, langs.Client)
	if err != nil {
		return err
	}
	if out != "" {
		_, err := fmt.Fprintln(os.Stdout, out)
		return err
	}
	return nil
}

const promptTimeout = 30 * time.Minute

type verbDispatcher interface {
	Dispatch(context.Context, []string) (contracts.Result, error)
}

func runPrompt(ctx context.Context, task task) error {
	name := sessionNameFor(task.Text)
	if task.printsTo(term.IsTerminal(int(os.Stdout.Fd())) && hasTerminalGateway()) {
		cfg, langs, err := loadOperator()
		if err != nil {
			return err
		}
		reg, err := newOperatorRegistry(ctx, cfg, langs.Client)
		if err != nil {
			return err
		}
		return runPromptWith(ctx, reg, name, task.Text, langs.Client, os.Stdout, os.Stderr)
	}
	return runPromptWindow(ctx, name, task.Text)
}

func createTaskSession(ctx context.Context, name string) []string {
	return append([]string{verbSession, "create", argName, name, "--terminal_only"},
		scope.LaunchFromEnv(ctx).Args()...)
}

func runPromptWindow(ctx context.Context, name, text string) error {
	lang := clitext.ClientLanguage()
	statePath := host.DefaultStatePath()
	unlock, lerr := host.LockState(statePath)
	if lerr != nil {
		instance := host.ServedInstanceID(statePath, "")
		if _, err := host.DaemonDispatch(ctx, instance, createTaskSession(ctx, name)); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, render(lang, clitext.ServeSessionStarted, clitext.Params{clitext.ParamName: name}))
		return newAttachRunner(slog.Default()).runAttached(ctx, instance, statePath, tui.OpenOn(name, text))
	}
	unlock()
	cfg, langs, err := loadOperator()
	if err != nil {
		return err
	}
	reg, err := newOperatorRegistry(ctx, cfg, langs.Client)
	if err != nil {
		return err
	}
	if _, err := reg.Dispatch(ctx, createTaskSession(ctx, name)); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, render(lang, clitext.ServeSessionStarted, clitext.Params{clitext.ParamName: name}))
	return serveWith(ctx, nil, &openWindow{session: name, text: text}, cfg, langs)
}

type openWindow struct {
	session string
	text    string
}

type foregroundOpener interface {
	OpenOn(session, text string)
}

func runPromptWith(ctx context.Context, dispatcher verbDispatcher, name, prompt string, lang i18n.Language, stdout, stderr io.Writer) error {
	if _, err := dispatcher.Dispatch(ctx, createTaskSession(ctx, name)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stderr, render(lang, clitext.ServeSessionStarted, clitext.Params{clitext.ParamName: name})); err != nil {
		return err
	}
	argv := append([]string{verbSession, "seed", argName, name}, cli.FlagArg("task", prompt)...)
	argv = append(argv, "--timeout", promptTimeout.String())
	res, err := dispatcher.Dispatch(ctx, argv)
	if err != nil {
		localized := cli.Localize(err, lang)
		return renderError(lang, localized, clitext.ServeSessionFailed, clitext.Params{clitext.ParamName: name, clitext.ParamErr: localized.Error()})
	}
	reply, err := cli.Text(res, lang)
	if err != nil {
		return err
	}
	if reply != "" {
		if _, err := fmt.Fprintln(stdout, reply); err != nil {
			return err
		}
	}
	return nil
}

const forwardTimeout = 60 * time.Second

func forwardUnknownVerb(ctx context.Context, argv []string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, forwardTimeout)
	defer cancel()
	instance := envx.Get("INSTANCE_ID")
	if instance == "" {
		if cfg, err := config.Load(config.DefaultPath()); err == nil {
			instance = cfg.Instance
		}
	}
	return host.ForwardToDaemon(ctx, host.DefaultStatePath(), instance, argv)
}

func buildGateways(ctx context.Context, lang i18n.Language) ([]host.Deps, error) {
	hub, err := host.BuildHub(ctx, contracts.Default.Gateways(), os.Getenv)
	if err != nil {
		return nil, err
	}
	var sets []host.Deps
	for _, kind := range hub.Kinds() {
		if set, ok := hub.Get(kind); ok {
			sets = append(sets, set)
		}
	}
	if len(sets) == 0 {
		return nil, renderError(lang, errNoGateway, clitext.ServeNoGateway, nil)
	}
	return sets, nil
}

func stateFlagPath(args []string) string {
	return scanFlag(args, "state", host.DefaultStatePath())
}

func scanFlag(args []string, name, def string) string {
	for idx, arg := range args {
		for _, prefix := range []string{"--" + name, "-" + name} {
			if arg == prefix {
				if idx+1 < len(args) {
					return args[idx+1]
				}
				return def
			}
			if v, ok := strings.CutPrefix(arg, prefix+"="); ok {
				return v
			}
		}
	}
	return def
}
