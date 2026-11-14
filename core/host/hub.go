package host

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
	"github.com/Herrscherd/herrscher/core/usage"
)

const defaultTeardownGrace = 5 * time.Second

type hub struct {
	lang     i18n.Language
	language languageSource
	st       *state.State
	sup      *supervisor.Supervisor
	gws      []Deps
	partDir  string
	reg      *cli.Registry
	metrics  *metrics.Registry
	warnings io.Writer

	coordinator    contracts.Coordinator
	eventPublisher func(session string, event contracts.Event)

	gate budgetGate

	contributedKinds map[string]bool

	instanceID string
	runtime    runtimedir.Layout

	warnedRoles sync.Map

	dispatchMu sync.Mutex
	lifecycle  sync.Mutex
	mu         sync.Mutex
	live       map[string]liveSession

	listenedSessions map[string]struct{}
	closed           bool

	teardownGrace time.Duration
	listen        func(ctx context.Context, path string) (net.Listener, error)
	startBridge   func(sess state.Session, grace time.Duration) error
	stopBridge    func(name string) error
	stopping      func(name string) <-chan struct{}
	refused       map[string]struct{}
	seedBackend   func(context.Context, state.Session) (contracts.Backend, error)
	models        *ModelDiscovery

	approvals *approvalTable
	sessions  *sessionTable
}

type forgetter interface{ forget(string) }

type hubOption func(*hub)

func withWarnings(w io.Writer) hubOption {
	return func(h *hub) { h.warnings = w }
}

func newHub(st *state.State, sup *supervisor.Supervisor, gws []Deps, partDir string, reg *cli.Registry, m *metrics.Registry, approvals *approvalTable, sessions *sessionTable, opts ...hubOption) *hub {
	created := &hub{st: st, sup: sup, gws: gws, partDir: partDir, reg: reg, metrics: m, live: make(map[string]liveSession), teardownGrace: defaultTeardownGrace, approvals: approvals, sessions: sessions, listen: control.Listen}
	for _, opt := range opts {
		opt(created)
	}
	if sup != nil {
		sup.SetFailureSink(created.reportSessionFailure)
		created.startBridge = sup.StartWithin
		created.stopBridge = sup.Stop
		created.stopping = sup.Stopping
	}
	return created
}

func (h *hub) textLanguage() languageSource {
	if h.language != nil {
		return h.language
	}
	return fixedLanguageSource(h.lang)
}

func (h *hub) sessionLanguageSource(name string) languageSource {
	recorded := liveSessionLanguage(h.st, name)
	operator := h.textLanguage()
	return func() i18n.Language {
		if lang := recorded(); lang != 0 {
			return lang
		}
		return operator()
	}
}

func (h *hub) operatorCommands() asOperator {
	return asOperator{disp: h, lang: h.textLanguage()}
}

func (h *hub) sessionCommands(name string) asSession {
	return asSession{disp: h, session: name, lang: h.sessionLanguageSource(name)}
}

func (h *hub) reportSessionFailure(sess state.Session, stage supervisor.Stage, cause error) {
	if h.eventPublisher == nil {
		slog.Warn("session failure not published, no event sink bound yet", "session", sess.Name, "stage", stage, "err", cause)
		return
	}
	lang := sessionLanguage(sess, h.textLanguage()())
	text := LocalizeError(lang, wrapHost(cause, sessionFailureKey(stage), map[string]any{nameParam: sess.Name, detailParam: cause.Error()})).Error()
	h.eventPublisher(sess.Name, contracts.Event{T: sessionNoticeKind, Text: text})
}

const sessionNoticeKind = "notice"

func sessionFailureKey(stage supervisor.Stage) string {
	if stage == supervisor.StagePrepare {
		return "session.prepare_failed"
	}
	return "session.launch_failed"
}

func eventTap(publish func(string, contracts.Event), session string) func(contracts.Event) {
	if publish == nil {
		return nil
	}
	return func(e contracts.Event) { publish(session, e) }
}

func (h *hub) liveSink(ctx context.Context, sess state.Session) sessionSink {
	return sessionSink{
		Resume: func(tok string) {
			if err := h.st.SetResumeToken(sess.Name, tok); err != nil {
				warnLive(h.warnings, h.sessionLanguageSource(sess.Name), "warn.session_resume_token", map[string]any{nameParam: sess.Name, detailParam: err})
			}
		},
		Project: func(p string) {
			if err := h.st.SetProjectPinned(sess.Name, p); err != nil {
				warnLive(h.warnings, h.sessionLanguageSource(sess.Name), "warn.session_project_pin", map[string]any{nameParam: sess.Name, detailParam: err})
			}
		},
		Transcript: func(e state.TranscriptEntry) {
			if err := state.AppendTranscript(state.TranscriptPath(h.partDir, sess.Name), e); err != nil {
				warnLive(h.warnings, h.sessionLanguageSource(sess.Name), "warn.session_transcript", map[string]any{nameParam: sess.Name, detailParam: err})
			}
		},
		Checkpoint: func(turnID, text string) {
			h.takeCheckpoint(ctx, sess, turnID, text)
		},
		Usage: func(records []usage.Record) {
			h.recordUsage(sess, records)
		},
	}
}

