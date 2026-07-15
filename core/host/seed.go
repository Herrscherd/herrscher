package host

import (
	"context"
	"fmt"
	"os"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const seedTurnTimeout = 120 * time.Second

// runOneShotSeed mounts the same in-process bridge turn used by the daemon:
// newSessionDriver owns the FIFO and SeedAndWait awaits reply{done}; bridge.RunOneShot
// supplies the registered backend over channels. Unlike RunSession/goLive this
// deliberately has no control socket, supervisor, or gateway binding.
func runOneShotSeed(ctx context.Context, st *state.State, name, task string) (string, error) {
	sess, ok := st.FindSession(name)
	if !ok {
		return "", fmt.Errorf("no session %q", name)
	}

	seedCtx, cancel := context.WithTimeout(ctx, seedTurnTimeout)
	defer cancel()
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d := newSessionDriver(name, nil, toBridge, fromBridge)
	go d.pump(seedCtx)

	newBackend := func(channel string) (contracts.Backend, error) {
		resolver := NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
		if backend, err := resolver.Backend(seedCtx, contracts.Default.Backends()); err != nil {
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
			return plugin.Backend(seedCtx, cfg)
		}
		return nil, fmt.Errorf("no backend plugin registered")
	}

	var bridgeErr = make(chan error, 1)
	go func() {
		err := bridge.RunOneShot(seedCtx, newBackend, nil, sess.ChannelID, toBridge, fromBridge)
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
	return reply, nil
}
