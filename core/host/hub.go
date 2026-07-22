package host

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	control "github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

// hub owns the live session set of a running daemon and implements
// contracts.SessionControl so a gateway can change it at runtime. The startup
// path (RunHub) and the runtime path (Dispatch) both bring a session live
// through goLive/goDead, so a session created via a slash command is wired
// exactly like one loaded at boot. The handler (behind reg) performs the
// resource work and supervisor start/stop; the hub owns only the control-socket
// acceptor + RunSession loop that the boot path used to inline.
type hub struct {
	ctx     context.Context
	st      *state.State
	sup     *supervisor.Supervisor
	gws     []Deps
	partDir string
	reg     *cli.Registry
	metrics *metrics.Registry

	// coordinator is the Model-O handoff decision point, given to every driven
	// session's RunSession. Set in serve.go's RunHub after newHub, before the
	// boot loop's goLive calls, so it is non-nil for every driver started here.
	coordinator contracts.Coordinator

	dispatchMu sync.Mutex // serializes operator commands (and their reconcile)
	mu         sync.Mutex
	live       map[string]context.CancelFunc // session name → cancel its RunSession
}

// forgetter est satisfaite par *coordinator ; elle vit hors du port
// contracts.Coordinator car forget est un détail host-interne (purge de l'état de
// join), pas une capacité exposée aux gateways.
type forgetter interface{ forget(string) }

func newHub(ctx context.Context, st *state.State, sup *supervisor.Supervisor, gws []Deps, partDir string, reg *cli.Registry, m *metrics.Registry) *hub {
	return &hub{ctx: ctx, st: st, sup: sup, gws: gws, partDir: partDir, reg: reg, metrics: m, live: map[string]context.CancelFunc{}}
}

// goLive wires a session into the running hub: it opens the control-socket
// acceptor and starts its RunSession loop. It does NOT start the supervisor —
// the boot loop and the create handler each own exactly one sup.Start. goLive is
// idempotent: a session already live is left untouched.
func (h *hub) goLive(sess state.Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.live[sess.Name]; ok {
		return
	}
	acc, err := control.Accept(control.SocketPath(sess.Name))
	if err != nil {
		fmt.Fprintf(os.Stderr, "herrscher serve: session %q: control socket: %v\n", sess.Name, err)
		return
	}
	sctx, cancel := context.WithCancel(h.ctx)
	bound := boundGateways(h.gws, effectiveKinds(h.gws, sess))
	go RunSession(sctx, sess.Name, sess.ChannelID, bound, acc, state.ParticipantsPath(h.partDir, sess.Name), h.metrics, h.coordinator,
		func(tok string) { _ = h.st.SetResumeToken(sess.Name, tok) },
		func(e state.TranscriptEntry) {
			_ = state.AppendTranscript(state.TranscriptPath(h.partDir, sess.Name), e)
		})
	h.live[sess.Name] = cancel
}

// goDead cancels a session's RunSession loop (which closes its acceptor and
// removes the socket). The supervisor was already stopped by the close handler.
func (h *hub) goDead(name string) {
	h.mu.Lock()
	cancel := h.live[name]
	delete(h.live, name)
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if f, ok := h.coordinator.(forgetter); ok {
		f.forget(name)
	}
}

// reconcile aligns the live set with persisted state: sessions present in state
// but not yet live are brought up, and live sessions no longer in state are torn
// down. It is idempotent and safe to call after every Dispatch.
func (h *hub) reconcile() {
	persisted := map[string]state.Session{}
	for _, s := range h.st.SnapshotSessions() {
		persisted[s.Name] = s
	}
	h.mu.Lock()
	liveNames := make([]string, 0, len(h.live))
	for n := range h.live {
		liveNames = append(liveNames, n)
	}
	h.mu.Unlock()
	for _, n := range liveNames {
		s, ok := persisted[n]
		if !ok || s.Archived {
			h.goDead(n) // removed, or archived in place → stop driving it and free its socket
		}
	}
	for _, s := range persisted {
		if s.Archived {
			continue // archived sessions are revived only via hub.Resume
		}
		h.goLive(s)
	}
}

