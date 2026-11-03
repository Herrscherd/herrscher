package host

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	control "github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

var sessionTeardownGrace = 5 * time.Second

type hub struct {
	st      *state.State
	sup     *supervisor.Supervisor
	gws     []Deps
	partDir string
	reg     *cli.Registry
	metrics *metrics.Registry

	coordinator    contracts.Coordinator
	eventPublisher func(session string, event contracts.Event)

	gate budgetGate

	contributedKinds map[string]bool

	instanceID string

	warnedRoles sync.Map

	dispatchMu sync.Mutex
	mu         sync.Mutex
	live       map[string]liveSession
}

type forgetter interface{ forget(string) }

type liveSession struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

func newHub(st *state.State, sup *supervisor.Supervisor, gws []Deps, partDir string, reg *cli.Registry, m *metrics.Registry) *hub {
	return &hub{st: st, sup: sup, gws: gws, partDir: partDir, reg: reg, metrics: m, live: map[string]liveSession{}}
}

func eventTap(publish func(string, contracts.Event), session string) func(contracts.Event) {
	if publish == nil {
		return nil
	}
	return func(e contracts.Event) { publish(session, e) }
}

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
	sctx, cancel := context.WithCancel(context.Background())
	bound := boundGateways(h.gws, effectiveKinds(h.gws, sess))
	done := make(chan struct{})
	h.live[sess.Name] = liveSession{cancel: cancel, done: done}
	go func() {
		defer close(done)
		runSessionIdentified(sctx, sess.Name, sess.ChannelID, bound, acc, state.ParticipantsPath(h.partDir, sess.Name), h.metrics, h.coordinator,
			sessionSink{
				Resume: func(tok string) {
					if err := h.st.SetResumeToken(sess.Name, tok); err != nil {
						fmt.Fprintf(os.Stderr, "herrscher serve: session %q: resume token not persisted, a restart will lose the conversation: %v\n", sess.Name, err)
					}
				},
				Project: func(p string) {
					if err := h.st.SetProjectPinned(sess.Name, p); err != nil {
						fmt.Fprintf(os.Stderr, "herrscher serve: session %q: memory project not persisted, the next start will guess again: %v\n", sess.Name, err)
					}
				},
				Transcript: func(e state.TranscriptEntry) {
					if err := state.AppendTranscript(state.TranscriptPath(h.partDir, sess.Name), e); err != nil {
						fmt.Fprintf(os.Stderr, "herrscher serve: session %q: transcript entry dropped: %v\n", sess.Name, err)
					}
				},
			},
			eventTap(h.eventPublisher, sess.Name),
			sessionIdentity{incarnation: sess.Incarnation, agent: sess.Agent}, h.gate)
	}()

	go serveCommandSocket(sctx, SessionCommandSocketPath(h.instanceID, sess.Name), asSession{disp: h, session: sess.Name})
}

func (h *hub) stopLive() {
	h.mu.Lock()
	lives := make([]liveSession, 0, len(h.live))
	for _, live := range h.live {
		lives = append(lives, live)
	}
	h.mu.Unlock()
	for _, live := range lives {
		live.cancel()
	}
}

func (h *hub) goDead(name string) {
	if h.sup != nil {
		logCleanup(h.sup.Stop(name))
	}
	h.mu.Lock()
	live, ok := h.live[name]
	delete(h.live, name)
	h.mu.Unlock()
	if ok {
		live.cancel()
		select {
		case <-live.done:
		case <-time.After(sessionTeardownGrace):
			fmt.Fprintf(os.Stderr, "herrscher serve: session %q: teardown still running after %s, releasing the name anyway\n", name, sessionTeardownGrace)
		}
	}
	if f, ok := h.coordinator.(forgetter); ok {
		f.forget(name)
	}
}

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
			h.goDead(n)
		}
	}
	for _, s := range persisted {
		if s.Archived {
			continue
		}
		h.goLive(s)
	}
}

