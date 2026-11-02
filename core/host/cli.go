package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	orchestrator "github.com/Herrscherd/herrscher-orchestrator"
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

func buildRegistry(ctx context.Context, d Deps, o Options, st *state.State, sup *supervisor.Supervisor, instID string, extra []contracts.Cmd) (*cli.Registry, hostDeps, error) {
	partDir := filepath.Dir(o.StatePath)
	wt := worktree.NewWorktreer(ctx, instID)
	fg := forge.New()
	upCfg, _ := service.DefaultConfig()
	up := serviceUpdater{cfg: upCfg, st: st}
	agents := agent.NewStore(filepath.Join(partDir, "agents"))
	if n, ok := agent.UserBudgetFromEnv(os.Getenv); ok {
		agents.SetUserBudget(n)
	}
	hdl := manager.NewHandler(d.Admin, sup, wt, fg, up, agents, st, o.DefaultCmd, partDir, o.DefaultGateways)
	hdl.SetSeeder(Seed)
	hdl.SetHosts(hostPlacer{
		ctx:           ctx,
		st:            st,
		instanceID:    instID,
		local:         wt,
		sourceVersion: func() string { return sourceVersionOf(ctx, st) },
	})
	sup.SetInstanceID(instID)
	sup.SetCommandSocket(func(session string) string { return SessionCommandSocketPath(instID, session) })
	sup.SetHostLookup(func(name string) (supervisor.Placement, bool) {
		h, ok := st.FindHost(name)
		if !ok {
			return supervisor.Placement{}, false
		}
		return supervisor.Placement{SSH: h.SSH, Bin: h.Bin}, true
	})
	hdl.SetGatewayOnly(func() bool { return ResolvePolicy(os.Getenv) == contracts.PolicyGatewayOnly })
	hdl.SetDefaultModel(os.Getenv(EnvDefaultModel))
	hdl.SetModelValidator(func(vendor, modelID string) error {
		entry, err := LookupModel(contracts.Default.Backends(), ResolvePolicy(os.Getenv), modelID)
		if err != nil {
			return err
		}
		if vendor != "" && vendor != entry.Vendor {
			return fmt.Errorf("model %q belongs to backend %q, but backend %q was requested; pick a model offered by %q or switch the vendor", modelID, entry.Vendor, vendor, vendor)
		}
		return nil
	})
	hdl.SetGateResolver(GateFor)
	sup.SetGateResolver(func(vendor string) string {
		grain, _ := GateFor(vendor)
		return grain
	})
	seedCoord := &coordinatorSlot{}
	schedSlot := &schedulerSlot{}

	reg := &cli.Registry{}
	for _, c := range hdl.Commands() {
		if err := reg.Add(c); err != nil {
			return nil, hostDeps{}, err
		}
	}
	for _, c := range ModelsCommands() {
		if err := reg.Add(c); err != nil {
			return nil, hostDeps{}, err
		}
	}
	for _, c := range extra {
		if err := reg.Add(c); err != nil {
			return nil, hostDeps{}, err
		}
	}
	if err := reg.Add(contracts.New("session", "seed").
		Help("run one agent turn in a session and print the reply").
		Param("name", "session name", true).
		Param("task", "opening task", true).
		ValueParam("turn_id", "optional caller-supplied turn identity", false).
		ValueParam("timeout", "cap this turn (Go duration, e.g. 30m); default HERRSCHER_SEED_TIMEOUT then 120s", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			rawTurnID, supplied := in.Lookup("turn_id")
			turnID, err := resolveTurnID(rawTurnID, supplied)
			if err != nil {
				return "", err
			}
			timeout, err := resolveSeedTimeout(in.Get("timeout"), os.Getenv)
			if err != nil {
				return "", err
			}
			runtime := oneShotSeedRuntimeFrom(cmdCtx)
			runtime.timeout = timeout
			reply, err := runOneShotSeedCommand(
				cmdCtx, st, in.Get("name"), in.Get("task"),
				turnID, runtime,
				seedCoord.coord, instID, dispatchLiveCommand,
			)
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
	if err := reg.Add(contracts.New("session", "send").
		Help("send one message into a live session's turn queue (the daemon runs the turn)").
		Param("name", "session name", true).
		Param("text", "message text", true).
		Param("author", "display name shown to the agent (default: you)", false).
		Param("attachments", "JSON array of local file paths to attach", false).
		Param("origin", "gateway kind this was typed in (e.g. terminal): the turn is rendered there "+
			"instead of in every channel the session is bound to", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			atts, err := parseAttachmentPaths(in.Get("attachments"))
			if err != nil {
				return "", err
			}
			author := in.Get("author")
			if author == "" {
				author = "you"
			}
			if !Submit(in.Get("name"), contracts.Inbound{
				Author:       author,
				AuthorID:     "local",
				Text:         in.Get("text"),
				Attachments:  atts,
				Conversation: contracts.Conversation{Gateway: in.Get("origin")},
			}) {
				return "", fmt.Errorf("no live session named %q", in.Get("name"))
			}
			return "", nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("session", "info").
		Help("list sessions as a frontend sees them (channel, vendor, dir, archived). JSON only").
		Do(func(_ context.Context, _ contracts.Input) (string, error) {
			b, err := json.Marshal(sessionInfos(st, partDir))
			return string(b), err
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("session", "scrollback").
		Help("dump a session's replayable history as the frontend renders it. JSON only").
		Param("name", "session name", true).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			b, err := json.Marshal(scrollbackOf(partDir, in.Get("name")))
			return string(b), err
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("session", "interrupt").
		Help("cancel a session's in-flight turn").
		Param("name", "session name", true).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			if !Interrupt(in.Get("name")) {
				return "", fmt.Errorf("session %q has no turn in flight", in.Get("name"))
			}
			return "interrupted " + in.Get("name"), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
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
	if err := reg.Add(contracts.New("memory", "restore").
		Help("reactivate an archived (or merged) memory node by key").
		Param("key", "node key", true).
		Param("force", "detach from its umbrella if the node was merged", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			force := in.Bool("force")
			if _, err := orchestrator.Restore(cmdCtx, mem, in.Get("key"), orchestrator.Force(force)); err != nil {
				return "", err
			}
			return "restored " + in.Get("key"), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("memory", "unlink").
		Help("remove the edge from→to between two memory nodes").
		Param("from", "source node key", true).
		Param("to", "target node key", true).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			if err := mem.Unlink(cmdCtx, in.Get("from"), in.Get("to")); err != nil {
				return "", err
			}
			return "unlinked " + in.Get("from") + " -> " + in.Get("to"), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("skill", "list").
		Help("list the skills agents wrote for themselves, with their state, approval and last use").
		Param("limit", "max rows (default 50)", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			limit := 50
			if v, err := strconv.Atoi(in.Get("limit")); err == nil && v > 0 {
				limit = v
			}
			hits, err := mem.Search(cmdCtx, contracts.Query{
				Kinds:           []contracts.NodeKind{contracts.KindSkill},
				IncludeArchived: true,
				Limit:           limit,
			})
			if err != nil {
				return "", err
			}
			if len(hits) == 0 {
				return "no learned skills yet", nil
			}
			var b strings.Builder
			for _, n := range hits {
				state := n.Meta[contracts.MetaState]
				if state == "" {
					state = contracts.StateActive
				}
				sharing := "private"
				if n.Meta[orchestrator.MetaApproved] != "" {
					sharing = "approved"
				}
				lastSeen := n.Meta[contracts.MetaLastSeen]
				if lastSeen == "" {
					lastSeen = "never"
				}
				fmt.Fprintf(&b, "%s\t%s\t%s\tlast used %s\n", n.Key, state, sharing, lastSeen)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("skill", "approve").
		Help("let a learned skill be promoted from its agent's private scope to the shared project scope").
		Param("key", "skill node key (see `skill list`)", true).
		Param("revoke", "clear the approval instead; a promotion already made is not undone", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			key := in.Get("key")
			approve := !in.Bool("revoke")
			if err := orchestrator.ApproveSkill(cmdCtx, mem, key, approve); err != nil {
				return "", err
			}
			if !approve {
				return "revoked " + key + " (a promotion already made stands; unmake it with `memory unlink` and `memory restore`)", nil
			}
			return "approved " + key, nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addScheduleCommands(reg, st, agents, schedSlot); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addHostCommands(reg, st); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addApproveCommands(reg, st, agents); err != nil {
		return nil, hostDeps{}, err
	}
	if err := addRoleCommands(reg, st); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("memory", "search").
		Help("full-text search the memory vault; --raw also searches the raw per-turn transcript tier (G7)").
		Param("text", "query text", true).
		Param("raw", "include raw per-turn transcript chunks (G7 archival tier)", false).
		Param("limit", "max hits (default 10)", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			limit := 10
			if v, err := strconv.Atoi(in.Get("limit")); err == nil && v > 0 {
				limit = v
			}
			hits, err := mem.Search(cmdCtx, contracts.Query{
				Text:       in.Get("text"),
				Ranked:     true,
				Limit:      limit,
				IncludeRaw: in.Bool("raw"),
			})
			if err != nil {
				return "", err
			}
			if len(hits) == 0 {
				return "no matches", nil
			}
			var b strings.Builder
			for _, n := range hits {
				fmt.Fprintf(&b, "%s\t[%s]\t%s\n", n.Key, n.Kind, n.Title)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("whoami").
		Help("print who git on this machine says you are").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return WhoamiOut(in.JSON)
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("commands").
		Help("list every command this daemon dispatches, with its params").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			specs := reg.Specs()
			if in.JSON {
				b, err := json.Marshal(specs)
				return string(b), err
			}
			return reg.Help(), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	return reg, hostDeps{wt: wt, agents: agents, handler: hdl, seedCoord: seedCoord, schedSlot: schedSlot}, nil
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

	self, _ := os.Executable()
	sup := supervisor.NewSupervisor(ctx, self)
	sup.SetBridgeEnv(GatewayEnvPairs())
	if o.DefaultGateways == nil {
		o.DefaultGateways = nonTerminalKinds(gws)
	}
	reg, deps, err := buildRegistry(ctx, Deps{Admin: adminForHome(gws, st.Home)}, o, st, sup, instID, nil)
	if err != nil {
		return nil, err
	}
	if ta := terminalAdmin(gws); ta != nil {
		deps.handler.SetTerminalAdmin(ta)
	}
	forwardDaemonOwnedCommands(reg, instID, dispatchLiveCommand)
	return reg, nil
}
