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
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/scope"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

var errNoGateway = errors.New("no gateway built")

func or(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

func runServe(ctx context.Context, args []string, open *openWindow) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.String("config", config.DefaultPath(), "path to the declarative config.json (optional)")
	cfg, err := config.Load(scanFlag(args, "config", config.DefaultPath()))
	if err != nil {
		return err
	}

	statePath := flags.String("state", host.DefaultStatePath(), "path to the daemon state file")
	defaultCmd := flags.String("cmd", or(cfg.Cmd, "claude"), "default bridged base command for new sessions (stream-json mode adds -p and the stream flags)")
	healthAddr := flags.String("health-addr", cfg.HealthAddr, "if set (e.g. :8787), serve GET /health")
	statusChannel := flags.String("status-channel", cfg.StatusChannel, "if set, maintain a self-updating status embed there")
	instanceID := flags.String("instance", or(envx.Get("INSTANCE_ID"), cfg.Instance), "per-daemon instance id (slug) used to namespace shared Discord/git resources; defaults to HERRSCHER_INSTANCE_ID then config.json")
	envFile := flags.String("env-file", "", "load DISCORD_BOT_TOKEN and other vars from this file before starting (used by `herrscher service`)")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *envFile != "" {
		if loadErr := loadEnvFile(*envFile); loadErr != nil {
			return loadErr
		}
	}
	owner := or(envx.Get("OWNER_ID"), cfg.Owner)
	var home *host.HomeRef
	if cfg.Home != nil && cfg.Home.ID != "" {
		home = &host.HomeRef{ID: cfg.Home.ID, Type: cfg.Home.Type}
	}

	unlock, attached, err := lockOrAttach(ctx, *statePath, *instanceID, open)
	if attached || err != nil {
		return err
	}
	defer unlock()

	installShippedSkills(ctx)

	gws, foreground, extra, err := startGateways(ctx)
	if err != nil {
		return err
	}

	opts := host.Options{
		StatePath:     *statePath,
		DefaultCmd:    *defaultCmd,
		HealthAddr:    *healthAddr,
		StatusChannel: *statusChannel,
		InstanceID:    *instanceID,
		Owner:         owner,
		Home:          home,
		Workspace:     cfg.Workspace,
		Source:        cfg.Source,

		RemoteCategories:    remoteCategories(),
		ForegroundBound:     foreground != nil,
		ContributedCommands: extra,
	}

	openOnForeground(open, foreground)
	return runHubForeground(ctx, *statePath, gws, foreground, opts)
}

func openOnForeground(open *openWindow, foreground contracts.Foreground) {
	if open == nil || foreground == nil {
		return
	}
	if o, ok := foreground.(foregroundOpener); ok {
		o.OpenOn(open.session, open.text)
	}
}

func lockOrAttach(ctx context.Context, statePath, instanceID string, open *openWindow) (func(), bool, error) {
	unlock, err := host.LockState(statePath)
	if err == nil {
		return unlock, false, nil
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		var opts []tui.Option
		if open != nil {
			opts = append(opts, tui.OpenOn(open.session, open.text))
		}
		aerr := runAttached(ctx, host.ServedInstanceID(statePath, instanceID), opts...)
		if aerr == nil {
			return nil, true, nil
		}
		_, _ = fmt.Fprintf(os.Stderr, "herrscher: could not attach to the running daemon: %v\n", aerr)
	}
	return nil, false, err
}

func startGateways(ctx context.Context) ([]host.Deps, contracts.Foreground, []contracts.Cmd, error) {
	hub, err := host.BuildHub(ctx, contracts.Default.Gateways(), os.Getenv)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, f := range hub.Unconfigured() {
		_, _ = fmt.Fprintln(os.Stderr, "herrscher: gateway not configured, skipping: "+f)
	}
	hub.AwaitPending(ctx, host.GatewayRetryWindow, func(failures []string, wait time.Duration) {
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "herrscher: gateway not up yet: %s; retrying in %s\n", f, wait)
		}
	})
	for _, f := range hub.Failures() {
		_, _ = fmt.Fprintln(os.Stderr, "herrscher: gateway unavailable: "+f)
	}
	var raw []host.Deps
	for _, kind := range hub.Kinds() {
		if set, ok := hub.Get(kind); ok {
			raw = append(raw, set)
		}
	}
	return bindGateways(raw)
}

