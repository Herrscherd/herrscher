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

// hostDeps surfaces pieces buildRegistry constructs internally that a caller may
// need beyond the registry itself — the worktree manager and agent store the
// boot-time Coordinator (Task 7/8) is built from. Kept minimal so the operator
// CLI (NewRegistry), which has no Coordinator to wire, can ignore it.
type hostDeps struct {
	wt        *worktree.Worktreer
	agents    *agent.Store
	handler   *manager.Handler
	seedCoord *coordinatorSlot
}

type coordinatorSlot struct {
	coord contracts.Coordinator
}

// buildRegistry constructs the session/service command handler over a given
// state + supervisor and registers its commands into a fresh CLI registry. The
// daemon (RunHub) and the operator CLI (NewRegistry) share this so a session
// created either way is built from identical deps. d.Admin supplies the channel
// admin port; instID namespaces shared git/gateway resources.
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
	hdl.SetSeeder(Seed) // host.Seed: live-session injection for `session switch` handoff
	// Reject an unknown/policy-excluded --model, or one owned by a backend other
	// than --vendor, at create/switch rather than on the first spawn. Uses the
	// same lookup + policy the spawn choke point does.
	// The route policy itself, so the manager can refuse the two ways of escaping
	// the catalog (a free-form cmd, or no model at all) without importing host.
	hdl.SetGatewayOnly(func() bool { return ResolvePolicy(os.Getenv) == contracts.PolicyGatewayOnly })
	// The model a session gets when it names none, as chosen at `herrscher init`.
	// Read once here rather than per-create: the daemon's environment is fixed for
	// its lifetime, and a mid-flight change would silently repoint live sessions.
	hdl.SetDefaultModel(os.Getenv(EnvDefaultModel))
	hdl.SetModelValidator(func(vendor, modelID string) error {
		entry, err := LookupModel(contracts.Default.Backends(), ResolvePolicy(os.Getenv), modelID)
		if err != nil {
			return err
		}
		if vendor != "" && vendor != entry.Vendor {
			return fmt.Errorf("model %q belongs to backend %q, but backend %q was requested — pick a model offered by %q or switch the vendor", modelID, entry.Vendor, vendor, vendor)
		}
		return nil
	})
	seedCoord := &coordinatorSlot{}

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
	// Verbs contributed by the loaded gateways, already namespaced under their
	// plugin's kind (see contributedCommands). Added after the host's own so a
	// plugin can never shadow a built-in: reg.Add refuses the second of a pair,
	// and the built-in is the one that must win.
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
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			rawTurnID, supplied := in.Lookup("turn_id")
			turnID, err := resolveTurnID(rawTurnID, supplied)
			if err != nil {
				return "", err
			}
			reply, err := runOneShotSeedCommand(
				cmdCtx, st, in.Get("name"), in.Get("task"),
				turnID, oneShotSeedRuntimeFrom(cmdCtx),
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
	// session send / session interrupt reach into the live turn loop of whichever
	// process runs them, and that is the point: forwarded over the command socket
	// they are how a client — an attached TUI — speaks into a session the daemon
	// owns. Run with no daemon listening they find no live session and say so,
	// which is the honest answer: there is nothing to speak into.
	if err := reg.Add(contracts.New("session", "send").
		Help("send one message into a live session's turn queue (the daemon runs the turn)").
		Param("name", "session name", true).
		Param("text", "message text", true).
		Param("author", "display name shown to the agent (default: you)", false).
		Param("attachments", "JSON array of local file paths to attach", false).
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
				Author:      author,
				AuthorID:    "local",
				Text:        in.Get("text"),
				Attachments: atts,
			}) {
				return "", fmt.Errorf("no live session named %q", in.Get("name"))
			}
			return "", nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("session", "info").
		Help("list sessions as a frontend sees them (channel, vendor, dir, archived) — JSON only").
		Do(func(_ context.Context, _ contracts.Input) (string, error) {
			b, err := json.Marshal(sessionInfos(st, partDir))
			return string(b), err
		})); err != nil {
		return nil, hostDeps{}, err
	}
	if err := reg.Add(contracts.New("session", "scrollback").
		Help("dump a session's replayable history as the frontend renders it — JSON only").
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
	// Registered last, so it describes a complete registry. A frontend that draws
	// its own command menu asks for this instead of keeping a copy of the verb
	// list beside the daemon's — a copy drifts silently, and the drift shows up as
	// a palette that offers a fraction of what the daemon would accept.
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
	return reg, hostDeps{wt: wt, agents: agents, handler: hdl, seedCoord: seedCoord}, nil
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
	// `session switch` restarts a bridge from this process too, so it needs the
	// same trusted hand-off of the captured gateway pair the daemon does.
	sup.SetBridgeEnv(GatewayEnvPairs())
	// The operator CLI builds one gateway; a session created here defaults to it
	// (unless it is the terminal gateway). The concrete kind comes from the built
	// gateway's manifest, so the manager package still never names a platform.
	if o.DefaultGateways == nil {
		o.DefaultGateways = nonTerminalKinds([]Deps{d})
	}
	// No contributed commands on the one-shot CLI path: they are contributed by
	// live gateway instances, and instantiating a gateway — opening a connection,
	// spending a token — so that a local `herrscher session list` can parse an
	// argv it will not use is a cost paid by every invocation for a case that
	// does not arise. The agent always runs under the daemon.
	reg, _, err := buildRegistry(ctx, d, o, st, sup, instID, nil)
	if err != nil {
		return nil, err
	}
	// This is the operator process, not the daemon: it holds its own copy of the
	// state and must not decide alone what the live sessions are. Installed here
	// rather than in buildRegistry, which the daemon shares — the daemon would be
	// asking itself.
	forwardSessionCommands(reg, instID, dispatchLiveCommand)
	return reg, nil
}