func sessionLanguage(sess state.Session, operator i18n.Language) i18n.Language {
	if lang := knownSessionLanguage(sess); lang != 0 {
		return lang
	}
	return operator
}

func knownSessionLanguage(sess state.Session) i18n.Language {
	lang, err := i18n.ParseLanguage(sess.Language)
	if err != nil {
		return 0
	}
	return lang
}

func liveSessionLanguage(store *state.State, name string) func() i18n.Language {
	return func() i18n.Language {
		if store == nil {
			return 0
		}
		session, ok := store.FindSession(name)
		if !ok {
			return 0
		}
		return knownSessionLanguage(session)
	}
}

func (h *hub) Dispatch(ctx context.Context, args []string) (contracts.Result, error) {
	if err := h.authorize(ctx, args); err != nil {
		return contracts.Result{}, err
	}
	ctx = withRestarter(ctx, h)

	if isSessionSeedCommand(args) {
		runtime := oneShotSeedRuntime{
			warnings:        h.warnings,
			sessionLanguage: h.sessionLanguageSource,
			coordinator:     h.coordinator,
			publish:         h.eventPublisher,
			backend:         h.seedBackend,
			models:          h.models,
			recordUsage:     h.recordUsage,
			record: func(session string, entry state.TranscriptEntry) {
				if err := state.AppendTranscript(state.TranscriptPath(h.partDir, session), entry); err != nil {
					warnLive(h.warnings, h.sessionLanguageSource(session), "warn.session_seed_transcript", map[string]any{nameParam: session, detailParam: err})
				}
			},
		}
		return h.reg.Dispatch(withOneShotSeedRuntime(ctx, runtime), args)
	}

	if isSessionForkCommand(args) {
		return h.reg.Dispatch(withForker(ctx, h.forkSession), args)
	}

	if h.isContributedCommand(args) {
		return h.reg.Dispatch(ctx, args)
	}

	if len(args) > 0 && args[0] == nsApprove {
		return h.reg.Dispatch(ctx, args)
	}

	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	out, err := h.reg.Dispatch(ctx, args)
	h.reconcile(ctx)
	return out, err
}

func (h *hub) isContributedCommand(args []string) bool {
	return len(args) > 0 && h.contributedKinds[args[0]]
}

func isSessionSeedCommand(args []string) bool {
	return len(args) >= 2 && args[0] == nsSession && args[1] == verbSeed
}

func (h *hub) Create(ctx context.Context, spec contracts.CreateSession) (contracts.Result, error) {
	return h.create(ctx, spec, "")
}

func (h *hub) CreateCoordinated(ctx context.Context, spec contracts.CreateSession, vendor string) (contracts.Result, error) {
	return h.create(ctx, spec, vendor)
}

func (h *hub) create(ctx context.Context, spec contracts.CreateSession, vendor string) (contracts.Result, error) {
	args := map[string]string{paramName: spec.Name}
	setStr := func(key, value string) {
		if value != "" {
			args[key] = value
		}
	}
	setStr("channel_id", spec.ChannelID)
	setStr("project", spec.Project)
	setStr("clone", spec.Clone)
	setStr("cmd", spec.Cmd)
	setStr("backend", spec.Backend)
	setStr("vendor", vendor)
	setStr(paramAgent, spec.Agent)
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
		args["terminal_only"] = valueTrue
	}
	if spec.Shared {
		args["shared"] = valueTrue
	}
	if spec.ProjectPinned {
		args["project_pinned"] = valueTrue
	}
	if spec.ConsolidateEvery != 0 {
		args["consolidate_every"] = strconv.Itoa(spec.ConsolidateEvery)
	}
	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	out, err := h.reg.Run(ctx, []string{nsSession, "create"}, contracts.Input{Args: args})
	h.reconcile(ctx)
	return out, err
}

func (h *hub) Close(ctx context.Context, name string, force bool) (contracts.Result, error) {
	args := map[string]string{paramName: name}
	if force {
		args["force"] = valueTrue
	}
	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	out, err := h.reg.Run(ctx, []string{nsSession, "close"}, contracts.Input{Args: args})
	h.reconcile(ctx)
	return out, err
}

func (h *hub) Sessions() []contracts.SessionInfo {
	return sessionInfos(h.st, h.partDir, h.sessions.compaction, modelTermsFor(h.models))
}

type modelTerms struct {
	vendor string
	model  string
	window int
	rates  modelRates
}

func modelTermsFor(models *ModelDiscovery) func(state.Session) modelTerms {
	resolve := resolvedTermsFor(models)
	return func(session state.Session) modelTerms {
		terms, err := resolve(session)
		if err != nil {
			return modelTerms{vendor: session.Vendor, model: sessionModel(session)}
		}
		return terms
	}
}

