package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const seedTurnTimeout = 120 * time.Second

// oneShotBackendFactory is a test seam for the one-shot bridge. The production
// factory resolves a remote backend when configured and otherwise builds the
// registered local backend with the session's backend settings.
var oneShotBackendFactory = newSeedBackend

// seedEventPublisher, when set at daemon boot (RunHub wires it to the events
// socket), taps every seed-turn event onto that socket keyed by session. nil in
// the operator CLI path and in tests — the seed then runs with no external
// event stream, exactly as before. It mirrors the oneShotBackendFactory seam:
// one process-wide daemon owns the socket, so a package var is sufficient.
var seedEventPublisher func(session string, e contracts.Event)

type seedCommandForwarder func(context.Context, string, []string) (string, bool, error)

// runOneShotSeedCommand sends an operator-process seed to the running daemon
// when one is available, because that process owns the live Coordinator. A
// daemon-side invocation already has coord and runs locally. With neither a
// daemon nor a coordinator it falls back to the historical uncoordinated
// one-shot behavior.
func runOneShotSeedCommand(ctx context.Context, st *state.State, name, task string, coord contracts.Coordinator, instID string, forward seedCommandForwarder) (string, error) {
	if coord == nil && forward != nil {
		if reply, handled, err := forward(ctx, CommandSocketPath(instID), []string{
			"session", "seed", "--name", name, "--task", task,
		}); handled {
			return reply, err
		}
	}
	return runOneShotSeed(ctx, st, name, task, coord)
}

// runOneShotSeed builds the session-scoped orchestrator and delegates to the
// testable one-shot runner. Resolver.Orchestrator supplies a remote proxy when
// requested; otherwise the local plugin receives the session name and the
// persisted extractor/journal/cadence config in its PluginConfig.
func runOneShotSeed(ctx context.Context, st *state.State, name, task string, coord contracts.Coordinator) (string, error) {
	sess, ok := st.FindSession(name)
	if !ok {
		return "", fmt.Errorf("no session %q", name)
	}
	orch, mem, err := seedOrchestrator(ctx, sess)
	if err != nil {
		return "", err
	}
	if mem != nil {
		defer mem.Close()
	}
	return runOneShotSeedWith(ctx, sess, task, orch, coord)
}

// runOneShotSeedWith mounts the same in-process bridge turn used by the daemon:
// newSessionDriver owns the FIFO and SeedAndWait awaits reply{done}; bridge.RunOneShot
// supplies the registered backend over channels. Unlike RunSession/goLive this
// deliberately has no control socket, supervisor, or gateway binding.
func runOneShotSeedWith(ctx context.Context, sess state.Session, task string, orch contracts.Orchestrator, coord contracts.Coordinator) (string, error) {
	if orch != nil {
		defer orch.Close()
	}

	seedCtx, cancel := context.WithTimeout(ctx, seedTurnTimeout)
	defer cancel()
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d := newSessionDriver(sess.Name, nil, toBridge, fromBridge)
	// Tap the seed turn onto the daemon's events socket (when one is serving) so
	// the app sees live thinking/status/chunk/reply. The seed path binds no
	// gateways, so this tap is the only way its events escape the process.
	if seedEventPublisher != nil {
		name := sess.Name
		d.emitTap = func(e contracts.Event) { seedEventPublisher(name, e) }
	}
	pumpCtx, pumpCancel := context.WithCancel(seedCtx)
	defer pumpCancel()
	pumpDone := make(chan struct{})
	go func() { defer close(pumpDone); d.pump(pumpCtx) }()

	var bridgeErr = make(chan error, 1)
	go func() {
		err := bridge.RunOneShot(seedCtx, func(channel string) (contracts.Backend, error) {
			return oneShotBackendFactory(seedCtx, sess)
		}, orch, sess.ChannelID, toBridge, fromBridge)
		bridgeErr <- err
		if err != nil {
			cancel()
		}
	}()

	reply, ok := d.SeedAndWait(seedCtx, task)
	if !ok {
		select {
		case err := <-bridgeErr:
			if err != nil {
				return "", err
			}
		default:
		}
		return "", fmt.Errorf("seed timeout")
	}
	if err := <-bridgeErr; err != nil {
		return "", err
	}
	if coord != nil {
		// The seed turn ran with a nil coordinator so the pump never re-enters
		// the dispatch mutex mid-turn; coordination happens here instead. Stop
		// the pump and join it before writing d.coordinator, so the field is
		// never read (turnloop) and written (here) from two goroutines at once.
		pumpCancel()
		<-pumpDone
		d.coordinator = coord
		d.maybeCoordinate(seedCtx, reply)
	}
	if orch != nil {
		if err := orch.Consolidate(seedCtx); err != nil {
			return "", fmt.Errorf("consolidate: %w", err)
		}
	}
	return reply, nil
}

// ApplyOrchestratorScope threads a session's runtime scope into an orchestrator
// plugin's config bag. It is the single source of truth for these Settings keys,
// shared by the live bridge (bridge.go) and the one-shot seed so the two paths
// cannot drift when a scope key is added or renamed. Empty optional values are
// omitted so a plain/unconfigured run's config stays byte-for-byte unchanged.
func ApplyOrchestratorScope(cfg *contracts.PluginConfig, session, project, agent, extractor, journal string, consolidateEvery int) {
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	cfg.Settings["session"] = session
	if project != "" {
		cfg.Settings["memory.project"] = project
	}
	if agent != "" {
		cfg.Settings["memory.agent"] = agent
	}
	if extractor != "" {
		cfg.Settings["memory.extractor"] = extractor
	}
	if journal != "" {
		cfg.Settings["memory.journal"] = journal
	}
	if consolidateEvery > 0 {
		cfg.Settings["memory.consolidate-every"] = strconv.Itoa(consolidateEvery)
	}
}

