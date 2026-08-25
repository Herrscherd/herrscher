package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/scope"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

// or returns a if non-empty, else b — used to layer config.json defaults under
// env vars when seeding a flag's default value.
func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// runServe builds the daemon and runs it, optionally opening its foreground on a
// session already created for a task (open, nil for a plain serve).
func runServe(ctx context.Context, args []string, open *openWindow) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	// --config is read up front (before Parse) so config.json can seed the other
	// flags' defaults; it's still registered for --help and validation.
	fs.String("config", config.DefaultPath(), "path to the declarative config.json (optional)")
	cfg, err := config.Load(scanFlag(args, "config", config.DefaultPath()))
	if err != nil {
		return err
	}

	statePath := fs.String("state", host.DefaultStatePath(), "path to the daemon state file")
	// Flag defaults are layered as: env > config.json > built-in. An explicitly
	// passed flag then wins naturally (Parse overwrites the default).
	defaultCmd := fs.String("cmd", or(cfg.Cmd, "claude"), "default bridged base command for new sessions (stream-json mode adds -p and the stream flags)")
	healthAddr := fs.String("health-addr", cfg.HealthAddr, "if set (e.g. :8787), serve GET /health")
	statusChannel := fs.String("status-channel", cfg.StatusChannel, "if set, maintain a self-updating status embed there")
	instanceID := fs.String("instance", or(envx.Get("INSTANCE_ID"), cfg.Instance), "per-daemon instance id (slug) used to namespace shared Discord/git resources; defaults to HERRSCHER_INSTANCE_ID then config.json")
	envFile := fs.String("env-file", "", "load DISCORD_BOT_TOKEN and other vars from this file before starting (used by `herrscher service`)")
	fs.Parse(args)
	if *envFile != "" {
		// Load secrets in Go rather than via a shell/batch wrapper, so the gateway
		// plugin's config resolution below sees them (the service passes its file
		// here; the implicit ./.env was already loaded in main).
		if err := loadEnvFile(*envFile); err != nil {
			return err
		}
	}
	// Owner: env HERRSCHER_OWNER_ID wins over config.json
	// (the owner id is kept in env alongside the token), then config seeds it for
	// declarative setups.
	owner := or(envx.Get("OWNER_ID"), cfg.Owner)
	var home *host.HomeRef
	if cfg.Home != nil && cfg.Home.ID != "" {
		home = &host.HomeRef{ID: cfg.Home.ID, Type: cfg.Home.Type}
	}

	// Claim the state file before anything observable happens — before a gateway
	// is dialled, a skill is written, or a session is adopted. A second daemon on
	// the same state answers every message twice, so it must be refused while the
	// refusal is still just an error message.
	unlock, err := host.LockState(*statePath)
	if err != nil {
		// Somebody else is serving. On a terminal that is almost never a mistake to
		// report and walk away from: the operator wants to see the agent that is
		// already running, and the daemon exposes exactly the two sockets a
		// frontend needs. Attach instead of refusing. Off a terminal there is no
		// TUI to attach, so the refusal stands.
		if term.IsTerminal(int(os.Stdout.Fd())) {
			// Not *instanceID: the daemon that holds the lock named its sockets
			// after the id it froze in this very state file, which is resolved
			// from the owner when no --instance was passed. Ours is usually
			// empty, and an empty id composes a socket path nobody listens on.
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

	// The playbooks ship inside the binary; put them on disk before any session
	// can be told to follow one.
	installShippedSkills(ctx)

	// Registry-driven wiring: the daemon instantiates every gateway from the
	// plugin registry rather than hand-wiring Discord. Each plugin self-registered
	// into contracts.Default from its init() (blank import in plugins.go); here we
	// build each one's GatewaySet from runtime config. Adding a gateway is a blank
	// import + rebuild — no code change here.
	hub, err := host.BuildHub(ctx, contracts.Default.Gateways(), os.Getenv)
	if err != nil {
		return err
	}
	// A gateway that failed to build is skipped so the others still run, but it
	// must not vanish silently: a revoked token takes the whole edge offline and
	// the only symptom would be a bot that never answers.
	//
	// First it gets a second chance. At boot the daemon races the machine's own
	// network, and a factory that could not resolve a name will succeed a few
	// seconds later — while the process itself stayed up, so nothing restarted it
	// and the edge stayed dark until somebody noticed. Nothing is supervising
	// sessions yet at this point, so the waiting costs a slower start and nothing
	// else.
	//
	// A gateway nobody configured is not in that race and is never waited for
	// (see GatewayHub.attempt): its var is unset for the life of the process, so
	// the retry window would only be the operator watching a countdown to a
	// foregone conclusion before the frontend they asked for opens. Say it once
	// and carry on.
	for _, f := range hub.Unconfigured() {
		fmt.Fprintln(os.Stderr, "herrscher: gateway not configured, skipping: "+f)
	}
	hub.AwaitPending(ctx, os.Getenv, host.GatewayRetryWindow, func(failures []string, in time.Duration) {
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

	// A bound foreground gateway + interactive TTY → run it on the main thread;
	// quitting it cancels ctx and stops the daemon. No foreground gateway, or a
	// background service (no TTY) → headless: the hub drives every gateway and
	// nothing takes over the foreground.
	// Point the foreground at the session this run is for, before it starts. A
	// foreground that cannot be opened on one still runs; it simply opens where it
	// always did, which is the honest outcome for a frontend that has no notion of
	// a named window.
	if open != nil && fg != nil {
		if o, ok := fg.(foregroundOpener); ok {
			o.OpenOn(open.session, open.text)
		}
	}
	if fg != nil && term.IsTerminal(int(os.Stdout.Fd())) {
		// The TUI owns the terminal; the background daemon (and libraries that
		// write straight to os.Stderr) must not paint over its alt-screen. Route
		// stderr to a log file beside state.json for the TUI's lifetime.
		if restore, rerr := redirectStderr(filepath.Join(filepath.Dir(*statePath), "serve.log")); rerr == nil {
			defer restore()
		}
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() { _ = host.RunHub(ctx, gws, opts) }()
		return fg.RunForeground(ctx, cancel)
	}
	return host.RunHub(ctx, gws, opts)
}

// bindGateways turns the freshly built gateways into what the daemon runs on.
//
// Everything a gateway offers beyond the plain Gateway interface must be read
// here, off the unwrapped object, because Degrade returns a fixed method set:
// whatever it does not forward stops existing the moment it wraps. Two such
// capabilities exist and both are taken the same way — the foreground the
// process may hand its main thread to, and the verbs a plugin grafts onto the
// daemon's registry. Only then is each gateway degraded and passed on.
//
// The composition root stays gateway-agnostic throughout: it asks interfaces
// what they are rather than importing any concrete frontend or plugin.
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

// runSession dispatches the operator `session` commands (create/close/list/who)
// through the core CLI registry. It builds the same gateway + handler deps the
// daemon uses from config.json + env, so a session created here matches one the
// daemon supervises.
func runSession(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "session", args)
}

func runAgent(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "agent", args)
}

// runHost dispatches the operator `host` commands (add/list/check/provision/rm)
// through the same registry the daemon serves. Provisioning cross-compiles and
// copies a binary, so it runs here rather than inside the daemon's event loop.
func runHost(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "host", args)
}

