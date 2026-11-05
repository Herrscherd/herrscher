package manager

import (
	"context"
	"errors"
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
	resolveVendor   func(vendor, modelID string) (string, error)
	coord           coordinationReader
	seed            func(name, task string) bool
	validateModel   func(vendor, modelID string) error
	validateAccount func(vendor, profile string) error
	gate            func(vendor string) (grain, why string)
	gatewayOnly     func() bool
	defaultModel    string
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

// ErrAccountVendor is returned when an account is requested for a session whose
// backend vendor cannot be determined, so the account cannot be checked.
var ErrAccountVendor = errors.New("no backend vendor is known for this session: pass --vendor, or set HERRSCHER_BACKEND")

// ErrAccountHost is returned when an account is requested for a session placed on
// another host, which resolves accounts in its own store.
var ErrAccountHost = errors.New("an account cannot be used for a session placed on another host")

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

func (h *Handler) checkAccount(runtime runtimeChoice, profile string) error {
	if profile == "" || h.validateAccount == nil {
		return nil
	}
	vendor, err := h.launchVendor(runtime)
	if err != nil {
		return fmt.Errorf("account %q: %w: %w", profile, ErrAccountVendor, err)
	}
	if vendor == "" {
		return fmt.Errorf("account %q: %w", profile, ErrAccountVendor)
	}
	return h.validateAccount(vendor, profile)
}

func checkAccountHost(hostName, account string) error {
	if hostName == "" || account == "" {
		return nil
	}
	return fmt.Errorf("account %q on host %q: %w; run the session on this host or drop the account", account, hostName, ErrAccountHost)
}

func (h *Handler) launchVendor(runtime runtimeChoice) (string, error) {
	desired := h.vendorOf(runtime)
	if h.resolveVendor == nil {
		return desired, nil
	}
	return h.resolveVendor(desired, runtime.model)
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
func NewHandler(d channelAdmin, sup supervisor, wt Worktrees, fg forges, up updater, agents agentStore, st *state.State, defaultCmd, partDir string, defaultGateways []string, validateAccount func(vendor, profile string) error, resolveVendor func(vendor, modelID string) (string, error)) *Handler {
	return &Handler{log: obs.NewLogger(os.Stderr, slog.LevelInfo), d: d, sup: sup, wt: wt, fg: fg, up: up, agents: agents, st: st, defaultCmd: defaultCmd, partDir: partDir, defaultGateways: defaultGateways, validateAccount: validateAccount, resolveVendor: resolveVendor}
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
