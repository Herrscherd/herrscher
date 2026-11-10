package host

import (
	"context"
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/envx"
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

func buildRegistry(ctx context.Context, deps Deps, options Options, hostState *state.State, sup *supervisor.Supervisor, instID string, extra []contracts.Cmd, approvals *approvalTable, sessions *sessionTable) (*cli.Registry, hostDeps, error) {
	options = withOperatorLanguage(options)
	partDir := filepath.Dir(options.StatePath)
	worktrees := worktree.NewWorktreer(instID)
	upCfg, cfgErr := service.DefaultConfig()
	logCleanup(cfgErr)
	updater := serviceUpdater{cfg: upCfg, st: hostState}
	agents := agent.NewStore(filepath.Join(partDir, "agents"))
	if n, ok := agent.UserBudgetFromEnv(os.Getenv); ok {
		agents.SetUserBudget(n)
	}
	models := options.modelDiscovery()
	hdl := newManagerHandler(deps, options, partDir, hostState, sup, worktrees, updater, agents, models)
	wireManagerHandler(ctx, hdl, hostState, instID, worktrees, sessions, models)
	wireSupervisor(sup, hostState, instID)
	seedCoord := &coordinatorSlot{}
	schedSlot := &schedulerSlot{}

	reg := &cli.Registry{}
	if err := addVerbs(reg, hdl.Commands()...); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addVerbs(reg, ModelsCommands(options.live.Get)...); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addVerbs(reg, extra...); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addSessionVerbs(reg, options.live.Get, hostState, partDir, instID, approvals, sessions, seedCoord, models); err != nil {
		return nil, hostDeps{}, err
	}
	handoff, err := sessionHandoffCmd(options.live.Get, hdl.Commands(), hostState, sessions, instID, seedCoord, hdl.PreflightAccount)
	if err != nil {
		return nil, hostDeps{}, err
	}
	if err := addVerbs(reg, handoff); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addCheckpointCommands(reg, options.live.Get, hostState, sessions); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addMemoryVerbs(reg, options.live.Get); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addSkillVerbs(reg, options.live.Get); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addScheduleCommands(reg, options.live.Get, hostState, agents, schedSlot); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addHostCommands(reg, hostState); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addApproveCommands(reg, options.live.Get, hostState, agents, approvals, sessions); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addRoleCommands(reg, options.live.Get, hostState); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addVerbs(reg, memorySearchVerb(options.live.Get), whoamiVerb(), commandsVerb(options.live.Get, reg), healthVerb(options.health), usageVerb(options.usage)); err != nil {
		return nil, hostDeps{}, err
	}
	localizeCommandErrors(reg, hostState, options.live)
	return reg, hostDeps{wt: worktrees, agents: agents, handler: hdl, seedCoord: seedCoord, schedSlot: schedSlot}, nil
}

func addVerbs(reg *cli.Registry, cmds ...contracts.Cmd) error {
	for _, cmd := range cmds {
		if err := reg.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newManagerHandler(deps Deps, options Options, partDir string, hostState *state.State, sup *supervisor.Supervisor, worktrees manager.Worktrees, updater serviceUpdater, agents *agent.Store, models *ModelDiscovery) *manager.Handler {
	options = withOperatorLanguage(options)
	launch := func(vendor, modelID, cmd string) (string, error) { return launchVendor(models, vendor, modelID, cmd) }
	hdl := manager.NewHandler(deps.Admin, sup, worktrees, forge.New(), updater, agents, hostState, options.DefaultCmd, partDir, options.DefaultGateways, validateProfileChoice, launch)
	hdl.SetRemoteBackend(options.RemoteCategories[contracts.CategoryBackend])
	hdl.SetLanguage(options.live.Get())
	hdl.SetDefaultLanguage(options.DefaultLanguage)
	hdl.OnLanguageChange(options.live.set)
	return hdl
}

func wireManagerHandler(ctx context.Context, hdl *manager.Handler, hostState *state.State, instID string, worktrees *worktree.Worktreer, sessions *sessionTable, models *ModelDiscovery) {
	hdl.SetSeeder(sessions.Seed)
	hdl.SetHosts(newHostPlacer(hostState, instID, worktrees, func() string { return sourceVersionOf(ctx, hostState) }))
	hdl.SetGatewayOnly(func() bool { return ResolvePolicy(envx.Getenv) == contracts.PolicyGatewayOnly })
	hdl.SetDefaultModel(envx.Getenv(EnvDefaultModel))
	hdl.SetVendorDefaultModel(func(vendor string) string { return envx.Getenv(VendorDefaultModelEnv(vendor)) })
	hdl.SetModelValidator(func(vendor, modelID string) error {
		return validateModelChoice(models, vendor, modelID)
	})
	hdl.SetGateResolver(GateFor)
}

func launchVendor(models *ModelDiscovery, vendor, modelID, cmd string) (string, error) {
	plugins := contracts.Default.Backends()
	chosen, _, err := chooseVendor(vendor, modelID, plugins, models)
	if err != nil {
		return "", err
	}
	if chosen == "" {
		chosen = vendorOfCmd(cmd, plugins)
	}
	if chosen != "" {
		return chosen, nil
	}
	plugin, err := selectBackend("", plugins)
	if err != nil {
		return "", err
	}
	return plugin.Manifest.Kind, nil
}

func validateModelChoice(models *ModelDiscovery, vendor, modelID string) error {
	entry, err := LookupModelWith(contracts.Default.Backends(), ResolvePolicy(envx.Getenv), models.Sources(), modelID)
	if err != nil {
		return err
	}
	if vendor != "" && vendor != entry.Vendor {
		return &modelBackendMismatchError{Model: modelID, Owner: entry.Vendor, Requested: vendor}
	}
	return nil
}

func wireSupervisor(sup *supervisor.Supervisor, store *state.State, instID string) {
	sup.SetInstanceID(instID)
	sup.SetCommandSocket(func(session string) string { return SessionCommandSocketPath(instID, session) })
	sup.SetHostLookup(func(name string) (supervisor.Placement, bool) {
		host, ok := store.FindHost(name)
		if !ok {
			return supervisor.Placement{}, false
		}
		return supervisor.Placement{SSH: host.SSH, Bin: host.Bin}, true
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
func NewRegistry(ctx context.Context, gws []Deps, options Options) (*cli.Registry, error) {
	hostState, err := state.LoadState(options.StatePath)
	if err != nil {
		return nil, err
	}
	var home *state.HomeRef
	if options.Home != nil {
		home = &state.HomeRef{ID: options.Home.ID, Type: options.Home.Type}
	}
	hostState.ApplyDefaults(home, options.Workspace, options.Source)

	instID, err := resolveInstanceID(hostState, options.InstanceID, options.Owner, obs.Stderr(false))
	if err != nil {
		return nil, localizeHostError(options.operatorLanguage(), err)
	}

	self, execErr := os.Executable()
	logCleanup(execErr)
	sup := supervisor.NewSupervisor(ctx, self)
	sup.SetStatePath(options.StatePath)
	sup.SetBridgeEnv(append(GatewayEnvPairs(), AnnounceKeyEnvPairs()...))
	if options.DefaultGateways == nil {
		options.DefaultGateways = nonTerminalKinds(gws)
	}
	reg, deps, err := buildRegistry(ctx, Deps{Admin: adminForHome(gws, hostState.Home)}, options, hostState, sup, instID, nil, newApprovalTable(), newSessionTable())
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