// runMemory dispatche les commandes opérateur `memory` (locate/forget/record)
// à travers le même registre CLI que la daemon sert.
func runMemory(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "memory", args)
}

// runModels dispatches the operator `models` commands (list) through the same
// CLI registry the daemon serves.
func runModels(ctx context.Context, args []string) error {
	return runRegistryVerb(ctx, "models", args)
}

// newOperatorRegistry builds the operator CLI registry — the same command
// surface the daemon serves, over this short-lived process's own state. Split
// out of runRegistryVerb because a prompt dispatches two verbs and must not
// build the gateway stack twice: each build opens the configured gateways.
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

// runRegistryVerb builds the operator registry (the same one the daemon serves)
// and dispatches a single top-level verb through it, printing any output. Both
// session and agent verbs share this so the binary and the gateways drive an
// identical command surface.
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

// promptTimeout caps the turn a bare prompt runs. A coordination seed is a short
// question and keeps the 120s default; a task typed by hand at a terminal is not
// one, and the operator is sitting there to interrupt it.
const promptTimeout = 30 * time.Minute

// verbDispatcher is what runPromptWith needs of the registry, and nothing more —
// so the sequencing can be tested without a gateway, a daemon or a backend.
type verbDispatcher interface {
	Dispatch(context.Context, []string) (string, error)
}

// runPrompt opens a session on a free-text task.
//
// The default is a window: the session is created, the task is sent into it, and
// the TUI opens on that tab with the turn running in front of the operator, which
// is what a task typed at a terminal is for. --print is the other half — one
// turn, the reply on stdout — and it is also what a run with no terminal gets,
// since there is nothing there to draw a window on and an operator piping the
// binary wants the answer, not a Bubbletea error.
//
// A build with no terminal gateway takes the same fallback, and must: there is
// no frontend to open, so the window path would create the session, start a
// headless daemon, and leave the task sitting in it unsent.
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

