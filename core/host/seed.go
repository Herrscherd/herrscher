package host

import (
	"context"
	"fmt"
	"os"
	"strconv"
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

// runOneShotSeed builds the session-scoped orchestrator and delegates to the
// testable one-shot runner. Resolver.Orchestrator supplies a remote proxy when
// requested; otherwise the local plugin receives the session name and the
// persisted extractor/journal/cadence config in its PluginConfig.
func runOneShotSeed(ctx context.Context, st *state.State, name, task string) (string, error) {
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
	return runOneShotSeedWith(ctx, sess, task, orch)
}

// runOneShotSeedWith mounts the same in-process bridge turn used by the daemon:
// newSessionDriver owns the FIFO and SeedAndWait awaits reply{done}; bridge.RunOneShot
// supplies the registered backend over channels. Unlike RunSession/goLive this
// deliberately has no control socket, supervisor, or gateway binding.
func runOneShotSeedWith(ctx context.Context, sess state.Session, task string, orch contracts.Orchestrator) (string, error) {
	if orch != nil {
		defer orch.Close()
	}

	seedCtx, cancel := context.WithTimeout(ctx, seedTurnTimeout)
	defer cancel()
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d := newSessionDriver(sess.Name, nil, toBridge, fromBridge)
	go d.pump(seedCtx)

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

func newSeedBackend(ctx context.Context, sess state.Session) (contracts.Backend, error) {
	desired := sess.Vendor
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
	if sess.Cmd != "" {
		cfg.Settings["cmd"] = sess.Cmd
	}
	if sess.Backend != "" {
		cfg.Settings["kind"] = sess.Backend
	}
	if sess.Worktree != "" {
		cfg.Settings["dir"] = sess.Worktree
	}
	return plugin.Backend(ctx, cfg)
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