func (h *hub) Dispatch(ctx context.Context, args []string) (string, error) {
	if err := h.authorize(ctx, args); err != nil {
		return "", err
	}

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

	if h.isContributedCommand(args) {
		return h.reg.Dispatch(ctx, args)
	}

	if len(args) > 0 && args[0] == "approve" {
		return h.reg.Dispatch(ctx, args)
	}

	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	out, err := h.reg.Dispatch(ctx, args)
	h.reconcile()
	return out, err
}

func (h *hub) isContributedCommand(args []string) bool {
	return len(args) > 0 && h.contributedKinds[args[0]]
}

func isSessionSeedCommand(args []string) bool {
	return len(args) >= 2 && args[0] == "session" && args[1] == "seed"
}

func (h *hub) Create(ctx context.Context, spec contracts.CreateSession) (string, error) {
	return h.create(ctx, spec, "")
}

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
	setStr("memory_project", spec.MemoryProject)
	setStr("memory_agent", spec.MemoryAgent)
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
	if spec.ProjectPinned {
		args["project_pinned"] = "true"
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

func (h *hub) Sessions() []contracts.SessionInfo {
	return sessionInfos(h.st, h.partDir)
}

func sessionInfos(st *state.State, partDir string) []contracts.SessionInfo {
	sessions := st.SnapshotSessions()
	out := make([]contracts.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		sum := state.ReadTranscriptSummary(state.TranscriptPath(partDir, s.Name))
		out = append(out, contracts.SessionInfo{
			Name:          s.Name,
			Incarnation:   s.Incarnation,
			ChannelID:     s.ChannelID,
			Type:          s.Type,
			Gateways:      s.BoundGateways(),
			Vendor:        s.Vendor,
			Model:         sessionModel(s),
			Effort:        sessionEffort(s),
			Project:       s.Project,
			Archived:      s.Archived,
			Resumable:     s.ResumeToken != "",
			LastTs:        sum.LastTs,
			ContextTokens: sum.ContextTokens,
			Dir:           sessionDir(s),
		})
	}
	return out
}

func sessionModel(s state.Session) string {
	if s.ModelID != "" {
		return s.ModelID
	}
	return invocationFlag(s.Cmd, "--model")
}

func sessionEffort(s state.Session) string {
	return invocationFlag(s.Cmd, "--effort")
}

func invocationFlag(cmd, flag string) string {
	fields := strings.Fields(cmd)
	for i, f := range fields {
		if f == flag && i+1 < len(fields) {
			return fields[i+1]
		}
		if v, ok := strings.CutPrefix(f, flag+"="); ok {
			return v
		}
	}
	return ""
}

func sessionDir(s state.Session) string {
	if s.Dir != "" {
		return s.Dir
	}
	return s.Worktree
}

const scrollbackCap = 200

func (h *hub) Scrollback(name string) []contracts.ScrollbackLine {
	return scrollbackOf(h.partDir, name)
}

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

func (h *hub) Resume(name string) error {
	sess, ok := h.st.FindSession(name)
	if !ok {
		return &NotFoundError{Kind: "session", Name: name}
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

func (h *hub) Interrupt(name string) bool {
	return Interrupt(name)
}

func (h *hub) Submit(name string, in contracts.Inbound) bool {
	return Submit(name, in)
}

func (h *hub) Pick(name, value string) bool {
	return Pick(name, value)
}

func (h *hub) Repos(ctx context.Context) ([]contracts.RepoRef, error) {
	out := workspaceRepos(h.st.WorkspaceRoot())
	repos, err := forge.New().List(ctx)
	if err != nil {
		slog.Warn("repos unavailable", "err", err)
		return out, nil
	}
	for _, r := range repos {
		out = append(out, contracts.RepoRef{Name: r.FullName, Description: r.Desc})
	}
	return out, nil
}

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