// Dispatch runs an operator command and reconciles the live set so a session
// create/close takes effect immediately. It implements contracts.SessionControl.
// Commands are serialized: gateways can deliver interactions concurrently, and
// running create/close (plus the reconcile that follows) one at a time keeps the
// existence checks and the live set consistent.
func (h *hub) Dispatch(ctx context.Context, args []string) (string, error) {
	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	out, err := h.reg.Dispatch(ctx, args)
	h.reconcile()
	return out, err
}

// Create starts a session from a typed spec. It maps CreateSession into the
// flags the session-create command declares and runs it through the typed
// registry seam (no argv assembly), then reconciles so the new session is live.
// It implements contracts.SessionControl.
func (h *hub) Create(ctx context.Context, spec contracts.CreateSession) (string, error) {
	args := map[string]string{"name": spec.Name}
	setStr := func(k, v string) {
		if v != "" {
			args[k] = v
		}
	}
	setStr("project", spec.Project)
	setStr("clone", spec.Clone)
	setStr("cmd", spec.Cmd)
	setStr("backend", spec.Backend)
	setStr("agent", spec.Agent)
	setStr("extractor", spec.Extractor)
	setStr("journal", spec.Journal)
	setStr("base", spec.Base)
	setStr("parent", spec.Parent)
	if len(spec.Gateways) > 0 {
		args["gateways"] = strings.Join(spec.Gateways, ",")
	}
	if spec.TerminalOnly {
		args["terminal_only"] = "true"
	}
	if spec.Shared {
		args["shared"] = "true"
	}
	if spec.ConsolidateEvery != 0 {
		args["consolidate_every"] = strconv.Itoa(spec.ConsolidateEvery)
	}
	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	out, err := h.reg.Run(ctx, []string{"session", "create"}, contracts.Input{Args: args})
	h.reconcile()
	return out, err
}

// Close tears a session down by name. It maps to the session-close flags and
// runs through the typed registry seam, then reconciles. It implements
// contracts.SessionControl.
func (h *hub) Close(ctx context.Context, name string, force bool) (string, error) {
	args := map[string]string{"name": name}
	if force {
		args["force"] = "true"
	}
	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	out, err := h.reg.Run(ctx, []string{"session", "close"}, contracts.Input{Args: args})
	h.reconcile()
	return out, err
}

// Sessions returns a snapshot of the hub's sessions. It implements
// contracts.SessionControl.
func (h *hub) Sessions() []contracts.SessionInfo {
	sessions := h.st.SnapshotSessions()
	out := make([]contracts.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		lastTs := state.ReadTranscriptLast(state.TranscriptPath(h.partDir, s.Name))
		out = append(out, contracts.SessionInfo{
			Name:      s.Name,
			ChannelID: s.ChannelID,
			Type:      s.Type,
			Gateways:  s.BoundGateways(),
			Vendor:    s.Vendor,
			Project:   s.Project,
			Archived:  s.Archived,
			Resumable: s.ResumeToken != "",
			LastTs:    lastTs,
		})
	}
	return out
}

// scrollbackCap bounds how many transcript entries a reopened view replays.
const scrollbackCap = 200

// Scrollback returns the last recorded transcript lines for a session, mapped to
// the neutral seam type. It implements contracts.SessionControl.
func (h *hub) Scrollback(name string) []contracts.ScrollbackLine {
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, name), scrollbackCap)
	if len(entries) == 0 {
		return nil
	}
	out := make([]contracts.ScrollbackLine, 0, len(entries))
	for _, e := range entries {
		out = append(out, contracts.ScrollbackLine{Role: e.Role, Text: e.Text})
	}
	return out
}

// Resume revives an archived session: it unarchives it and brings it live (the
// backend resumes from its stored token, and the supervisor restarts the
// bridge). A live session is a no-op success. It implements
// contracts.SessionControl.
func (h *hub) Resume(name string) error {
	sess, ok := h.st.FindSession(name)
	if !ok {
		return fmt.Errorf("no session %q", name)
	}
	if sess.Archived {
		if err := h.st.SetArchived(name, false); err != nil {
			return err
		}
		sess.Archived = false
	}
	h.goLive(sess)
	_ = h.sup.Start(sess)
	return nil
}

// Interrupt cancels the named session's in-flight turn, delegating to the live
// driver via the session registry. It implements contracts.SessionControl.
func (h *hub) Interrupt(name string) bool {
	return Interrupt(name)
}

var _ contracts.SessionControl = (*hub)(nil)
