package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
	"github.com/Herrscherd/herrscher/core/service"
)

// hostDeps surfaces pieces buildRegistry constructs internally that a caller may
// need beyond the registry itself — the worktree manager and agent store the
// boot-time Coordinator (Task 7/8) is built from. Kept minimal so the operator
// CLI (NewRegistry), which has no Coordinator to wire, can ignore it.
type hostDeps struct {
	wt      *worktree.Worktreer
	agents  *agent.Store
	handler *manager.Handler
}

// buildRegistry constructs the session/service command handler over a given
// state + supervisor and registers its commands into a fresh CLI registry. The
// daemon (RunHub) and the operator CLI (NewRegistry) share this so a session
// created either way is built from identical deps. d.Admin supplies the channel
// admin port; instID namespaces shared git/gateway resources.
func buildRegistry(ctx context.Context, d Deps, o Options, st *state.State, sup *supervisor.Supervisor, instID string) (*cli.Registry, hostDeps, error) {
	partDir := filepath.Dir(o.StatePath)
	wt := worktree.NewWorktreer(ctx, instID)
	fg := forge.New()
	upCfg, _ := service.DefaultConfig()
	up := serviceUpdater{cfg: upCfg, st: st}
	agents := agent.NewStore(filepath.Join(partDir, "agents"))
	ensureCodexAgent(agents)
	hdl := manager.NewHandler(d.Admin, sup, wt, fg, up, agents, st, o.DefaultCmd, partDir, o.DefaultGateways)

	reg := &cli.Registry{}
	for _, c := range hdl.Commands() {
		if err := reg.Add(c); err != nil {
			return nil, hostDeps{}, err
		}
	}
	if err := reg.Add(contracts.New("session", "seed").
		Help("run one agent turn in a session and print the reply").
		Param("name", "session name", true).
		Param("task", "opening task", true).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			reply, err := runOneShotSeed(cmdCtx, st, in.Get("name"), in.Get("task"))
			if err != nil {
				return "", err
			}
			if in.JSON {
				b, err := json.Marshal(map[string]string{"session": in.Get("name"), "reply": reply})
				return string(b), err
			}
			return reply, nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	// memory locate/forget/record — surface d'exposition mémoire consommée par
	// exec (voir docs/superpowers/specs/2026-07-17-memory-exposure-surface).
	// Chaque commande construit la mémoire in-process et la ferme.
	if err := reg.Add(contracts.New("memory", "locate").
		Help("print openable URIs (obsidian://, file://) of a memory node's note").
		Param("key", "node key", true).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			loc, ok := mem.(contracts.Locator)
			if !ok {
				return "", fmt.Errorf("memory backend does not support locate")
			}
			l, err := loc.Locate(cmdCtx, in.Get("key"))
			if err != nil {
				return "", err
			}
			if in.JSON {
				b, err := json.Marshal(l)
				return string(b), err
			}
			if l.Obsidian != "" {
				return l.Obsidian, nil
			}
			return l.File, nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("memory", "forget").
		Help("delete a memory node by key (idempotent)").
		Param("key", "node key", true).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			del, ok := mem.(contracts.Deleter)
			if !ok {
				return "", fmt.Errorf("memory backend does not support forget")
			}
			if err := del.Delete(cmdCtx, in.Get("key")); err != nil {
				return "", err
			}
			return "forgotten " + in.Get("key"), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("memory", "record").
		Help("upsert a memory node").
		Param("key", "node key", true).
		Param("kind", "node kind (decision, project, …)", true).
		Param("title", "node title", false).
		Param("body", "node body (markdown)", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			kind := contracts.NodeKind(in.Get("kind"))
			switch kind {
			case contracts.KindOrganization, contracts.KindProject, contracts.KindRepo,
				contracts.KindServer, contracts.KindArchitecture, contracts.KindProduction,
				contracts.KindSession, contracts.KindDecision, contracts.KindUser,
				contracts.KindAgent, contracts.KindDomain:
				// ok
			default:
				return "", fmt.Errorf("memory record: unknown kind %q", in.Get("kind"))
			}
			n := contracts.Node{
				Key:   in.Get("key"),
				Kind:  kind,
				Title: in.Get("title"),
				Body:  in.Get("body"),
			}
			if err := mem.Record(cmdCtx, n); err != nil {
				return "", err
			}
			return "recorded " + n.Key, nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	return reg, hostDeps{wt: wt, agents: agents, handler: hdl}, nil
}

// NewRegistry builds the operator CLI registry: it loads its own state +
// supervisor (the operator invocation is a short-lived process) and registers
// the session/service handler's commands. The returned registry dispatches argv
// (see core/cli).
func NewRegistry(ctx context.Context, d Deps, o Options) (*cli.Registry, error) {
	st, err := state.LoadState(o.StatePath)
	if err != nil {
		return nil, err
	}
	var home *state.HomeRef
	if o.Home != nil {
		home = &state.HomeRef{ID: o.Home.ID, Type: o.Home.Type}
	}
	st.ApplyDefaults(home, o.Workspace, o.Source)

	instID, err := resolveInstanceID(st, o.InstanceID, o.Owner, obs.Stderr(false))
	if err != nil {
		return nil, err
	}

	self, _ := os.Executable()
	sup := supervisor.NewSupervisor(ctx, self)
	// The operator CLI builds one gateway; a session created here defaults to it
	// (unless it is the terminal gateway). The concrete kind comes from the built
	// gateway's manifest, so the manager package still never names a platform.
	if o.DefaultGateways == nil {
		o.DefaultGateways = nonTerminalKinds([]Deps{d})
	}
	reg, _, err := buildRegistry(ctx, d, o, st, sup, instID)
	return reg, err
}
