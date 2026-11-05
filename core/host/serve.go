package host

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/health"
	"github.com/Herrscherd/herrscher/core/internal/instanceid"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
	"github.com/Herrscherd/herrscher/core/keyproxy"
	"github.com/Herrscherd/herrscher/core/service"
)

type serviceUpdater struct {
	cfg service.Config
	st  *state.State
}

const buildTimeout = 5 * time.Minute

func (u serviceUpdater) Build(ctx context.Context, pull bool) (string, error) {
	src := u.st.SourceDir()
	if src == "" {
		return "", errSourceNotSet
	}
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	if pull {
		if err := service.Pull(ctx, src); err != nil {
			return "", err
		}
	}
	if err := service.Build(ctx, src, u.cfg.BinPath); err != nil {
		return "", err
	}
	if err := service.Smoke(ctx, u.cfg.BinPath); err != nil {
		return "", err
	}
	return service.SourceVersion(ctx, src), nil
}

func (u serviceUpdater) Restart(ctx context.Context) error {
	return service.RestartDetached(ctx, u.cfg)
}

// Deps is the channel the daemon drives. It is the neutral contracts.GatewaySet
// the registry produces, so serve carries no platform-specific imports and works
// with any gateway plugin.
type Deps = contracts.GatewaySet

// Options holds the parsed flags for the serve daemon.
type Options struct {
	StatePath     string
	Language      i18n.Language
	Models        *ModelDiscovery
	DefaultCmd    string
	HealthAddr    string
	StatusChannel string

	CompactPercent *int
	CompactStack   []string
	ContextLimits  map[string]int

	// InstanceID is the explicit per-daemon namespace (-instance flag /
	// HERRSCHER_INSTANCE_ID). Empty falls back to HERRSCHER_OWNER_ID, then legacy
	// mode.
	InstanceID string

	// Declarative config.json defaults. Owner is the per-daemon instance-id
	// fallback (env HERRSCHER_OWNER_ID takes precedence, resolved by the caller).
	// Home, Workspace and Source seed state in-memory only if unset, so a live
	// /set always wins (see state.ApplyDefaults).
	Owner     string
	Home      *HomeRef
	Workspace string
	Source    string

	// RemoteCategories lists plugin categories served out-of-process; RunHub
	// spawns and supervises a plugin-host child per entry. Empty => all in-proc.
	RemoteCategories map[contracts.Category]bool

	// ForegroundBound is true when a foreground (TUI) gateway is bound to the
	// process. Set by the caller (serve.go runServe) where fg != nil is computed.
	ForegroundBound bool

	// DefaultGateways is the primary gateway set a new session binds to when it
	// names none. The caller derives it from the built gateways (the concrete
	// platform kinds), so the manager package never names a gateway itself.
	DefaultGateways []string

	// ContributedCommands are the plugin verbs to graft onto the daemon's
	// registry, already namespaced by CollectCommands. The caller collects them
	// because only the composition root still holds the unwrapped gateways; by
	// the time a set reaches here it has been decorated and no longer answers to
	// CommandSource.
	ContributedCommands []contracts.Cmd
}

// HomeRef is the seed home channel from config.json: a channel id and its kind
// ("category" | "forum"). Kept platform-neutral so the caller need not import the
// internal state package.
type HomeRef struct {
	ID   string
	Type string
}

// DefaultStatePath returns the default path to the daemon state file.
func DefaultStatePath() string {
	if d := envx.Get("STATE_DIR"); d != "" {
		return filepath.Join(d, "state.json")
	}
	home, homeErr := os.UserHomeDir()
	logCleanup(homeErr)
	return filepath.Join(home, ".config", "herrscher", "state.json")
}

