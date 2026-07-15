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
	return runOneShotSeedWith(ctx, st, name, task, orch)
}

// runOneShotSeedWith mounts the same in-process bridge turn used by the daemon:
// newSessionDriver owns the FIFO and SeedAndWait awaits reply{done}; bridge.RunOneShot
// supplies the registered backend over channels. Unlike RunSession/goLive this
// deliberately has no control socket, supervisor, or gateway binding.
func runOneShotSeedWith(ctx context.Context, st *state.State, name, task string, orch contracts.Orchestrator) (string, error) {
	sess, ok := st.FindSession(name)
	if !ok {
		return "", fmt.Errorf("no session %q", name)
	}
	if orch != nil {
		defer orch.Close()
	}

	seedCtx, cancel := context.WithTimeout(ctx, seedTurnTimeout)
	defer cancel()
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d := newSessionDriver(name, nil, toBridge, fromBridge)
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

func newSeedBackend(ctx context.Context, sess state.Session) (contracts.Backend, error) {
	resolver := NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
	if backend, err := resolver.Backend(ctx, contracts.Default.Backends()); err != nil {
		return nil, err
	} else if backend != nil {
		return backend, nil
	}
	for _, plugin := range contracts.Default.Backends() {
		if plugin.Backend == nil {
			continue
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
	return nil, fmt.Errorf("no backend plugin registered")
}

func seedOrchestrator(ctx context.Context, sess state.Session) (contracts.Orchestrator, contracts.Memory, error) {
	resolver := NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
	mem, err := resolver.Memory(ctx, contracts.Default.Memories(), os.Getenv)
	if err != nil {
		return nil, nil, err
	}
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
		if cfg.Settings == nil {
			cfg.Settings = map[string]string{}
		}
		cfg.Settings["session"] = sess.Name
		if sess.Project != "" {
			cfg.Settings["memory.project"] = sess.Project
		}
		if sess.Agent != "" {
			cfg.Settings["memory.agent"] = sess.Agent
		}
		if sess.Extractor != "" {
			cfg.Settings["memory.extractor"] = sess.Extractor
		}
		if sess.Journal != "" {
			cfg.Settings["memory.journal"] = sess.Journal
		}
		if sess.ConsolidateEvery > 0 {
			cfg.Settings["memory.consolidate-every"] = strconv.Itoa(sess.ConsolidateEvery)
		}
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
