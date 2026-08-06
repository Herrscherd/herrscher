package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	control "github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/forge"
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
	// eventPublisher is the daemon event-socket tap. Every driven session's
	// turn driver fans its events here, as does the request-scoped one-shot
	// seed path, so an attached reader sees the same live stream a gateway
	// does. It is bound once at boot before the command socket starts.
	eventPublisher func(session string, event contracts.Event)

	// gate decides, after each completed turn, whether a session must pause on a
	// budget cap. nil (the operator CLI never sets it) preserves the
	// noBudgetGate{} default RunSession/newSessionDriver falls back to.
	gate budgetGate

	dispatchMu sync.Mutex // serializes operator commands (and their reconcile)
	mu         sync.Mutex
	live       map[string]liveSession // session name → owned RunSession lifetime
}

// forgetter est satisfaite par *coordinator ; elle vit hors du port
// contracts.Coordinator car forget est un détail host-interne (purge de l'état de
// join), pas une capacité exposée aux gateways.
type forgetter interface{ forget(string) }

type liveSession struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

func newHub(ctx context.Context, st *state.State, sup *supervisor.Supervisor, gws []Deps, partDir string, reg *cli.Registry, m *metrics.Registry) *hub {
	return &hub{ctx: ctx, st: st, sup: sup, gws: gws, partDir: partDir, reg: reg, metrics: m, live: map[string]liveSession{}}
}

// eventTap binds a session name to the daemon's event publisher so the turn
// driver, which knows only its own events, can hand them over already addressed.
// A nil publisher (the operator CLI, tests) yields a nil tap rather than a
// closure that calls nothing, so the driver's own nil check stays the one place
// the absence is decided.
func eventTap(publish func(string, contracts.Event), session string) func(contracts.Event) {
	if publish == nil {
		return nil
	}
	return func(e contracts.Event) { publish(session, e) }
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
	done := make(chan struct{})
	h.live[sess.Name] = liveSession{cancel: cancel, done: done}
	go func() {
		defer close(done)
		runSessionIdentified(sctx, sess.Name, sess.ChannelID, bound, acc, state.ParticipantsPath(h.partDir, sess.Name), h.metrics, h.coordinator,
			// Neither callback can return an error to its caller, so the only thing
			// left to do with a failure is say it. Both are worth saying: a lost
			// resume token means the next restart silently starts the conversation
			// over, and a lost transcript entry means the session log quietly has a
			// hole in it. Failing here is a full disk or a permission problem — rare,
			// and exactly the kind of thing nobody finds without a line in the log.
			func(tok string) {
				if err := h.st.SetResumeToken(sess.Name, tok); err != nil {
					fmt.Fprintf(os.Stderr, "herrscher serve: session %q: resume token not persisted, a restart will lose the conversation: %v\n", sess.Name, err)
				}
			},
			func(e state.TranscriptEntry) {
				if err := state.AppendTranscript(state.TranscriptPath(h.partDir, sess.Name), e); err != nil {
					fmt.Fprintf(os.Stderr, "herrscher serve: session %q: transcript entry dropped: %v\n", sess.Name, err)
				}
			},
			eventTap(h.eventPublisher, sess.Name),
			sessionIdentity{incarnation: sess.Incarnation, agent: sess.Agent}, h.gate)
	}()
}