func resolveInstanceID(hostState *state.State, optID, ownerID string, log *slog.Logger) (string, error) {
	resolved, err := instanceid.Resolve(optID, ownerID)
	if err != nil {
		return "", err
	}
	if hostState.InstanceID != "" {
		if resolved != "" && resolved != hostState.InstanceID {
			return "", &instanceIDMismatchError{Have: hostState.InstanceID, Want: resolved}
		}
		return hostState.InstanceID, nil
	}
	if resolved == "" {
		return "", nil
	}
	if len(hostState.SnapshotSessions()) > 0 {
		log.Warn("legacy sessions present; staying in non-namespaced mode",
			"sessions", len(hostState.SnapshotSessions()))
		return "", nil
	}
	if err := hostState.SetInstanceID(resolved); err != nil {
		return "", fmt.Errorf("persist instanceID: %w", err)
	}
	return resolved, nil
}

// RunHub is the always-on multi-gateway daemon: it supervises one pure-runner
// bridge per persisted session, drives each session's turns over a control
// Acceptor (fanning events out to every bound gateway), and serves
// health/liveness. gws are the gateway sets the daemon owns (built from the
// registry by the caller). Command dispatch no longer runs here.
// Session/service commands run through the operator CLI (see NewRegistry).
func RunHub(ctx context.Context, gws []Deps, closers []contracts.Gateway, options Options) error {
	base := obs.Stderr(false)
	defer closeGateways(ctx, closers, base)
	log := base.With("component", "serve")
	monitor := health.NewHealth(time.Now())

	store, err := loadHubState(options)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	self, execErr := os.Executable()
	logCleanup(execErr)
	startRemotePluginHosts(ctx, self, options.RemoteCategories, base)
	partDir := filepath.Dir(options.StatePath)
	sup := newHubSupervisor(ctx, self, base, monitor, partDir, options.StatePath)

	instID, err := resolveInstanceID(store, options.InstanceID, options.Owner, log)
	if err != nil {
		return fmt.Errorf("resolve instance id: %w", err)
	}

	if options.DefaultGateways == nil {
		options.DefaultGateways = nonTerminalKinds(gws)
	}
	approvals := newApprovalTable().withRules(store)
	sessions := newSessionTable()
	policy, err := newCompactionPolicy(ctx, options.CompactPercent, options.CompactStack, options.ContextLimits)
	if err != nil {
		return fmt.Errorf("compaction: %w", err)
	}
	sessions.compaction = policy
	sessions.store = store
	reg, deps, err := buildRegistry(ctx, Deps{Admin: adminForHome(gws, store.Home)}, options, store, sup, instID, options.ContributedCommands, approvals, sessions)
	if err != nil {
		return fmt.Errorf("build command registry: %w", err)
	}
	kinds := contributedKinds(options.ContributedCommands)
	sup.SetBridgeEnv(hubBridgeEnv(reg.Specs(), kinds))
	ensureCodexAgent(deps.agents)
	if ta := terminalAdmin(gws); ta != nil {
		deps.handler.SetTerminalAdmin(ta)
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	keys, err := keyproxy.Open(ctx, cfg.Keys, keyproxy.OSLoader(), log)
	if err != nil && !errors.Is(err, keyproxy.ErrDisabled) {
		return fmt.Errorf("key proxy: %w", err)
	}
	defer CloseKeyProxy(ctx, log, keys)
	runningHub := newHub(store, sup, gws, partDir, reg, monitor.Metrics(), approvals, sessions)
	runningHub.lang = options.Language
	runningHub.models = options.modelDiscovery()
	runningHub.seedBackend = func(seedCtx context.Context, sess state.Session) (contracts.Backend, error) {
		return newSeedBackend(seedCtx, sess, keys, runningHub.models)
	}
	liveStopped := make(chan struct{})
	go func() {
		defer close(liveStopped)
		<-ctx.Done()
		runningHub.stopLive()
	}()
	runningHub.contributedKinds = kinds
	runningHub.instanceID = instID
	runningHub.gate = deps.handler.BudgetGate()
	coord := wireCoordination(runningHub, deps, store, sessions)

	sched := newScheduler(store, store, runningHub, sessions.Seed, time.Now)
	deps.schedSlot.sched = sched
	go sched.Run(ctx)

	deps.handler.SetCoordinationReader(coordViewAdapter{c: coord})

	serveHubSockets(ctx, instID, runningHub)
	goLiveSessions(ctx, runningHub, sup, store, monitor)
	bindSessionControl(gws, runningHub)

	if instID != "" {
		log.Info("instance resolved", "instance", instID)
	}
	logCompiledPlugins(log)
	startHubProbes(ctx, gws, options, store, monitor, instID)

	log.Info("hub up; supervising sessions", "gateways", strings.Join(kindsOf(gws), ","))
	<-ctx.Done()
	<-liveStopped
	return ctx.Err()
}

// CloseKeyProxy closes the key proxy when keys is set and logs a failed close.
func CloseKeyProxy(ctx context.Context, log *slog.Logger, keys *keyproxy.Proxy) {
	if keys == nil {
		return
	}
	if err := keys.Close(ctx); err != nil {
		log.Warn("key proxy did not close cleanly", "err", err)
	}
}

func loadHubState(opts Options) (*state.State, error) {
	store, err := state.LoadState(opts.StatePath)
	if err != nil {
		return nil, err
	}
	var home *state.HomeRef
	if opts.Home != nil {
		home = &state.HomeRef{ID: opts.Home.ID, Type: opts.Home.Type}
	}
	store.ApplyDefaults(home, opts.Workspace, opts.Source)
	seedTerminalHome(store, opts.ForegroundBound)
	return store, nil
}

func newHubSupervisor(ctx context.Context, self string, base *slog.Logger, host *health.Health, partDir, statePath string) *supervisor.Supervisor {
	sup := supervisor.NewSupervisor(ctx, self)
	sup.SetStatePath(statePath)
	sup.SetLogger(base)
	sup.SetMetrics(host.Metrics())
	sup.SetAgentsRoot(filepath.Join(partDir, "agents"))
	return sup
}

func hubBridgeEnv(specs []cli.Spec, kinds map[string]bool) []string {
	bridgeEnv := append(GatewayEnvPairs(), AnnounceKeyEnvPairs()...)
	if pair := CapabilityEnvPair(specs, func(family string) bool { return kinds[family] }); pair != "" {
		bridgeEnv = append(bridgeEnv, pair)
	}
	return bridgeEnv
}

func wireCoordination(hubCore *hub, deps hostDeps, st *state.State, sessions *sessionTable) *coordinator {
	coord := newCoordinator(hubCore, deps.agents, deps.wt, st, hubCore, sessions.Seed)
	hubCore.coordinator = coord
	deps.seedCoord.coord = coord
	return coord
}

func serveHubSockets(ctx context.Context, instID string, hubCore *hub) {
	events := newEventSocket()
	serveEventsSocket(ctx, EventsSocketPath(instID), events)
	hubCore.eventPublisher = events.Publish
	serveCommandSocket(ctx, CommandSocketPath(instID), asOperator{disp: hubCore, lang: hubCore.lang})
}

func goLiveSessions(ctx context.Context, runningHub *hub, sup *supervisor.Supervisor, hostState *state.State, host *health.Health) {
	sessions := hostState.SnapshotSessions()
	for idx := range sessions {
		sess := sessions[idx]
		if sess.Archived {
			continue
		}
		runningHub.goLive(ctx, sess)
		sup.StartWith(ctx, sess)
	}
	host.SetSessions(len(hostState.SnapshotSessions()))
}

func bindSessionControl(gws []Deps, hubCore *hub) {
	for _, g := range gws {
		if rcv, ok := g.Gateway.(contracts.SessionControlReceiver); ok {
			rcv.BindSessionControl(hubCore)
		}
	}
}

func logCompiledPlugins(log *slog.Logger) {
	plugins := contracts.Default.Plugins()
	for idx := range plugins {
		p := plugins[idx]
		log.Info("plugin compiled in", "kind", p.Manifest.Kind, "category", p.Manifest.Category, keyStatus, p.Manifest.Status)
	}
}

func startHubProbes(ctx context.Context, gws []Deps, options Options, store *state.State, monitor *health.Health, instID string) {
	if options.HealthAddr != "" {
		go serveHealth(ctx, options.HealthAddr, monitor)
	}
	if pr := firstProber(gws); pr != nil {
		go pingLoop(ctx, pr, monitor)
	}
	if options.StatusChannel != "" {
		if cr := firstReader(gws); cr != nil {
			go statusLoop(ctx, cr, options.StatusChannel, store, monitor, instID)
		}
	}
}

func seedTerminalHome(hostState *state.State, hasForeground bool) {
	if !hasForeground || hostState.Home.ID != "" {
		return
	}
	logCleanup(hostState.SetHome(state.HomeRef{ID: homeTerminalID, Type: kindTerminal}))
}

func startRemotePluginHosts(ctx context.Context, self string, remote map[contracts.Category]bool, log *slog.Logger) {
	base := log.With("component", "plugin-host")
	for category := range remote {
		if !SupportedRemoteCategory(category) {
			base.Warn("remote category not yet supported; staying in-process", "category", category)
			continue
		}
		startRemotePluginHost(ctx, self, category, base.With("category", string(category)))
	}
}

func startRemotePluginHost(ctx context.Context, self string, cat contracts.Category, log *slog.Logger) {
	go func() {
		backoff := obs.RestartBackoff()
		for ctx.Err() == nil {
			cmd := exec.CommandContext(ctx, self, "plugin-host", "--category", string(cat), "--instance", string(cat)+"-0")
			if url := os.Getenv("HERRSCHER_NATS"); url != "" {
				cmd.Args = append(cmd.Args, "--nats", url)
			}
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			cmd.Env = append(os.Environ(), AnnounceKeyEnvPairs()...)
			start := time.Now()
			logCleanup(cmd.Run())
			if ctx.Err() != nil {
				return
			}
			delay := backoff.Next(time.Since(start))
			log.Warn("plugin-host exited, restarting", "delay", delay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}()
}

func firstProber(gws []Deps) contracts.Prober {
	for _, dep := range gws {
		if dep.Prober != nil {
			return dep.Prober
		}
	}
	return nil
}

func firstAdmin(gws []Deps) contracts.ChannelAdmin {
	for _, dep := range gws {
		if dep.Admin != nil {
			return dep.Admin
		}
	}
	return nil
}

func adminForHome(gws []Deps, home state.HomeRef) contracts.ChannelAdmin {
	wantTerminal := home.Type == kindTerminal
	for _, dep := range gws {
		if dep.Admin == nil || dep.Gateway == nil {
			continue
		}
		if (dep.Gateway.Manifest().Kind == kindTerminal) == wantTerminal {
			return dep.Admin
		}
	}
	return firstAdmin(gws)
}

func terminalAdmin(gws []Deps) contracts.ChannelAdmin {
	for _, dep := range gws {
		if dep.Admin == nil || dep.Gateway == nil {
			continue
		}
		if dep.Gateway.Manifest().Kind == kindTerminal {
			return dep.Admin
		}
	}
	return nil
}

func firstReader(gws []Deps) contracts.ChannelReader {
	for _, dep := range gws {
		if dep.Reader != nil {
			return dep.Reader
		}
	}
	return nil
}

func effectiveKinds(all []Deps, sess state.Session) []string {
	if kinds := sess.BoundGateways(); len(kinds) > 0 {
		return kinds
	}
	if sess.IsLegacy() {
		return nonTerminalKinds(all)
	}
	return nil
}

func kindsOf(all []Deps) []string {
	out := make([]string, 0, len(all))
	for _, dep := range all {
		if dep.Gateway != nil {
			out = append(out, dep.Gateway.Manifest().Kind)
		}
	}
	return out
}

func nonTerminalKinds(all []Deps) []string {
	var out []string
	for _, dep := range all {
		if dep.Gateway == nil {
			continue
		}
		if dep.Gateway.Manifest().Kind != kindTerminal {
			out = append(out, dep.Gateway.Manifest().Kind)
		}
	}
	return out
}

func boundGateways(all []Deps, kinds []string) []Deps {
	want := make(map[string]bool)
	for _, k := range kinds {
		want[k] = true
	}
	var out []Deps
	for _, dep := range all {
		if dep.Gateway != nil && want[dep.Gateway.Manifest().Kind] {
			out = append(out, dep)
		}
	}
	return out
}
