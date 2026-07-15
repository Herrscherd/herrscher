package manager

import (
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// Handler holds the dependencies the session/service/agent commands act on.
// Commands (commands.go) turns its methods into declared contracts.Cmd values
// the CLI dispatches.
type Handler struct {
	d          channelAdmin
	sup        supervisor
	wt         worktrees
	fg         forges
	up         updater
	agents     agentStore
	st         *state.State
	defaultCmd string
	partDir    string // dir holding participants/<name>.log journals
	coord      coordinationReader // nil until wired; session list omits coordination when nil
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
func NewHandler(d channelAdmin, sup supervisor, wt worktrees, fg forges, up updater, agents agentStore, st *state.State, defaultCmd, partDir string) *Handler {
	return &Handler{d: d, sup: sup, wt: wt, fg: fg, up: up, agents: agents, st: st, defaultCmd: defaultCmd, partDir: partDir}
}

// PartDir returns the participants journal directory (used by tests/wiring).
func (h *Handler) PartDir() string { return h.partDir }

// SetCoordinationReader wires the join-state source used to enrich session list.
// nil-safe: until set, session list omits the coordination field.
func (h *Handler) SetCoordinationReader(r coordinationReader) { h.coord = r }

// Agents returns the durable agent store (used by tests/wiring).
func (h *Handler) Agents() agentStore { return h.agents }
