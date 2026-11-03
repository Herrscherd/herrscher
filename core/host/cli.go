package host

import (
	"context"
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

type hostDeps struct {
	wt        *worktree.Worktreer
	agents    *agent.Store
	handler   *manager.Handler
	seedCoord *coordinatorSlot
	schedSlot *schedulerSlot
}

type coordinatorSlot struct {
	coord contracts.Coordinator
}

type schedulerSlot struct {
	sched scheduleRunner
}

func buildRegistry(ctx context.Context, d Deps, o Options, st *state.State, sup *supervisor.Supervisor, instID string, extra []contracts.Cmd, approvals *approvalTable, sessions *sessionTable) (*cli.Registry, hostDeps, error) {
	partDir := filepath.Dir(o.StatePath)
	wt := worktree.NewWorktreer(ctx, instID)
	upCfg, cfgErr := service.DefaultConfig()
	logCleanup(cfgErr)
	up := serviceUpdater{cfg: upCfg, st: st}
	agents := agent.NewStore(filepath.Join(partDir, "agents"))
	if n, ok := agent.UserBudgetFromEnv(os.Getenv); ok {
		agents.SetUserBudget(n)
	}
	hdl := manager.NewHandler(d.Admin, sup, wt, forge.New(), up, agents, st, o.DefaultCmd, partDir, o.DefaultGateways)
	wireManagerHandler(ctx, hdl, st, instID, wt, sessions)
	wireSupervisor(sup, st, instID)
	seedCoord := &coordinatorSlot{}
	schedSlot := &schedulerSlot{}

	reg := &cli.Registry{}
	if err := addVerbs(reg, hdl.Commands()...); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addVerbs(reg, ModelsCommands()...); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addVerbs(reg, extra...); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addSessionVerbs(reg, st, partDir, instID, sessions, seedCoord); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addMemoryVerbs(reg); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addSkillVerbs(reg); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addScheduleCommands(reg, st, agents, schedSlot); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addHostCommands(reg, st); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addApproveCommands(reg, st, agents, approvals, sessions); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addRoleCommands(reg, st); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addVerbs(reg, memorySearchVerb(), whoamiVerb(), commandsVerb(reg)); err != nil {
		return nil, hostDeps{}, err
	}
	return reg, hostDeps{wt: wt, agents: agents, handler: hdl, seedCoord: seedCoord, schedSlot: schedSlot}, nil
}

func addVerbs(reg *cli.Registry, cmds ...contracts.Cmd) error {
	for _, cmd := range cmds {
		if err := reg.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func wireManagerHandler(ctx context.Context, hdl *manager.Handler, st *state.State, instID string, wt *worktree.Worktreer, sessions *sessionTable) {
	hdl.SetSeeder(sessions.Seed)
	hdl.SetHosts(newHostPlacer(ctx, st, instID, wt, func() string { return sourceVersionOf(ctx, st) }))
	hdl.SetGatewayOnly(func() bool { return ResolvePolicy(os.Getenv) == contracts.PolicyGatewayOnly })
	hdl.SetDefaultModel(os.Getenv(EnvDefaultModel))
	hdl.SetModelValidator(validateModelChoice)
	hdl.SetGateResolver(GateFor)
}

func validateModelChoice(vendor, modelID string) error {
	entry, err := LookupModel(contracts.Default.Backends(), ResolvePolicy(os.Getenv), modelID)
	if err != nil {
		return err
	}
	if vendor != "" && vendor != entry.Vendor {
		return &modelBackendMismatchError{Model: modelID, Owner: entry.Vendor, Requested: vendor}
	}
	return nil
}

func wireSupervisor(sup *supervisor.Supervisor, st *state.State, instID string) {
	sup.SetInstanceID(instID)
	sup.SetCommandSocket(func(session string) string { return SessionCommandSocketPath(instID, session) })
	sup.SetHostLookup(func(name string) (supervisor.Placement, bool) {
		h, ok := st.FindHost(name)
		if !ok {
			return supervisor.Placement{}, false
		}
		return supervisor.Placement{SSH: h.SSH, Bin: h.Bin}, true
	})
	sup.SetGateResolver(func(vendor string) string {
		grain, _ := GateFor(vendor)
		return grain
	})
}

// NewRegistry builds the operator CLI registry: it loads its own state +
// supervisor (the operator invocation is a short-lived process) and registers
// the session/service handler's commands. The returned registry dispatches argv
// (see core/cli).
//
// It takes every built gateway set rather than one, for the same reason RunHub
// does: which admin a session channel is minted on is a question about the set:
// the home's owner for a normal session, the terminal gateway for a
// terminal-only one. One set can only ever answer the first.
func NewRegistry(ctx context.Context, gws []Deps, o Options) (*cli.Registry, error) {
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

	self, execErr := os.Executable()
	logCleanup(execErr)
	sup := supervisor.NewSupervisor(ctx, self)
	sup.SetBridgeEnv(GatewayEnvPairs())
	if o.DefaultGateways == nil {
		o.DefaultGateways = nonTerminalKinds(gws)
	}
	reg, deps, err := buildRegistry(ctx, Deps{Admin: adminForHome(gws, st.Home)}, o, st, sup, instID, nil, newApprovalTable(), newSessionTable())
	if err != nil {
		return nil, err
	}
	if ta := terminalAdmin(gws); ta != nil {
		deps.handler.SetTerminalAdmin(ta)
	}
	forwardDaemonOwnedCommands(reg, instID, dispatchLiveCommand)
	return reg, nil
}

func memoryKindAllowed(kind contracts.NodeKind) bool {
	switch kind {
	case contracts.KindOrganization, contracts.KindProject, contracts.KindRepo,
		contracts.KindServer, contracts.KindArchitecture, contracts.KindProduction,
		contracts.KindSession, contracts.KindDecision, contracts.KindUser,
		contracts.KindAgent, contracts.KindDomain:
		return true
	case contracts.KindTranscript, contracts.KindSkill:
		return false
	}
	return false
}
