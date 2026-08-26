package manager

import (
	"context"
	"fmt"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// Handler holds the dependencies the session/service/agent commands act on.
// Commands (commands.go) turns its methods into declared contracts.Cmd values
// the CLI dispatches.
type Handler struct {
	d          channelAdmin
	td         channelAdmin // terminal (TUI) admin; nil when no terminal gateway is bound
	sup        supervisor
	wt         Worktrees
	fg         forges
	up         updater
	agents     agentStore
	st         *state.State
	defaultCmd string
	// defaultGateways is the primary gateway set a session binds to when it names
	// none explicitly. The composition root injects the concrete platform kinds
	// (e.g. from the built non-terminal gateways) so this package never does.
	defaultGateways []string
	partDir         string             // dir holding participants/<name>.log journals
	coord           coordinationReader // nil until wired; session list omits coordination when nil
	// seed injects an opening turn into a live session (the same path handoff uses).
	// nil in the operator CLI (no live drivers); the daemon wires host.Seed.
	seed func(name, task string) bool
	// validateModel resolves a catalog model id under the active route policy and
	// returns a naming error when it is unknown, excluded, or owned by a backend
	// other than the requested vendor. The composition root wires host.LookupModel;
	// this package cannot import host (host imports it).
	// nil = no catalog wired, every id passes.
	validateModel func(vendor, modelID string) error
	// gatewayOnly reports whether the active route policy forbids native spawns.
	// The composition root wires host.ResolvePolicy; nil = unrestricted, which is
	// the internal build and the pre-policy behaviour.
	gatewayOnly func() bool
	// defaultModel is the catalog id a session gets when it names none, chosen at
	// `herrscher init`. Empty = no default, the pre-existing behaviour.
	defaultModel string
	// hs resolves where a session runs: which worktree implementation, which
	// workspace root. nil = everything is local, which is the operator CLI and
	// every daemon with no host registered.
	hs Hosts
}

// SetHosts wires the placement resolver. Without it every session is local,
// which is exactly what herrscher did before hosts existed.
func (h *Handler) SetHosts(hs Hosts) { h.hs = hs }

// worktreesOn returns the worktree implementation for a host, and the workspace
// root over there. An empty host is this machine, answered by the injected
// local Worktreer without a subprocess: the local case gains nothing from
// paying a process spawn to share a code path with the remote one.
func (h *Handler) worktreesOn(hostName string) (Worktrees, string, error) {
	if hostName == "" || h.hs == nil {
		if hostName != "" {
			return nil, "", fmt.Errorf("session wants host %q but no host is registered", hostName)
		}
		return h.wt, h.st.WorkspaceRoot(), nil
	}
	wt, err := h.hs.Worktrees(hostName)
	if err != nil {
		return nil, "", err
	}
	ws, err := h.hs.Workspace(hostName)
	if err != nil {
		return nil, "", err
	}
	return wt, ws, nil
}

// materializeOn provisions an agent into a worktree, wherever that worktree is.
func (h *Handler) materializeOn(ctx context.Context, hostName string, a agent.Agent, worktreePath string) error {
	if hostName == "" || h.hs == nil {
		return a.Materialize(worktreePath)
	}
	return h.hs.Materialize(ctx, hostName, a, worktreePath)
}

// SetDefaultModel wires the operator's configured default. It is a fallback,
// never an override: an explicit --model always wins.
func (h *Handler) SetDefaultModel(id string) { h.defaultModel = id }

// resolveModel fills in the configured default when a create names no model.
//
// Two cases deliberately keep an empty id rather than take the default:
//
//   - an explicit cmd, which carries its own invocation. Adding a model to it
//     would hand the spawn a gateway environment for a model the argv does not
//     name, so the session would report one model and run another.
//   - a vendor the default does not belong to. The default is a preference, so
//     an explicit --vendor outranks it; validation would otherwise reject a
//     command the operator wrote correctly.
func (h *Handler) resolveModel(modelID, vendor string, cmdExplicit bool) string {
	if modelID != "" || h.defaultModel == "" || cmdExplicit {
		return modelID
	}
	if vendor != "" && h.checkModel(vendor, h.defaultModel) != nil {
		return ""
	}
	return h.defaultModel
}

// SetGatewayOnly wires the active route policy. Under gateway-only every turn
// must run on the product's account, so the two ways of escaping the catalog
// have to be closed at the command:
//
//   - an explicit `cmd` is a free-form argv the catalog never sees. It can name
//     any binary and any `--model`, so it bills our account for something the
//     operator never selected, and it bypasses the policy outright.
//   - no `model` at all skips the catalog lookup entirely, so the spawn gets no
//     gateway environment and runs on the machine's own vendor login — silently,
//     while the session reads as gateway-routed.
//
// Both are legitimate on the internal build, where the machine's own login IS
// the intended account. This is why the check is policy-gated rather than
// unconditional.
func (h *Handler) SetGatewayOnly(fn func() bool) { h.gatewayOnly = fn }

// checkSpawnSource refuses the two escapes above. It is deliberately separate
// from checkModel: that one validates a supplied id, this one constrains what
// may be supplied at all.
func (h *Handler) checkSpawnSource(cmdExplicit bool, modelID string) error {
	if h.gatewayOnly == nil || !h.gatewayOnly() {
		return nil
	}
	if cmdExplicit {
		return fmt.Errorf("an explicit cmd is refused under the gateway-only route policy: it bypasses the model catalog — select a model instead")
	}
	if modelID == "" {
		return fmt.Errorf("a model is required under the gateway-only route policy: without one the session would spawn on this machine's own vendor login")
	}
	return nil
}

// SetModelValidator wires the catalog check applied to `--vendor`/`--model` on
// session create/switch, so a typo, a policy-excluded id, or a vendor that does
// not own the model fails at the command instead of much later, as an opaque
// spawn failure — or worse, as a turn silently run on the machine's own login.
func (h *Handler) SetModelValidator(fn func(vendor, modelID string) error) { h.validateModel = fn }

// checkModel validates a supplied vendor/model pair. An empty id is the legacy
// path (the model rides in cmd) and is always accepted; an empty vendor means
// "whichever backend owns the model", which the catalog resolves.
func (h *Handler) checkModel(vendor, modelID string) error {
	if modelID == "" || h.validateModel == nil {
		return nil
	}
	return h.validateModel(vendor, modelID)
}

// CoordView mirrors host.CoordinationView so the manager stays decoupled from
// the host package (no import cycle). The host wires an adapter implementing
// coordinationReader.
type CoordView struct {
	Role     string
	Lead     string
	Reported int
	Expected int
	Complete bool
}

// coordinationReader supplies a session's join state for session list enrichment.
type coordinationReader interface {
	CoordinationView(name string) (CoordView, bool)
}

// NewHandler builds a Handler. defaultCmd is the bridge command used when a
// session is created without an explicit cmd. partDir is the directory under
// which per-session participant journals live (participants/<name>.log). agents
// owns the durable agent homes used to provision sessions.
func NewHandler(d channelAdmin, sup supervisor, wt Worktrees, fg forges, up updater, agents agentStore, st *state.State, defaultCmd, partDir string, defaultGateways []string) *Handler {
	return &Handler{d: d, sup: sup, wt: wt, fg: fg, up: up, agents: agents, st: st, defaultCmd: defaultCmd, partDir: partDir, defaultGateways: defaultGateways}
}

// SetTerminalAdmin wires the terminal (TUI) channel admin used to route
// terminal-only sessions to a local terminal channel instead of the operator's
// home gateway. nil-safe: until set, terminal-only sessions fall back to the
// home gateway's admin.
func (h *Handler) SetTerminalAdmin(td channelAdmin) { h.td = td }

// PartDir returns the participants journal directory (used by tests/wiring).
func (h *Handler) PartDir() string { return h.partDir }

// SetCoordinationReader wires the join-state source used to enrich session list.
// nil-safe: until set, session list omits the coordination field.
func (h *Handler) SetCoordinationReader(r coordinationReader) { h.coord = r }

// SetSeeder wires the live-session seed injector (host.Seed). The daemon calls
// this; the operator CLI leaves it nil (switch --handoff none still works).
func (h *Handler) SetSeeder(fn func(name, task string) bool) { h.seed = fn }

// Agents returns the durable agent store (used by tests/wiring).
func (h *Handler) Agents() agentStore { return h.agents }