// createTaskSession is the argv that opens the session behind `herrscher
// "<task>"`. It asks for the same memory roots the TUI's own default session
// gets: a launch that names its task is no less worth learning from than one
// that does not, and reading the rule from core/scope keeps the composition
// root free of any dependency on the terminal plugin.
func createTaskSession(name string) []string {
	return append([]string{"session", "create", "--name", name, "--terminal_only"},
		scope.LaunchFromEnv().Args()...)
}

// runPromptWindow opens the TUI on a session created for this task.
//
// Which of the two launch paths runs is decided by the state lock, the same
// question `herrscher` on its own asks: a daemon already serving owns the state,
// so the window attaches to it over its sockets and that daemon runs the turn —
// quitting the window leaves both standing. Nothing serving means this process
// becomes the daemon, and its own gateway carries the session in the foreground.
//
// Where the session is created follows from that, and the two are not
// interchangeable. `session create` acts on whichever process runs it: run here
// against a live daemon it would write a session that daemon does not know it
// has, and an attached window — which lists sessions by asking the daemon —
// would wait for a tab that never arrives. So the daemon creates its own, and
// only the process that is about to become one creates it locally.
//
// The task itself is never sent here. The window sends it, once, when the
// session reaches the hub's list; seeding it from this side would run the turn
// behind a window that then had to work out what it had missed.
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
	// Nothing was serving, so this process will be the daemon. Hand the lock
	// straight back before runServe takes it again — holding it here would make
	// this process refuse itself.
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

// openWindow is what a launch already knows about the window it is opening: the
// session it belongs to and the first message to send there. It travels as a
// parameter rather than a serve flag because no operator should ever type it —
// it is one launch path telling another what it is for.
type openWindow struct {
	session string
	text    string
}

// foregroundOpener is the foreground gateway's optional ability to be pointed at
// a session before it starts. Declared here, structurally, so the composition
// root can use it without importing the frontend that implements it — the same
// way it reads contracts.Foreground off the unwrapped gateway.
type foregroundOpener interface {
	OpenOn(session, text string)
}

// runPromptWith is the sequencing itself: create, then seed.
//
// The session name goes to stderr and the reply to stdout, so `herrscher "…" >
// out.md` holds the answer and nothing else.
//
// A failed seed deliberately leaves the session standing. It owns a worktree and
// the beginning of a transcript, and removing that is removing what the operator
// needs to see why the turn failed; the error names the session so they can
// resume or close it themselves.
func runPromptWith(ctx context.Context, d verbDispatcher, name, prompt string, stdout, stderr io.Writer) error {
	if _, err := d.Dispatch(ctx, createTaskSession(name)); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "session: "+name)
	argv := append([]string{"session", "seed", "--name", name}, cli.FlagArg("task", prompt)...)
	argv = append(argv, "--timeout", promptTimeout.String())
	reply, err := d.Dispatch(ctx, argv)
	if err != nil {
		return fmt.Errorf("session %s: %w", name, err)
	}
	if reply != "" {
		fmt.Fprintln(stdout, reply)
	}
	return nil
}

// forwardTimeout bounds one forwarded command. Generous enough that no honest
// verb reaches it — the daemon answers these from memory or from one API call —
// and short enough that a stuck one gives the terminal back.
const forwardTimeout = 60 * time.Second

// forwardUnknownVerb relays an argv this binary has no case for to the running
// daemon. The agent does not speak to the daemon over the socket — it shells out
// to this binary — while the verbs gateway plugins contribute exist only in the
// daemon's registry, built from live gateway instances. Forwarding is what joins
// the two; this process stays ignorant of what it relayed.
//
// It resolves the instance the same way runRegistryVerb does, so both find the
// same daemon. A config.json that will not load is not fatal here: it only costs
// the fallback id, and the dial then misses like any other absent daemon.
//
// The wait is bounded. A forwarded verb can reach a plugin that calls a remote
// API, and the daemon's own read timeout covers only the request, never the
// dispatch — so without a deadline here a rate-limited gateway leaves the
// operator's terminal hanging with nothing to read and nothing to cancel. The
// deadline is what turns that into an error a caller can act on.
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

// buildGateways returns every gateway set the hub could build. BuildHub already
// builds them all — the old buildGateway threw away everything but the first —
// so this costs nothing extra and lets the operator registry answer the same
// questions the daemon can (which admin owns the home, which one is the
// terminal). Gateways whose config is absent are simply not in the result.
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

// scanFlag returns the value of --name / -name (space- or =-separated) from a
// raw arg slice without consuming a FlagSet, so config.json can be read before
// Parse to seed other flags' defaults. Returns def when the flag is absent.
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
