package manager

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// Handler holds the dependencies the session/service/agent commands act on.
// Commands (commands.go) turns its methods into declared contracts.Cmd values
// the CLI dispatches.
type Handler struct {
	log             *slog.Logger
	d               channelAdmin
	td              channelAdmin
	sup             supervisor
	wt              Worktrees
	fg              forges
	up              updater
	agents          agentStore
	st              *state.State
	defaultCmd      string
	defaultGateways []string
	partDir         string
	coord           coordinationReader
	seed            func(name, task string) bool
	validateModel   func(vendor, modelID string) error
	gate            func(vendor string) (grain, why string)
	gatewayOnly     func() bool
	defaultModel    string
	vendorDefault   func(vendor string) string
	hs              Hosts
}

// SetHosts wires the placement resolver. Without it every session is local,
// which is exactly what herrscher did before hosts existed.
func (h *Handler) SetHosts(hs Hosts) { h.hs = hs }

func (h *Handler) worktreesOn(hostName string) (Worktrees, string, error) {
	if hostName == "" || h.hs == nil {
		if hostName != "" {
			return nil, "", fmt.Errorf("session wants host %q but %w", hostName, ErrNoHostRegistered)
		}
		return h.wt, h.st.WorkspaceRoot(), nil
	}
	worktrees, err := h.hs.Worktrees(hostName)
	if err != nil {
		return nil, "", err
	}
	workspaceRoot, err := h.hs.Workspace(hostName)
	if err != nil {
		return nil, "", err
	}
	return worktrees, workspaceRoot, nil
}

func (h *Handler) materializeOn(ctx context.Context, hostName string, profile agent.Agent, worktreePath string, hook bool) error {
	if hostName == "" || h.hs == nil {
		bin := ""
		if hook {
			bin = agent.SelfBin()
		}
		return profile.MaterializeAs(ctx, worktreePath, bin)
	}
	return h.hs.Materialize(ctx, hostName, profile, worktreePath, hook)
}

// SetDefaultModel wires the operator's configured default. It is a fallback,
// never an override: an explicit --model always wins.
func (h *Handler) SetDefaultModel(id string) { h.defaultModel = id }

// SetVendorDefaultModel wires the per-vendor default model lookup.
func (h *Handler) SetVendorDefaultModel(fn func(vendor string) string) { h.vendorDefault = fn }

type fallbackModel struct {
	source string
	id     string
}

func (h *Handler) resolveModel(modelID, vendor, agentModel string, cmdExplicit bool) string {
	if modelID != "" || cmdExplicit {
		return modelID
	}
	candidates := []fallbackModel{
		{source: "agent", id: agentModel},
		{source: "vendor", id: h.vendorDefaultFor(vendor)},
		{source: "global", id: h.defaultModel},
	}
	for _, candidate := range candidates {
		if candidate.id == "" {
			continue
		}
		if vendor != "" {
			if err := h.checkModel(vendor, candidate.id); err != nil {
				if candidate.source != "global" {
					h.log.Warn("default model skipped, it does not fit the vendor", "source", candidate.source, "model", candidate.id, "vendor", vendor, "err", err)
				}
				continue
			}
		}
		return candidate.id
	}
	return ""
}

func (h *Handler) vendorDefaultFor(vendor string) string {
	if vendor == "" || h.vendorDefault == nil {
		return ""
	}
	return h.vendorDefault(vendor)
}

func (h *Handler) agentDefaults(name string) (string, string) {
	if name == "" || h.agents == nil {
		return "", ""
	}
	profile, ok := h.agents.Get(name)
	if !ok {
		return "", ""
	}
	return profile.Model, profile.Backend
}

// SetGatewayOnly wires the active route policy. Under gateway-only every turn
// must run on the product's account, so the two ways of escaping the catalog
// have to be closed at the command:
//
//   - an explicit `cmd` is a free-form argv the catalog never sees. It can name
//     any binary and any `--model`, so it bills our account for something the
//     operator never selected, and it bypasses the policy outright.
//   - no `model` at all skips the catalog lookup entirely, so the spawn gets no
//     gateway environment and runs on the machine's own vendor login, silently,
//     while the session reads as gateway-routed.
//
// Both are legitimate on the internal build, where the machine's own login IS
// the intended account. This is why the check is policy-gated rather than
// unconditional.
func (h *Handler) SetGatewayOnly(fn func() bool) { h.gatewayOnly = fn }

func (h *Handler) checkSpawnSource(cmdExplicit bool, modelID string) error {
	if h.gatewayOnly == nil || !h.gatewayOnly() {
		return nil
	}
	if cmdExplicit {
		return ErrExplicitCmdRefused
	}
	if modelID == "" {
		return ErrModelRequired
	}
	return nil
}

// SetModelValidator wires the catalog check applied to `--vendor`/`--model` on
// session create/switch, so a typo, a policy-excluded id, or a vendor that does
// not own the model fails at the command instead of much later, as an opaque
// spawn failure, or worse, as a turn silently run on the machine's own login.
func (h *Handler) SetModelValidator(fn func(vendor, modelID string) error) { h.validateModel = fn }

// SetGateResolver wires the per-vendor approval granularity, read by the host
// from the compiled backends' manifests. Until it is set, every vendor reports
// that it enforces nothing, which is the only truthful answer a daemon with
// nothing to ask can give.
func (h *Handler) SetGateResolver(fn func(vendor string) (grain, why string)) { h.gate = fn }

type approvalGate struct {
	enforced bool
	why      string
}

func (h *Handler) gateFor(vendor string) approvalGate {
	if h.gate == nil {
		return approvalGate{why: "this daemon was built without a way to tell what its backends enforce"}
	}
	grain, why := h.gate(vendor)
	return approvalGate{enforced: grain == "tool", why: why}
}

func vendorLabel(vendor string) string {
	if vendor == "" {
		return "this backend"
	}
	return vendor
}

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

type coordinationReader interface {
	CoordinationView(name string) (CoordView, bool)
}

// NewHandler builds a Handler. defaultCmd is the bridge command used when a
// session is created without an explicit cmd. partDir is the directory under
// which per-session participant journals live (participants/<name>.log). agents
// owns the durable agent homes used to provision sessions.
func NewHandler(d channelAdmin, sup supervisor, wt Worktrees, fg forges, up updater, agents agentStore, st *state.State, defaultCmd, partDir string, defaultGateways []string) *Handler {
	return &Handler{log: obs.NewLogger(os.Stderr, slog.LevelInfo), d: d, sup: sup, wt: wt, fg: fg, up: up, agents: agents, st: st, defaultCmd: defaultCmd, partDir: partDir, defaultGateways: defaultGateways}
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