// BuildBackend selects and constructs a backend by vendor. A remote resolver
// backend wins when configured; otherwise the matching registered plugin is
// built with the invocation, backend kind, and working directory settings.
func BuildBackend(ctx context.Context, vendor, cmd, kind, dir, resume string) (contracts.Backend, error) {
	desired := vendor
	if desired == "" {
		desired = os.Getenv("HERRSCHER_BACKEND")
	}
	plugins := contracts.Default.Backends()
	resolver := NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
	if backend, err := resolver.Backend(ctx, plugins, desired); err != nil {
		return nil, err
	} else if backend != nil {
		return backend, nil
	}
	plugin, err := selectBackend(desired, plugins)
	if err != nil {
		return nil, err
	}
	cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
	if err != nil {
		return nil, err
	}
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	if cmd != "" {
		cfg.Settings["cmd"] = cmd
	}
	if kind != "" {
		cfg.Settings["kind"] = kind
	}
	if dir != "" {
		cfg.Settings["dir"] = dir
	}
	if resume != "" {
		cfg.Settings["resume"] = resume
	}
	return plugin.Backend(ctx, cfg)
}

func newSeedBackend(ctx context.Context, sess state.Session) (contracts.Backend, error) {
	dir := sess.Dir
	if dir == "" {
		dir = sess.Worktree
	}
	return BuildBackend(ctx, sess.Vendor, sess.Cmd, sess.Backend, resolveBackendDir(dir), sess.ResumeToken)
}

// resolveBackendDir upgrades persisted relative session directories at the
// backend boundary. A supervised bridge may already be running inside that
// relative directory, so detect that suffix before joining it to cwd; otherwise
// the path would be applied twice (…/worker/…/worker).
func resolveBackendDir(dir string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	if filepath.IsAbs(dir) {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return dir
	}
	cwd = filepath.Clean(cwd)
	suffix := string(os.PathSeparator) + dir
	if (os.PathSeparator == '\\' && strings.HasSuffix(strings.ToLower(cwd), strings.ToLower(suffix))) ||
		(os.PathSeparator != '\\' && strings.HasSuffix(cwd, suffix)) {
		return cwd
	}
	return filepath.Join(cwd, dir)
}

func selectBackend(desired string, plugins []contracts.Plugin) (contracts.Plugin, error) {
	for _, plugin := range plugins {
		if plugin.Backend == nil {
			continue
		}
		if desired == "" || plugin.Manifest.Kind == desired {
			return plugin, nil
		}
	}
	if desired != "" {
		return contracts.Plugin{}, fmt.Errorf("unknown backend %q", desired)
	}
	return contracts.Plugin{}, fmt.Errorf("no backend plugin registered")
}

// provisionSeedScope ensures the memory scope roots exist before a one-shot seed
// turn, keyed with the same contracts helpers the orchestrator derives its scope
// from so the keys cannot drift. It is the seed-path counterpart of the live
// bridge's provisionScope: best-effort (memory stays optional) and skipped for
// memories that cannot create nodes.
func provisionSeedScope(ctx context.Context, mem contracts.Memory, project, agent string) {
	p, ok := mem.(contracts.Provisioner)
	if !ok {
		return
	}
	if project != "" {
		_ = p.EnsureProject(ctx, contracts.ProjectKey(project), project)
	}
	if agent != "" {
		_ = p.EnsureAgent(ctx, contracts.AgentKey(agent), agent)
	}
}

func seedOrchestrator(ctx context.Context, sess state.Session) (contracts.Orchestrator, contracts.Memory, error) {
	resolver := NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
	mem, err := resolver.Memory(ctx, contracts.Default.Memories(), os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	// Ensure the scope roots exist before the turn, mirroring the live bridge's
	// provisionScope. Without this a one-shot seed against a fresh vault fails at
	// the first Consolidate: RecordShared/RecordPrivate link candidates under the
	// project/agent roots, and the obsidian vault errors when those parent notes
	// are absent. Best-effort and plugin-agnostic — a memory that cannot create
	// nodes simply does not satisfy Provisioner and is skipped.
	provisionSeedScope(ctx, mem, sess.Project, sess.Agent)
	orch, err := resolver.Orchestrator(ctx, contracts.Default.Orchestrators())
	if err != nil {
		if mem != nil {
			_ = mem.Close()
		}
		return nil, nil, err
	}
	if orch != nil {
		return orch, mem, nil
	}
	for _, plugin := range contracts.Default.Orchestrators() {
		if plugin.Orchestrator == nil {
			continue
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
		if err != nil {
			if mem != nil {
				_ = mem.Close()
			}
			return nil, nil, err
		}
		ApplyOrchestratorScope(&cfg, sess.Name, sess.Project, sess.Agent, sess.Extractor, sess.Journal, sess.ConsolidateEvery)
		orch, err := plugin.Orchestrator(ctx, cfg, mem)
		if err != nil {
			if mem != nil {
				_ = mem.Close()
			}
			return nil, nil, err
		}
		return orch, mem, nil
	}
	return nil, mem, nil
}
