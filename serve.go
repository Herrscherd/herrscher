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

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func runServe(ctx context.Context, args []string, open *openWindow) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.String("config", config.DefaultPath(), "path to the declarative config.json (optional)")
	cfg, err := config.Load(scanFlag(args, "config", config.DefaultPath()))
	if err != nil {
		return err
	}

	statePath := fs.String("state", host.DefaultStatePath(), "path to the daemon state file")
	defaultCmd := fs.String("cmd", or(cfg.Cmd, "claude"), "default bridged base command for new sessions (stream-json mode adds -p and the stream flags)")
	healthAddr := fs.String("health-addr", cfg.HealthAddr, "if set (e.g. :8787), serve GET /health")
	statusChannel := fs.String("status-channel", cfg.StatusChannel, "if set, maintain a self-updating status embed there")
	instanceID := fs.String("instance", or(envx.Get("INSTANCE_ID"), cfg.Instance), "per-daemon instance id (slug) used to namespace shared Discord/git resources; defaults to HERRSCHER_INSTANCE_ID then config.json")
	envFile := fs.String("env-file", "", "load DISCORD_BOT_TOKEN and other vars from this file before starting (used by `herrscher service`)")
	if err = fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *envFile != "" {
		if err = loadEnvFile(*envFile); err != nil {
			return err
		}
	}
	owner := or(envx.Get("OWNER_ID"), cfg.Owner)
	var home *host.HomeRef
	if cfg.Home != nil && cfg.Home.ID != "" {
		home = &host.HomeRef{ID: cfg.Home.ID, Type: cfg.Home.Type}
	}

	unlock, err := host.LockState(*statePath)
	if err != nil {
		if term.IsTerminal(int(os.Stdout.Fd())) {
			var opts []tui.Option
			if open != nil {
				opts = append(opts, tui.OpenOn(open.session, open.text))
			}
			aerr := runAttached(ctx, host.ServedInstanceID(*statePath, *instanceID), opts...)
			if aerr == nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "herrscher: could not attach to the running daemon: %v\n", aerr)
		}
		return err
	}
	defer unlock()

	installShippedSkills(ctx)

	hub, err := host.BuildHub(ctx, contracts.Default.Gateways(), os.Getenv)
	if err != nil {
		return err
	}
	for _, f := range hub.Unconfigured() {
		fmt.Fprintln(os.Stderr, "herrscher: gateway not configured, skipping: "+f)
	}
	hub.AwaitPending(ctx, host.GatewayRetryWindow, func(failures []string, in time.Duration) {
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "herrscher: gateway not up yet: %s; retrying in %s\n", f, in)
		}
	})
	for _, f := range hub.Failures() {
		fmt.Fprintln(os.Stderr, "herrscher: gateway unavailable: "+f)
	}
	var raw []host.Deps
	for _, kind := range hub.Kinds() {
		if set, ok := hub.Get(kind); ok {
			raw = append(raw, set)
		}
	}
	gws, fg, extra, err := bindGateways(raw)
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
		ForegroundBound:     fg != nil,
		ContributedCommands: extra,
	}

	if open != nil && fg != nil {
		if o, ok := fg.(foregroundOpener); ok {
			o.OpenOn(open.session, open.text)
		}
	}
	if fg != nil && term.IsTerminal(int(os.Stdout.Fd())) {
		if restore, rerr := redirectStderr(filepath.Join(filepath.Dir(*statePath), "serve.log")); rerr == nil {
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
		return fg.RunForeground(hubCtx, cancel)
	}
	return host.RunHub(ctx, gws, opts)
}

func bindGateways(sets []host.Deps) ([]host.Deps, contracts.Foreground, []contracts.Cmd, error) {
	extra, err := host.CollectCommands(sets)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("gateway commands: %w", err)
	}
	var fg contracts.Foreground
	gws := make([]host.Deps, 0, len(sets))
	for _, set := range sets {
		if f, ok := set.Gateway.(contracts.Foreground); ok && fg == nil {
			fg = f
		}
		set.Gateway = contracts.Degrade(set.Gateway)
		gws = append(gws, set)
	}
	return gws, fg, extra, nil
}

func runSession(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "session", args)
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
		fmt.Println(out)
	}
	return nil
}

const promptTimeout = 30 * time.Minute

type verbDispatcher interface {
	Dispatch(context.Context, []string) (string, error)
}

func runPrompt(ctx context.Context, t task) error {
	name := sessionNameFor(t.Text)
	if t.printsTo(term.IsTerminal(int(os.Stdout.Fd())) && hasTerminalGateway()) {
		reg, err := newOperatorRegistry(ctx)
		if err != nil {
			return err
		}
		return runPromptWith(ctx, reg, name, t.Text, os.Stdout, os.Stderr)
	}
	return runPromptWindow(ctx, name, t.Text)
}

func createTaskSession(name string) []string {
	return append([]string{"session", "create", "--name", name, "--terminal_only"},
		scope.LaunchFromEnv().Args()...)
}

func runPromptWindow(ctx context.Context, name, text string) error {
	statePath := host.DefaultStatePath()
	unlock, lerr := host.LockState(statePath)
	if lerr != nil {
		instance := host.ServedInstanceID(statePath, "")
		if _, err := host.DaemonDispatch(ctx, instance, createTaskSession(name)); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "session: "+name)
		return runAttached(ctx, instance, tui.OpenOn(name, text))
	}
	unlock()
	reg, err := newOperatorRegistry(ctx)
	if err != nil {
		return err
	}
	if _, err := reg.Dispatch(ctx, createTaskSession(name)); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "session: "+name)
	return runServe(ctx, nil, &openWindow{session: name, text: text})
}

type openWindow struct {
	session string
	text    string
}

type foregroundOpener interface {
	OpenOn(session, text string)
}

func runPromptWith(ctx context.Context, d verbDispatcher, name, prompt string, stdout, stderr io.Writer) error {
	if _, err := d.Dispatch(ctx, createTaskSession(name)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stderr, "session: "+name); err != nil {
		return err
	}
	argv := append([]string{"session", "seed", "--name", name}, cli.FlagArg("task", prompt)...)
	argv = append(argv, "--timeout", promptTimeout.String())
	reply, err := d.Dispatch(ctx, argv)
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
		return nil, fmt.Errorf("no gateway built")
	}
	return sets, nil
}

func scanFlag(args []string, name, def string) string {
	for i, a := range args {
		for _, p := range []string{"--" + name, "-" + name} {
			if a == p {
				if i+1 < len(args) {
					return args[i+1]
				}
				return def
			}
			if v, ok := strings.CutPrefix(a, p+"="); ok {
				return v
			}
		}
	}
	return def
}
