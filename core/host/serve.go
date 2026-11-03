package host

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/health"
	"github.com/Herrscherd/herrscher/core/internal/instanceid"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
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
	DefaultCmd    string
	HealthAddr    string
	StatusChannel string

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

func resolveInstanceID(st *state.State, optID, ownerID string, log *slog.Logger) (string, error) {
	resolved, err := instanceid.Resolve(optID, ownerID)
	if err != nil {
		return "", err
	}
	if st.InstanceID != "" {
		if resolved != "" && resolved != st.InstanceID {
			return "", &instanceIDMismatchError{Have: st.InstanceID, Want: resolved}
		}
		return st.InstanceID, nil
	}
	if resolved == "" {
		return "", nil
	}
	if len(st.SnapshotSessions()) > 0 {
		log.Warn("legacy sessions present; staying in non-namespaced mode",
			"sessions", len(st.SnapshotSessions()))
		return "", nil
	}
	if err := st.SetInstanceID(resolved); err != nil {
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
func RunHub(ctx context.Context, gws []Deps, o Options) error {
	base := obs.Stderr(false)
	log := base.With("component", "serve")
	h := health.NewHealth(time.Now())

	st, err := state.LoadState(o.StatePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	var home *state.HomeRef
	if o.Home != nil {
		home = &state.HomeRef{ID: o.Home.ID, Type: o.Home.Type}
	}
	st.ApplyDefaults(home, o.Workspace, o.Source)
	seedTerminalHome(st, o.ForegroundBound)

	self, execErr := os.Executable()
	logCleanup(execErr)
	startRemotePluginHosts(ctx, self, o.RemoteCategories, base)
	partDir := filepath.Dir(o.StatePath)
	sup := supervisor.NewSupervisor(ctx, self)
	sup.SetLogger(base)
	sup.SetMetrics(h.Metrics())
	sup.SetAgentsRoot(filepath.Join(partDir, "agents"))

	instID, err := resolveInstanceID(st, o.InstanceID, o.Owner, log)
	if err != nil {
		return fmt.Errorf("resolve instance id: %w", err)
	}

	if o.DefaultGateways == nil {
		o.DefaultGateways = nonTerminalKinds(gws)
	}
	reg, deps, err := buildRegistry(ctx, Deps{Admin: adminForHome(gws, st.Home)}, o, st, sup, instID, o.ContributedCommands)
	if err != nil {
		return fmt.Errorf("build command registry: %w", err)
	}
	bridgeEnv := GatewayEnvPairs()
	kinds := contributedKinds(o.ContributedCommands)
	if pair := CapabilityEnvPair(reg.Specs(), func(family string) bool { return kinds[family] }); pair != "" {
		bridgeEnv = append(bridgeEnv, pair)
	}
	sup.SetBridgeEnv(bridgeEnv)
	ensureCodexAgent(deps.agents)
	if ta := terminalAdmin(gws); ta != nil {
		deps.handler.SetTerminalAdmin(ta)
	}
	hb := newHub(st, sup, gws, partDir, reg, h.Metrics())
	go func() {
		<-ctx.Done()
		hb.stopLive()
	}()
	hb.contributedKinds = kinds
	hb.instanceID = instID
	hb.gate = deps.handler.BudgetGate()
	coord := newCoordinator(hb, deps.agents, deps.wt, st, hb, Seed)
	hb.coordinator = coord
	deps.seedCoord.coord = coord

	sched := newScheduler(st, st, hb, Seed, time.Now)
	deps.schedSlot.sched = sched
	go sched.Run(ctx)

	deps.handler.SetCoordinationReader(coordViewAdapter{c: coord})

	es := newEventSocket()
	go serveEventsSocket(ctx, EventsSocketPath(instID), es)
	hb.eventPublisher = es.Publish

	go serveCommandSocket(ctx, CommandSocketPath(instID), asOperator{hb})

	for _, sess := range st.SnapshotSessions() {
		if sess.Archived {
			continue
		}
		hb.goLive(sess)
		sup.Start(sess)
	}
	h.SetSessions(len(st.SnapshotSessions()))

	for _, g := range gws {
		if rcv, ok := g.Gateway.(contracts.SessionControlReceiver); ok {
			rcv.BindSessionControl(hb)
		}
	}

	if instID != "" {
		log.Info("instance resolved", "instance", instID)
	}

	for _, p := range contracts.Default.Plugins() {
		log.Info("plugin compiled in", "kind", p.Manifest.Kind, "category", p.Manifest.Category, "status", p.Manifest.Status)
	}

	if o.HealthAddr != "" {
		go serveHealth(ctx, o.HealthAddr, h)
	}
	if pr := firstProber(gws); pr != nil {
		go pingLoop(ctx, pr, h)
	}
	if o.StatusChannel != "" {
		if cr := firstReader(gws); cr != nil {
			go statusLoop(ctx, cr, o.StatusChannel, st, h, instID)
		}
	}

	log.Info("hub up; supervising sessions", "gateways", strings.Join(kindsOf(gws), ","))
	<-ctx.Done()
	return ctx.Err()
}

func seedTerminalHome(st *state.State, hasForeground bool) {
	if !hasForeground || st.Home.ID != "" {
		return
	}
	logCleanup(st.SetHome(state.HomeRef{ID: "terminal", Type: "terminal"}))
}

func startRemotePluginHosts(ctx context.Context, self string, remote map[contracts.Category]bool, log *slog.Logger) {
	base := log.With("component", "plugin-host")
	for c := range remote {
		if !SupportedRemoteCategory(c) {
			base.Warn("remote category not yet supported; staying in-process", "category", c)
			continue
		}
		startRemotePluginHost(ctx, self, c, base.With("category", string(c)))
	}
}

func startRemotePluginHost(ctx context.Context, self string, cat contracts.Category, log *slog.Logger) {
	go func() {
		bo := obs.RestartBackoff()
		for ctx.Err() == nil {
			cmd := exec.CommandContext(ctx, self, "plugin-host", "--category", string(cat), "--instance", string(cat)+"-0")
			if url := os.Getenv("HERRSCHER_NATS"); url != "" {
				cmd.Args = append(cmd.Args, "--nats", url)
			}
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			cmd.Env = os.Environ()
			start := time.Now()
			logCleanup(cmd.Run())
			if ctx.Err() != nil {
				return
			}
			delay := bo.Next(time.Since(start))
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
	for _, g := range gws {
		if g.Prober != nil {
			return g.Prober
		}
	}
	return nil
}

func firstAdmin(gws []Deps) contracts.ChannelAdmin {
	for _, g := range gws {
		if g.Admin != nil {
			return g.Admin
		}
	}
	return nil
}

func adminForHome(gws []Deps, home state.HomeRef) contracts.ChannelAdmin {
	wantTerminal := home.Type == "terminal"
	for _, g := range gws {
		if g.Admin == nil || g.Gateway == nil {
			continue
		}
		if (g.Gateway.Manifest().Kind == "terminal") == wantTerminal {
			return g.Admin
		}
	}
	return firstAdmin(gws)
}

func terminalAdmin(gws []Deps) contracts.ChannelAdmin {
	for _, g := range gws {
		if g.Admin == nil || g.Gateway == nil {
			continue
		}
		if g.Gateway.Manifest().Kind == "terminal" {
			return g.Admin
		}
	}
	return nil
}

func firstReader(gws []Deps) contracts.ChannelReader {
	for _, g := range gws {
		if g.Reader != nil {
			return g.Reader
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
	for _, g := range all {
		if g.Gateway != nil {
			out = append(out, g.Gateway.Manifest().Kind)
		}
	}
	return out
}

func nonTerminalKinds(all []Deps) []string {
	var out []string
	for _, g := range all {
		if g.Gateway == nil {
			continue
		}
		if g.Gateway.Manifest().Kind != "terminal" {
			out = append(out, g.Gateway.Manifest().Kind)
		}
	}
	return out
}

func boundGateways(all []Deps, kinds []string) []Deps {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var out []Deps
	for _, g := range all {
		if g.Gateway != nil && want[g.Gateway.Manifest().Kind] {
			out = append(out, g)
		}
	}
	return out
}
