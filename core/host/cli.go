package host

import (
	"context"
	"encoding/json"
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
	wt     *worktree.Worktreer
	agents *agent.Store
}

// buildRegistry constructs the session/service command handler over a given
// state + supervisor and registers its commands into a fresh CLI registry. The
// daemon (RunHub) and the operator CLI (NewRegistry) share this so a session
// created either way is built from identical deps. d.Admin supplies the channel
// admin port; instID namespaces shared git/Discord resources.
func buildRegistry(ctx context.Context, d Deps, o Options, st *state.State, sup *supervisor.Supervisor, instID string) (*cli.Registry, hostDeps, error) {
	partDir := filepath.Dir(o.StatePath)
	wt := worktree.NewWorktreer(ctx, instID)
	fg := forge.New()
	upCfg, _ := service.DefaultConfig()
	up := serviceUpdater{cfg: upCfg, st: st}
	agents := agent.NewStore(filepath.Join(partDir, "agents"))
	hdl := manager.NewHandler(d.Admin, sup, wt, fg, up, agents, st, o.DefaultCmd, partDir)

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
	return reg, hostDeps{wt: wt, agents: agents}, nil
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
	reg, _, err := buildRegistry(ctx, d, o, st, sup, instID)
	return reg, err
}