func resolvedTermsFor(models *ModelDiscovery) func(state.Session) (modelTerms, error) {
	sources := models.Sources()
	policy := ResolvePolicy(envx.Getenv)
	plugins := contracts.Default.Backends()
	return func(session state.Session) (modelTerms, error) {
		modelID := sessionModel(session)
		terms := modelTerms{vendor: session.Vendor, model: modelID}
		if modelID == "" {
			return terms, nil
		}
		entry, err := lookupModel(plugins, policy, sources, modelID)
		if err != nil {
			return terms, err
		}
		terms.window = entry.ContextWindow
		terms.rates = ratesOf(entry.ModelSpec)
		return terms, nil
	}
}

type unresolvedWarnings struct {
	session string
	mu      sync.Mutex
	warned  map[string]bool
}

func (w *unresolvedWarnings) warnOnce(modelID string, err error) {
	w.mu.Lock()
	if w.warned[modelID] {
		w.mu.Unlock()
		return
	}
	w.warned[modelID] = true
	w.mu.Unlock()
	slog.Warn("session model no longer resolves, its context window and cost estimate fall back to the defaults",
		"session", w.session, "model", modelID, "err", err)
}

func liveModelTerms(store *state.State, models *ModelDiscovery, name string) func() modelTerms {
	resolve := resolvedTermsFor(models)
	warnings := &unresolvedWarnings{session: name, warned: make(map[string]bool)}
	return func() modelTerms {
		session, ok := store.FindSession(name)
		if !ok {
			return modelTerms{}
		}
		terms, err := resolve(session)
		if err != nil {
			warnings.warnOnce(sessionModel(session), err)
		}
		return terms
	}
}

func sessionInfos(st *state.State, partDir string, policy compactionPolicy, terms func(state.Session) modelTerms) []contracts.SessionInfo {
	sessions := st.SnapshotSessions()
	out := make([]contracts.SessionInfo, 0, len(sessions))
	for idx := range sessions {
		live := sessions[idx]
		sum := state.ReadTranscriptSummary(state.TranscriptPath(partDir, live.Name))
		out = append(out, contracts.SessionInfo{
			Name:               live.Name,
			Incarnation:        live.Incarnation,
			ChannelID:          live.ChannelID,
			Type:               live.Type,
			Gateways:           live.BoundGateways(),
			Vendor:             live.Vendor,
			Model:              sessionModel(live),
			Effort:             sessionEffort(live),
			Project:            live.Project,
			Archived:           live.Archived,
			Resumable:          live.ResumeToken != "",
			LastTs:             sum.LastTs,
			ContextTokens:      sum.ContextTokens,
			Dir:                sessionDir(live),
			AutoCompactPercent: policy.percentFor(live),
			ContextWindow:      policy.limits.LimitFor(live.Vendor, terms(live).window),
			Language:           knownSessionLanguage(live),
		})
	}
	return out
}

func sessionModel(session state.Session) string {
	if session.ModelID != "" {
		return session.ModelID
	}
	return invocationFlag(session.Cmd, "--model")
}

func sessionEffort(s state.Session) string {
	return invocationFlag(s.Cmd, "--effort")
}

func invocationFlag(cmd, flag string) string {
	fields := strings.Fields(cmd)
	for index, field := range fields {
		if field == flag && index+1 < len(fields) {
			return fields[index+1]
		}
		if v, ok := strings.CutPrefix(field, flag+"="); ok {
			return v
		}
	}
	return ""
}

func sessionDir(session state.Session) string {
	if session.Dir != "" {
		return session.Dir
	}
	return session.Worktree
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

func (h *hub) RestartWithResume(name, token string) error {
	sess, ok := h.st.FindSession(name)
	if !ok {
		return &NotFoundError{Kind: kindSession, Name: name}
	}
	h.goDead(name)
	sess.ResumeToken = token
	return h.goLiveStarted(context.Background(), sess, func() error {
		return h.st.SetResumeToken(name, token)
	})
}

func (h *hub) Resume(name string) error {
	sess, ok := h.st.FindSession(name)
	if !ok {
		return &NotFoundError{Kind: kindSession, Name: name}
	}
	live := sess
	live.Archived = false
	return h.goLiveStarted(context.Background(), live, func() error {
		if !sess.Archived {
			return nil
		}
		return h.st.SetArchived(name, false)
	})
}

func (h *hub) Interrupt(name string) bool {
	return h.sessions.Interrupt(name)
}

func (h *hub) Submit(name string, in contracts.Inbound) bool {
	return h.sessions.Submit(context.Background(), name, in)
}

func (h *hub) Pick(name, value string) bool {
	if answerApprovalPick(h.approvals, h.sessions, name, value) {
		return true
	}
	return h.sessions.pick(name, value)
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
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(workspace, entry.Name(), ".git")); err != nil {
			continue
		}
		out = append(out, contracts.RepoRef{Name: entry.Name(), Description: "workspace checkout", Local: true})
	}
	return out
}

var _ contracts.SessionControl = (*hub)(nil)