// goDead stops the session's supervised bridge, cancels its RunSession loop
// (which closes the acceptor and removes the socket), and waits for teardown
// before releasing the reusable session name.
//
// Stopping the bridge here rather than trusting the close handler to have done
// it is what keeps a removed session from leaving a process behind: the
// supervisor's restart loop holds its session by value and respawns forever, so
// a bridge outliving its socket dials a path that no longer exists and crashes
// on a loop until the daemon is restarted. Stop is idempotent, so the handlers
// that already stop the bridge lose nothing by us stopping it again.
func (h *hub) goDead(name string) {
	if h.sup != nil { // a hub built without one (tests of the teardown seam alone)
		_ = h.sup.Stop(name)
	}
	h.mu.Lock()
	live, ok := h.live[name]
	if ok {
		live.cancel()
		<-live.done
		delete(h.live, name)
	}
	h.mu.Unlock()
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
// Mutating commands are serialized: gateways can deliver interactions
// concurrently, and running create/close (plus the reconcile that follows) one
// at a time keeps the existence checks and the live set consistent.
//
// A session seed is deliberately outside dispatchMu. Its completed reply can
// coordinate back into this hub (Delegate → Create, for example), and holding
// the non-reentrant mutation lock across that turn would deadlock. Nested typed
// mutations still acquire dispatchMu themselves and perform their own reconcile.
func (h *hub) Dispatch(ctx context.Context, args []string) (string, error) {
	// A seed turn may synchronously emit a coordination trailer. Delegate then
	// re-enters this hub through Create, which owns dispatchMu for its actual
	// state mutation. Do not hold the same non-reentrant mutex around the outer
	// model turn or Delegate deadlocks before it can create the worker. Seed
	// itself does not mutate the live set; any coordination operation performs
	// its own locked mutation and reconcile. The one-shot seed runtime carries
	// the live coordinator (plus event-publish and transcript-record taps) to the
	// seed runner via context.
	if isSessionSeedCommand(args) {
		runtime := oneShotSeedRuntime{
			coordinator: h.coordinator,
			publish:     h.eventPublisher,
			record: func(session string, entry state.TranscriptEntry) {
				if err := state.AppendTranscript(state.TranscriptPath(h.partDir, session), entry); err != nil {
					fmt.Fprintf(os.Stderr, "herrscher serve: session %q: seed transcript entry dropped: %v\n", session, err)
				}
			},
		}
		return h.reg.Dispatch(withOneShotSeedRuntime(ctx, runtime), args)
	}

	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	out, err := h.reg.Dispatch(ctx, args)
	h.reconcile()
	return out, err
}

func isSessionSeedCommand(args []string) bool {
	return len(args) >= 2 && args[0] == "session" && args[1] == "seed"
}

// Create starts a session from a typed spec. It maps CreateSession into the
// flags the session-create command declares and runs it through the typed
// registry seam (no argv assembly), then reconciles so the new session is live.
// It implements contracts.SessionControl.
func (h *hub) Create(ctx context.Context, spec contracts.CreateSession) (string, error) {
	return h.create(ctx, spec, "")
}

// CreateCoordinated is the coordinator's internal creation seam. Vendor is kept
// separate from the public contracts.CreateSession so ordinary SessionControl
// callers retain their existing API while delegated workers can inherit a lead's
// backend target.
func (h *hub) CreateCoordinated(ctx context.Context, spec contracts.CreateSession, vendor string) (string, error) {
	return h.create(ctx, spec, vendor)
}

func (h *hub) create(ctx context.Context, spec contracts.CreateSession, vendor string) (string, error) {
	args := map[string]string{"name": spec.Name}
	setStr := func(k, v string) {
		if v != "" {
			args[k] = v
		}
	}
	setStr("channel_id", spec.ChannelID)
	setStr("project", spec.Project)
	setStr("clone", spec.Clone)
	setStr("cmd", spec.Cmd)
	setStr("backend", spec.Backend)
	setStr("vendor", vendor)
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
	return sessionInfos(h.st, h.partDir)
}

// sessionInfos renders the persisted sessions as the frontend-facing view. It is
// shared with the `session info` command so an attached frontend, which reaches
// the daemon over the command socket instead of holding this hub, sees exactly
// what an in-process one does rather than a second rendering that can drift.
func sessionInfos(st *state.State, partDir string) []contracts.SessionInfo {
	sessions := st.SnapshotSessions()
	out := make([]contracts.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		lastTs := state.ReadTranscriptLast(state.TranscriptPath(partDir, s.Name))
		out = append(out, contracts.SessionInfo{
			Name:        s.Name,
			Incarnation: s.Incarnation,
			ChannelID:   s.ChannelID,
			Type:        s.Type,
			Gateways:    s.BoundGateways(),
			Vendor:      s.Vendor,
			Project:     s.Project,
			Archived:    s.Archived,
			Resumable:   s.ResumeToken != "",
			LastTs:      lastTs,
			Dir:         sessionDir(s),
		})
	}
	return out
}

// sessionDir resolves a session's run directory the way the supervisor does:
// the explicit bridge Dir, else the worktree, else empty (inherit launcher cwd).
// A gateway uses it as the base for @ file-mention completion.
func sessionDir(s state.Session) string {
	if s.Dir != "" {
		return s.Dir
	}
	return s.Worktree
}

// scrollbackCap bounds how many transcript entries a reopened view replays.
const scrollbackCap = 200

// Scrollback returns the last recorded transcript lines for a session, mapped to
// the neutral seam type. It implements contracts.SessionControl.
func (h *hub) Scrollback(name string) []contracts.ScrollbackLine {
	return scrollbackOf(h.partDir, name)
}

// scrollbackOf is Scrollback without the hub, shared with the `session
// scrollback` command so an attached frontend repaints from the same lines an
// in-process one does.
func scrollbackOf(partDir, name string) []contracts.ScrollbackLine {
	entries := state.ReadTranscript(state.TranscriptPath(partDir, name), scrollbackCap)
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
	h.sup.Start(sess)
	return nil
}

// Interrupt cancels the named session's in-flight turn, delegating to the live
// driver via the session registry. It implements contracts.SessionControl.
func (h *hub) Interrupt(name string) bool {
	return Interrupt(name)
}

// Submit injects one inbound message into the named session's turn queue,
// delegating to the live driver via the session registry. It implements
// contracts.SessionControl.
func (h *hub) Submit(name string, in contracts.Inbound) bool {
	return Submit(name, in)
}

// Pick answers the named session's pending choice, delegating to the live driver
// via the session registry. It implements contracts.SessionControl.
func (h *hub) Pick(name, value string) bool {
	return Pick(name, value)
}

// Repos lists the targets a session can be created on: the workspace
// sub-directories already checked out, plus whatever the configured forge can
// clone. Forge failures (no gh/glab, no auth) are not fatal — the local list is
// still useful on its own. It implements contracts.SessionControl.
func (h *hub) Repos(ctx context.Context) ([]contracts.RepoRef, error) {
	out := workspaceRepos(h.st.WorkspaceRoot())
	repos, err := forge.New().List(ctx)
	if err != nil {
		return out, nil
	}
	for _, r := range repos {
		out = append(out, contracts.RepoRef{Name: r.FullName, Description: r.Desc})
	}
	return out, nil
}

// workspaceRepos lists the immediate sub-directories of the workspace that are
// git checkouts, as local RepoRefs. A missing or unreadable workspace yields
// none rather than an error: the forge list alone is still a usable answer.
func workspaceRepos(workspace string) []contracts.RepoRef {
	if workspace == "" {
		return nil
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return nil
	}
	var out []contracts.RepoRef
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(workspace, e.Name(), ".git")); err != nil {
			continue
		}
		out = append(out, contracts.RepoRef{Name: e.Name(), Description: "workspace checkout", Local: true})
	}
	return out
}

var _ contracts.SessionControl = (*hub)(nil)