func runHubForeground(ctx context.Context, statePath string, gws []host.Deps, foreground contracts.Foreground, opts host.Options) error {
	if foreground == nil || !term.IsTerminal(int(os.Stdout.Fd())) {
		return host.RunHub(ctx, gws, opts)
	}
	if restore, rerr := redirectStderr(filepath.Join(filepath.Dir(statePath), "serve.log")); rerr == nil {
		defer func() {
			if err := restore(); err != nil {
				slog.Error("restore stderr", "err", err)
			}
		}()
	}
	hubCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		if err := host.RunHub(hubCtx, gws, opts); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("hub stopped", "err", err)
		}
	}()
	return foreground.RunForeground(hubCtx, cancel)
}

func bindGateways(sets []host.Deps) ([]host.Deps, contracts.Foreground, []contracts.Cmd, error) {
	extra, err := host.CollectCommands(sets)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("gateway commands: %w", err)
	}
	var foreground contracts.Foreground
	gws := make([]host.Deps, 0, len(sets))
	for _, set := range sets {
		if f, ok := set.Gateway.(contracts.Foreground); ok && foreground == nil {
			foreground = f
		}
		set.Gateway = contracts.Degrade(set.Gateway)
		gws = append(gws, set)
	}
	return gws, foreground, extra, nil
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

func newOperatorRegistry(ctx context.Context) (*cli.Registry, error) {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return nil, err
	}
	gws, err := buildGateways(ctx)
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
	reg, err := newOperatorRegistry(ctx)
	if err != nil {
		return err
	}
	out, err := reg.Dispatch(ctx, append([]string{verb}, args...))
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
	Dispatch(context.Context, []string) (string, error)
}

func runPrompt(ctx context.Context, task task) error {
	name := sessionNameFor(task.Text)
	if task.printsTo(term.IsTerminal(int(os.Stdout.Fd())) && hasTerminalGateway()) {
		reg, err := newOperatorRegistry(ctx)
		if err != nil {
			return err
		}
		return runPromptWith(ctx, reg, name, task.Text, os.Stdout, os.Stderr)
	}
	return runPromptWindow(ctx, name, task.Text)
}

func createTaskSession(ctx context.Context, name string) []string {
	return append([]string{verbSession, "create", argName, name, "--terminal_only"},
		scope.LaunchFromEnv(ctx).Args()...)
}

func runPromptWindow(ctx context.Context, name, text string) error {
	statePath := host.DefaultStatePath()
	unlock, lerr := host.LockState(statePath)
	if lerr != nil {
		instance := host.ServedInstanceID(statePath, "")
		if _, err := host.DaemonDispatch(ctx, instance, createTaskSession(ctx, name)); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(os.Stderr, "session: "+name)
		return runAttached(ctx, instance, tui.OpenOn(name, text))
	}
	unlock()
	reg, err := newOperatorRegistry(ctx)
	if err != nil {
		return err
	}
	if _, err := reg.Dispatch(ctx, createTaskSession(ctx, name)); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(os.Stderr, "session: "+name)
	return runServe(ctx, nil, &openWindow{session: name, text: text})
}

type openWindow struct {
	session string
	text    string
}

type foregroundOpener interface {
	OpenOn(session, text string)
}

func runPromptWith(ctx context.Context, dispatcher verbDispatcher, name, prompt string, stdout, stderr io.Writer) error {
	if _, err := dispatcher.Dispatch(ctx, createTaskSession(ctx, name)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stderr, "session: "+name); err != nil {
		return err
	}
	argv := append([]string{verbSession, "seed", argName, name}, cli.FlagArg("task", prompt)...)
	argv = append(argv, "--timeout", promptTimeout.String())
	reply, err := dispatcher.Dispatch(ctx, argv)
	if err != nil {
		return fmt.Errorf("session %s: %w", name, err)
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

func buildGateways(ctx context.Context) ([]host.Deps, error) {
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
		return nil, errNoGateway
	}
	return sets, nil
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
