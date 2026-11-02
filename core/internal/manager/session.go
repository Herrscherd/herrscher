package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const cloneTimeout = 10 * time.Minute

const maxSessions = 64

type sessionJSON struct {
	Id           string            `json:"id"`
	Incarnation  string            `json:"incarnation"`
	Name         string            `json:"name"`
	Agent        string            `json:"agent"`
	Project      string            `json:"project"`
	Status       string            `json:"status"`
	Worktree     string            `json:"worktree"`
	Gateways     []string          `json:"gateways"`
	Parent       string            `json:"parent,omitempty"`
	Coordination *coordinationJSON `json:"coordination,omitempty"`
	Resumable    bool              `json:"resumable"`
	Usage        *usageJSON        `json:"usage,omitempty"`
	Task         string            `json:"task,omitempty"`
}

type usageJSON struct {
	Cost           float64 `json:"cost,omitempty"`
	TokensIn       int     `json:"tokens_in,omitempty"`
	TokensOut      int     `json:"tokens_out,omitempty"`
	CacheRead      int     `json:"cache_read,omitempty"`
	CacheCreate    int     `json:"cache_create,omitempty"`
	Turns          int     `json:"turns,omitempty"`
	StartedAt      string  `json:"started_at,omitempty"`
	CostCap        float64 `json:"cost_cap,omitempty"`
	TokenCap       uint64  `json:"token_cap,omitempty"`
	CohortCostCap  float64 `json:"cohort_cost_cap,omitempty"`
	CohortTokenCap uint64  `json:"cohort_token_cap,omitempty"`
	PausedReason   string  `json:"paused_reason,omitempty"`
}

func cohortTotals(target state.Session, all []state.Session,
	usageFor func(state.Session) sessionUsage,
) sessionUsage {
	byName := make(map[string]state.Session, len(all))
	children := make(map[string][]string, len(all))
	for i := range all {
		s := all[i]
		byName[s.Name] = s
		if s.Parent != "" && s.Parent != s.Name {
			children[s.Parent] = append(children[s.Parent], s.Name)
		}
	}
	root := target
	seen := map[string]bool{root.Name: true}
	for root.Parent != "" && root.Parent != root.Name {
		p, ok := byName[root.Parent]
		if !ok || seen[p.Name] {
			break
		}
		seen[p.Name] = true
		root = p
	}
	var totalCost float64
	var totalTokens uint64
	visited := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		s, ok := byName[name]
		if !ok {
			return
		}
		u := usageFor(s)
		totalCost += u.cost
		totalTokens += u.tokens
		for _, ch := range children[name] {
			walk(ch)
		}
	}
	walk(root.Name)
	return sessionUsage{cost: totalCost, tokens: totalTokens}
}

type transcriptUsage struct {
	usage *usageJSON
	task  string
}

func aggregateUsage(entries []state.TranscriptEntry) transcriptUsage {
	if len(entries) == 0 {
		return transcriptUsage{}
	}
	u := &usageJSON{StartedAt: entries[0].Ts}
	var task string
	for _, e := range entries {
		switch e.Role {
		case "assistant":
			u.Turns++
			u.Cost += e.Cost
			u.TokensIn += e.TokensIn
			u.TokensOut += e.TokensOut
			u.CacheRead += e.CacheRead
			u.CacheCreate += e.CacheCreate
		case "user":
			task = e.Text
		}
	}
	return transcriptUsage{usage: u, task: task}
}

func (h *Handler) attachUsage(row *sessionJSON) {
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, row.Name), 0)
	if agg := aggregateUsage(entries); agg.usage != nil {
		row.Usage = agg.usage
		if agg.task != "" {
			row.Task = agg.task
		}
	}
}

type budgetPatch struct {
	costCap        *float64
	tokenCap       *uint64
	cohortCostCap  *float64
	cohortTokenCap *uint64
}

func fptr(f float64) *float64 { return &f }
func uptr(u uint64) *uint64   { return &u }

func applyBudget(s state.Session, p budgetPatch, curCost float64, curTokens uint64) state.Session {
	if p.costCap != nil {
		s.CostCap = *p.costCap
	}
	if p.tokenCap != nil {
		s.TokenCap = *p.tokenCap
	}
	if p.cohortCostCap != nil {
		s.CohortCostCap = *p.cohortCostCap
	}
	if p.cohortTokenCap != nil {
		s.CohortTokenCap = *p.cohortTokenCap
	}
	if s.PausedReason == "cost" && (s.CostCap == 0 || curCost < s.CostCap) {
		s.PausedReason = ""
	}
	if s.PausedReason == "tokens" && (s.TokenCap == 0 || curTokens < s.TokenCap) {
		s.PausedReason = ""
	}
	return s
}

func (h *Handler) sessionSetBudgetRun(ctx context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	sess, ok := h.st.FindSession(name)
	if !ok {
		return "", &SessionNotFoundError{Name: name}
	}
	patch, err := parseBudgetPatch(in)
	if err != nil {
		return "", err
	}
	targets := []state.Session{sess}
	if scope, _ := in.Lookup("scope"); scope == "cohort" {
		targets = cohortMembers(sess, h.st.SnapshotSessions())
	}
	for i := range targets {
		if applyErr := h.applyBudgetTo(targets[i], patch); applyErr != nil {
			return "", applyErr
		}
	}
	return fmt.Sprintf("✅ budget updated for %d session(s)", len(targets)), nil
}

func parseBudgetPatch(in contracts.Input) (budgetPatch, error) {
	var patch budgetPatch
	if err := setFloatCap(in, "cost_cap", &patch.costCap); err != nil {
		return budgetPatch{}, err
	}
	if err := setCountCap(in, "token_cap", &patch.tokenCap); err != nil {
		return budgetPatch{}, err
	}
	if err := setFloatCap(in, "cohort_cost_cap", &patch.cohortCostCap); err != nil {
		return budgetPatch{}, err
	}
	if err := setCountCap(in, "cohort_token_cap", &patch.cohortTokenCap); err != nil {
		return budgetPatch{}, err
	}
	return patch, nil
}

func setFloatCap(in contracts.Input, key string, dst **float64) error {
	v, ok := in.Lookup(key)
	if !ok || v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return &InvalidValueError{Field: key, Value: v}
	}
	*dst = fptr(f)
	return nil
}

func setCountCap(in contracts.Input, key string, dst **uint64) error {
	v, ok := in.Lookup(key)
	if !ok || v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return &InvalidValueError{Field: key, Value: v}
	}
	*dst = uptr(nonNegativeCount(n))
	return nil
}

func (h *Handler) applyBudgetTo(target state.Session, patch budgetPatch) error {
	usageFor := h.usageOf
	usage := usageFor(target)
	cohort := cohortTotals(target, h.st.SnapshotSessions(), usageFor)
	updated := applyBudget(target, patch, usage.cost, usage.tokens)
	releaseCohortPause(&updated, cohort)
	if err := h.st.SetBudget(updated.Name, updated.CostCap, updated.TokenCap, updated.CohortCostCap, updated.CohortTokenCap, updated.PausedReason); err != nil {
		return fmt.Errorf("persist budget for %q: %w", updated.Name, err)
	}
	return nil
}

func (h *Handler) usageOf(s state.Session) sessionUsage {
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, s.Name), 0)
	u := aggregateUsage(entries).usage
	if u == nil {
		return sessionUsage{}
	}
	return sessionUsage{cost: u.Cost, tokens: nonNegativeCount(u.TokensIn + u.TokensOut)}
}

func releaseCohortPause(updated *state.Session, cohort sessionUsage) {
	if updated.PausedReason == "cohort_cost" && (updated.CohortCostCap == 0 || cohort.cost < updated.CohortCostCap) {
		updated.PausedReason = ""
	}
	if updated.PausedReason == "cohort_tokens" && (updated.CohortTokenCap == 0 || cohort.tokens < updated.CohortTokenCap) {
		updated.PausedReason = ""
	}
}

func (h *Handler) stampBudget(row *sessionJSON, s state.Session) {
	if s.CostCap == 0 && s.TokenCap == 0 && s.CohortCostCap == 0 &&
		s.CohortTokenCap == 0 && s.PausedReason == "" {
		return
	}
	if row.Usage == nil {
		row.Usage = &usageJSON{}
	}
	row.Usage.CostCap = s.CostCap
	row.Usage.TokenCap = s.TokenCap
	row.Usage.CohortCostCap = s.CohortCostCap
	row.Usage.CohortTokenCap = s.CohortTokenCap
	row.Usage.PausedReason = s.PausedReason
}

type coordinationJSON struct {
	Role     string `json:"role"`
	Lead     string `json:"lead"`
	Reported int    `json:"reported"`
	Expected int    `json:"expected"`
	Complete bool   `json:"complete"`
}

func sessionJSONRow(s state.Session) sessionJSON {
	gateways := s.BoundGateways()
	if gateways == nil {
		gateways = []string{}
	}
	status := "running"
	if s.PausedReason != "" {
		status = "paused"
	}
	if s.Archived {
		status = "archived"
	}
	return sessionJSON{
		Id: s.Name, Incarnation: s.Incarnation, Name: s.Name, Agent: s.Agent, Project: s.Project, Status: status,
		Worktree: s.Worktree, Gateways: gateways, Parent: s.Parent,
		Resumable: s.ResumeToken != "",
	}
}

func sessionBanner(repo, name, worktree, branch, cmd, modelID string, shared bool) string {
	b := fmt.Sprintf("🚀 Session **%s** ready.\n", name)
	if repo == "" {
		b += "• Project: **(cwd)**\n"
	} else {
		b += fmt.Sprintf("• Project: **%s** (%#q)\n", filepath.Base(repo), repo)
	}
	switch {
	case worktree != "":
		b += "• Mode: isolated worktree\n"
		b += fmt.Sprintf("• Worktree: %#q\n", worktree)
		b += fmt.Sprintf("• Branch: %#q\n", branch)
	case shared:
		b += "• Mode: shared (main checkout)\n"
		b += "• Branch: none (runs on current branch)\n"
	default:
		b += "• Mode: shared (not a git repo)\n"
	}
	b += fmt.Sprintf("• Command: %#q", cmd)
	if modelID != "" {
		b += fmt.Sprintf("\n• Model: %#q", modelID)
	}
	return b
}

type budgetCaps struct {
	cost, cohortCost     float64
	tokens, cohortTokens uint64
}

type runtimeChoice struct {
	cmd      string
	explicit bool
	backend  string
	agent    string
	vendor   string
	model    string
}

type sessionOptions struct {
	parent, memProject, memAgent string
	projectPinned                bool
	extractor, journal           string
	consolidateEvery             int
	gateways                     []string
	caps                         budgetCaps
}

type createRequest struct {
	name, base string
	home       state.HomeRef
	admin      channelAdmin
	adopted    string
	runtime    runtimeChoice
	options    sessionOptions
	hostName   string
	mode       string
	shared     bool
}

type workspace struct {
	path    string
	reused  bool
	scratch bool
}

type channelSlot struct {
	id    string
	owned bool
	typ   string
}

type placement struct {
	wt      Worktrees
	repo    string
	project string
	space   workspace
}

func (h *Handler) sessionCreateRun(ctx context.Context, in contracts.Input) (string, error) {
	req, err := h.parseCreateRequest(ctx, in)
	if err != nil {
		return "", err
	}
	if readyErr := h.checkHostReady(req.hostName); readyErr != nil {
		return "", readyErr
	}
	place, err := h.placeSession(ctx, in, &req)
	if err != nil {
		return "", err
	}
	approvalsNote := h.approvalsNote(req, place.space)
	runDir := place.space.path
	if runDir == "" {
		runDir = place.repo
	}
	sess := newSessionRecord(req, place.space, place.project, runDir)
	slot, err := openChannel(ctx, req, h.st.QualifiedName(req.name))
	if err != nil {
		if _, unsupported := errors.AsType[*UnsupportedHomeTypeError](err); unsupported {
			return "", err
		}
		h.rollbackPlacement(place, req.name)
		return "", err
	}
	sess.ChannelID, sess.Owned, sess.Type = slot.id, slot.owned, slot.typ
	sess.Approvals = req.mode
	if err := h.st.AddSession(sess); err != nil {
		h.rollbackPlacement(place, req.name)
		if note := h.tidyChannel(ctx, sess); note != "" {
			h.log.Warn("channel rollback", "session", req.name, "note", note)
		}
		return "", fmt.Errorf("persist: %w", err)
	}
	h.sup.Start(sess)
	banner := sessionBanner(place.repo, req.name, place.space.path, place.wt.Branch(req.name), req.runtime.cmd, req.runtime.model, req.shared)
	h.warnIfFailed(req.admin.Send(ctx, sess.ChannelID, banner), "send banner for "+req.name)
	return fmt.Sprintf("✅ Running on %s.\n\n%s%s", req.admin.ChannelRef(sess.ChannelID), banner, approvalsNote), nil
}

func (h *Handler) placeSession(ctx context.Context, in contracts.Input, req *createRequest) (placement, error) {
	wt, wtRoot, err := h.worktreesOn(req.hostName)
	if err != nil {
		return placement{}, err
	}
	project, err := h.resolveProject(ctx, in, req.hostName, wtRoot)
	if err != nil {
		return placement{}, err
	}
	repo := repoFor(wtRoot, project)
	if h.hs != nil {
		if projectErr := h.hs.EnsureProject(ctx, req.hostName, project, repo); projectErr != nil {
			return placement{}, projectErr
		}
	}
	space, err := ensureWorkspace(wt, repo, *req)
	if err != nil {
		return placement{}, err
	}
	if req.runtime.agent != "" {
		if provisionErr := h.provisionAgent(ctx, req, wt, repo, &space); provisionErr != nil {
			return placement{}, provisionErr
		}
	}
	return placement{wt: wt, repo: repo, project: project, space: space}, nil
}

func (h *Handler) rollbackPlacement(place placement, name string) {
	h.rollbackWorkspace(place.wt, place.repo, name, place.space)
}

func (h *Handler) parseCreateRequest(ctx context.Context, in contracts.Input) (createRequest, error) {
	raw, ok := in.Lookup(nameParam)
	if !ok {
		return createRequest{}, ErrMissingName
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		return createRequest{}, &InvalidValueError{Field: nameParam, Value: raw, Hint: "use letters, digits, - or _ (max 64, no /, spaces or ..)"}
	}
	if _, exists := h.st.FindSession(name); exists {
		return createRequest{}, &SessionExistsError{Name: name}
	}
	if n := len(h.st.SnapshotSessions()); n >= maxSessions {
		return createRequest{}, fmt.Errorf("%w (%d): close a session before creating another", ErrSessionLimit, maxSessions)
	}
	terminalOnly := in.Bool("terminal_only")
	adopted, _ := in.Lookup("channel_id")
	home, admin, err := h.chooseHome(ctx, in, adopted, terminalOnly)
	if err != nil {
		return createRequest{}, err
	}
	runtime, err := h.chooseRuntime(in)
	if err != nil {
		return createRequest{}, err
	}
	options, err := h.parseSessionOptions(in, terminalOnly)
	if err != nil {
		return createRequest{}, err
	}
	mode := strings.TrimSpace(in.Get("approvals"))
	if err := validateApprovalsMode(mode); err != nil {
		return createRequest{}, err
	}
	base, _ := in.Lookup("base")
	return createRequest{
		name:     name,
		base:     base,
		home:     home,
		admin:    admin,
		adopted:  adopted,
		runtime:  runtime,
		options:  options,
		hostName: h.resolveHost(in, runtime.agent),
		mode:     mode,
		shared:   in.Bool("shared"),
	}, nil
}

func (h *Handler) chooseHome(ctx context.Context, in contracts.Input, adopted string, terminalOnly bool) (state.HomeRef, channelAdmin, error) {
	home, admin := h.st.Home, h.d
	if terminalOnly && h.td != nil {
		home = state.HomeRef{ID: terminalPlace, Type: terminalPlace}
		admin = h.td
	}
	under, ok := in.Lookup("under")
	if !ok || under == "" {
		if home.ID == "" && adopted == "" {
			return state.HomeRef{}, nil, ErrNoHome
		}
		return home, admin, nil
	}
	if adopted != "" {
		return state.HomeRef{}, nil, ErrConflictingChannel
	}
	if terminalOnly {
		return state.HomeRef{}, nil, ErrConflictingPlacement
	}
	kind, err := admin.Kind(ctx, under)
	if err != nil {
		return state.HomeRef{}, nil, fmt.Errorf("inspect channel: %w", err)
	}
	if kind != categoryKind && kind != forumKind {
		return state.HomeRef{}, nil, &ChannelKindError{Channel: under, Kind: kind, Need: "under must be a category or forum (to run in an existing conversation, pass channel_id)"}
	}
	return state.HomeRef{ID: under, Type: kind}, admin, nil
}

func (h *Handler) chooseRuntime(in contracts.Input) (runtimeChoice, error) {
	var r runtimeChoice
	r.cmd = h.defaultCmd
	if c, ok := in.Lookup("cmd"); ok && c != "" {
		r.cmd = c
		r.explicit = true
	}
	r.backend, _ = in.Lookup("backend")
	if r.backend == "" {
		r.backend = "stream"
	}
	r.agent, _ = in.Lookup("agent")
	r.vendor, _ = in.Lookup("vendor")
	model, _ := in.Lookup("model")
	r.model = h.resolveModel(model, r.vendor, r.explicit)
	if err := h.checkModel(r.vendor, r.model); err != nil {
		return runtimeChoice{}, err
	}
	if err := h.checkSpawnSource(r.explicit, r.model); err != nil {
		return runtimeChoice{}, err
	}
	return r, nil
}

func (h *Handler) parseSessionOptions(in contracts.Input, terminalOnly bool) (sessionOptions, error) {
	var o sessionOptions
	o.parent, _ = in.Lookup("parent")
	o.memProject, _ = in.Lookup("memory_project")
	if o.memProject != "" && !projectRe.MatchString(o.memProject) {
		return sessionOptions{}, &InvalidValueError{Field: "memory_project", Value: o.memProject, Hint: "use a single name (no /, spaces, or ..)"}
	}
	o.memAgent, _ = in.Lookup("memory_agent")
	o.projectPinned = in.Bool("project_pinned")
	o.extractor, _ = in.Lookup("extractor")
	o.journal, _ = in.Lookup("journal")
	every, err := optionalNonNegative(in, "consolidate_every")
	if err != nil {
		return sessionOptions{}, err
	}
	o.consolidateEvery = every
	gwList, _ := in.Lookup("gateways")
	o.gateways = ParseGateways(gwList, terminalOnly, h.defaultGateways)
	caps, err := parseBudgetCaps(in)
	if err != nil {
		return sessionOptions{}, err
	}
	o.caps = caps
	return o, nil
}

func parseBudgetCaps(in contracts.Input) (budgetCaps, error) {
	var caps budgetCaps
	var err error
	if caps.cost, err = optionalFloat(in, "cost_cap"); err != nil {
		return budgetCaps{}, err
	}
	tokens, err := optionalNonNegative(in, "token_cap")
	if err != nil {
		return budgetCaps{}, err
	}
	caps.tokens = nonNegativeCount(tokens)
	if caps.cohortCost, err = optionalFloat(in, "cohort_cost_cap"); err != nil {
		return budgetCaps{}, err
	}
	cohortTokens, err := optionalNonNegative(in, "cohort_token_cap")
	if err != nil {
		return budgetCaps{}, err
	}
	caps.cohortTokens = nonNegativeCount(cohortTokens)
	return caps, nil
}

func optionalFloat(in contracts.Input, key string) (float64, error) {
	v, ok := in.Lookup(key)
	if !ok || v == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, &InvalidValueError{Field: key, Value: v}
	}
	return f, nil
}

func optionalNonNegative(in contracts.Input, key string) (int, error) {
	v, ok := in.Lookup(key)
	if !ok || v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, &InvalidValueError{Field: key, Value: v, Hint: nonNegativeHint}
	}
	return n, nil
}

func (h *Handler) resolveHost(in contracts.Input, agentName string) string {
	hostName, _ := in.Lookup("host")
	hostName = strings.TrimSpace(hostName)
	if hostName == "" && agentName != "" {
		if a, ok := h.agents.Get(agentName); ok {
			hostName = a.Host
		}
	}
	if hostName == state.LocalHost {
		return ""
	}
	return hostName
}

func validateApprovalsMode(mode string) error {
	switch approval.Mode(mode) {
	case "", approval.ModeAsk, approval.ModeBypass, approval.ModeStrict:
		return nil
	}
	return &UnknownApprovalModeError{Mode: mode}
}

func (h *Handler) checkHostReady(hostName string) error {
	if hostName == "" || h.hs == nil {
		return nil
	}
	return h.hs.Ready(hostName)
}

func (h *Handler) resolveProject(ctx context.Context, in contracts.Input, hostName, wtRoot string) (string, error) {
	project, _ := in.Lookup("project")
	if wtRoot == "" {
		return project, nil
	}
	if spec, ok := in.Lookup("clone"); ok && spec != "" {
		if hostName != "" {
			return "", fmt.Errorf("%w %q: create the project locally first, then create the session with --host", ErrCloneUnsupported, hostName)
		}
		cctx, cancel := context.WithTimeout(ctx, cloneTimeout)
		dir, err := h.fg.Clone(cctx, spec, wtRoot)
		cancel()
		if err != nil {
			return "", fmt.Errorf("clone: %w", err)
		}
		project = filepath.Base(dir)
	}
	if project != "" && !projectRe.MatchString(project) {
		return "", &InvalidValueError{Field: "project", Value: project, Hint: "use a single name (no /, spaces, or ..)"}
	}
	return project, nil
}

func ensureWorkspace(wt Worktrees, repo string, req createRequest) (workspace, error) {
	if req.shared {
		return workspace{}, nil
	}
	reused := wt.PreExisting(repo, req.name)
	path, err := wt.Create(repo, req.name, req.base)
	if err != nil {
		return workspace{}, fmt.Errorf("worktree: %w", err)
	}
	return workspace{path: path, reused: reused}, nil
}

func (h *Handler) provisionAgent(ctx context.Context, req *createRequest, wt Worktrees, repo string, space *workspace) error {
	agentName := req.runtime.agent
	if req.shared {
		return &SharedAgentError{Agent: agentName}
	}
	if space.path == "" {
		if _, statErr := os.Stat(wt.ScratchPath(repo, req.name)); statErr == nil {
			space.reused = true
		}
		path, err := wt.Scratch(repo, req.name)
		if err != nil {
			return fmt.Errorf("provision agent %q: %w", agentName, err)
		}
		space.path, space.scratch = path, true
	}
	a, found := h.agents.Get(agentName)
	if !found {
		h.rollbackWorkspace(wt, repo, req.name, *space)
		return &UnknownAgentError{Name: agentName}
	}
	if req.runtime.vendor == "" {
		req.runtime.vendor = a.Backend
	}
	if !req.runtime.explicit && a.Cmd != "" {
		req.runtime.cmd = a.Cmd
	}
	if err := h.materializeOn(ctx, req.hostName, a, space.path, req.mode != string(approval.ModeBypass)); err != nil {
		h.rollbackWorkspace(wt, repo, req.name, *space)
		return fmt.Errorf("provision agent %q: %w", agentName, err)
	}
	return nil
}

func (h *Handler) rollbackWorkspace(wt Worktrees, repo, name string, space workspace) {
	if space.reused {
		return
	}
	if space.scratch {
		if rmErr := wt.RemoveScratch(repo, name); rmErr != nil {
			h.log.Warn("scratch rollback failed", "session", name, "err", rmErr)
		}
		return
	}
	if rmErr := wt.Remove(repo, name, true); rmErr != nil {
		h.log.Warn("worktree rollback failed", "session", name, "err", rmErr)
	}
}

func (h *Handler) approvalsNote(req createRequest, space workspace) string {
	mode := req.mode
	if mode == "" || mode == string(approval.ModeBypass) {
		return ""
	}
	vendor, name, agentName := req.runtime.vendor, req.name, req.runtime.agent
	gate := h.gateFor(vendor)
	switch {
	case !gate.enforced:
		why := gate.why
		if why == "" {
			why = "it exposes no approval channel"
		}
		return fmt.Sprintf("\n\n⚠️ approvals: %#q cannot enforce them (%s), so **%s** runs ungated.", vendorLabel(vendor), why, name)
	case req.shared || space.path == "":
		return fmt.Sprintf("\n\n⚠️ approvals: a shared session has no isolated worktree to gate, so **%s** runs ungated.", name)
	case req.hostName != "" && agentName == "":
		return fmt.Sprintf("\n\n⚠️ approvals: a remote session without an agent has no materialization channel, so **%s** runs ungated.", name)
	case agentName == "":
		if err := agent.MaterializeHook(space.path, agent.SelfBin()); err != nil {
			h.log.Warn("materialize hook failed", "session", name, "err", err)
			return fmt.Sprintf("\n\n⚠️ approvals: the hook could not be written (%v), so **%s** runs ungated.", err, name)
		}
	}
	return ""
}

func newSessionRecord(req createRequest, space workspace, project, runDir string) state.Session {
	r, o := req.runtime, req.options
	return state.Session{
		Name: req.name, Cmd: r.cmd, Backend: r.backend, Vendor: r.vendor, ModelID: r.model,
		Worktree: space.path, Scratch: space.scratch, Dir: runDir, Project: project, Agent: r.agent,
		MemoryProject: o.memProject, MemoryAgent: o.memAgent, ProjectPinned: o.projectPinned,
		Parent: o.parent, Gateways: o.gateways, Extractor: o.extractor, Journal: o.journal,
		ConsolidateEvery: o.consolidateEvery, CostCap: o.caps.cost, TokenCap: o.caps.tokens,
		CohortCostCap: o.caps.cohortCost, CohortTokenCap: o.caps.cohortTokens, Host: req.hostName,
	}
}

func openChannel(ctx context.Context, req createRequest, title string) (channelSlot, error) {
	if req.adopted != "" {
		return channelSlot{id: req.adopted, typ: "text"}, nil
	}
	switch req.home.Type {
	case categoryKind, terminalPlace:
		chID, err := req.admin.CreateUnder(ctx, req.home.ID, title)
		if err != nil {
			return channelSlot{}, fmt.Errorf("create channel: %w", err)
		}
		return channelSlot{id: chID, owned: true, typ: "text"}, nil
	case forumKind:
		chID, err := req.admin.ForumPost(ctx, req.home.ID, title, "Session **"+title+"** started.")
		if err != nil {
			return channelSlot{}, fmt.Errorf("create forum post: %w", err)
		}
		return channelSlot{id: chID, owned: true, typ: forumKind}, nil
	default:
		return channelSlot{}, &UnsupportedHomeTypeError{Type: req.home.Type}
	}
}

func (h *Handler) adminFor(sess state.Session) channelAdmin {
	if h.td != nil && strings.HasPrefix(sess.ChannelID, "terminal/") {
		return h.td
	}
	return h.d
}

func (h *Handler) sessionCloseRun(ctx context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	wt, ws, err := h.worktreesOn(sess.Host)
	if err != nil {
		return "", err
	}
	h.warnIfFailed(h.sup.Stop(name), "stop "+name)
	repo := repoFor(ws, sess.Project)
	if sess.Worktree != "" && sess.Scratch {
		if err := wt.RemoveScratch(repo, name); err != nil {
			h.sup.Start(sess)
			return "", err
		}
	}
	if sess.Worktree != "" && !sess.Scratch {
		force := in.Bool("force")
		if err := wt.Remove(repo, name, force); err != nil {
			h.sup.Start(sess)
			return "", fmt.Errorf("%w; commit, or close with force:true to discard (branch session/%s is kept)", err, name)
		}
	}
	note := h.tidyChannel(ctx, sess)
	if err := h.st.RemoveSession(name); err != nil {
		sess.Worktree, sess.Dir = "", repo
		h.sup.Start(sess)
		return "", fmt.Errorf("persist: %w", err)
	}
	h.warnIfFailed(state.RemoveParticipantJournal(state.ParticipantsPath(h.partDir, name)), "remove participants of "+name)
	h.warnIfFailed(state.RemoveTranscript(state.TranscriptPath(h.partDir, name)), "remove transcript of "+name)
	return fmt.Sprintf("🗄️ Session **%s** closed.%s", name, note), nil
}

func (h *Handler) sessionArchiveRun(ctx context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	h.warnIfFailed(h.sup.Stop(name), "stop "+name)
	note := h.tidyChannel(ctx, sess)
	if err := h.st.SetArchived(name, true); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return fmt.Sprintf("📦 Session **%s** archived. Resume it from /resume.%s", name, note), nil
}

func (h *Handler) tidyChannel(ctx context.Context, sess state.Session) string {
	if !sess.Owned {
		return ""
	}
	if err := h.adminFor(sess).Archive(ctx, sess.ChannelID); err != nil {
		return fmt.Sprintf("\n⚠️ le salon est resté ouvert : %v", err)
	}
	return ""
}

func (h *Handler) sessionRenameRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	raw, ok := in.Lookup("to")
	if !ok {
		return "", ErrMissingTo
	}
	to := slugify(raw)
	if to == "" || !sessionNameRe.MatchString(to) {
		return "", &InvalidValueError{Field: nameParam, Value: raw, Hint: "use letters, digits, - or _ (max 64, no /, spaces or ..)"}
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	if to == name {
		return fmt.Sprintf("session **%s** porte déjà ce nom", name), nil
	}
	if _, taken := h.st.FindSession(to); taken {
		return "", &SessionExistsError{Name: to}
	}
	h.warnIfFailed(h.sup.Stop(name), "stop "+name)
	if err := h.st.RenameSession(name, to); err != nil {
		h.sup.Start(sess)
		return "", fmt.Errorf("persist: %w", err)
	}
	note := ""
	if err := state.MoveSessionFile(state.TranscriptPath(h.partDir, name), state.TranscriptPath(h.partDir, to)); err != nil {
		note += fmt.Sprintf("\n⚠️ le transcript est resté sous %#q : %v", name, err)
	}
	if err := state.MoveSessionFile(state.ParticipantsPath(h.partDir, name), state.ParticipantsPath(h.partDir, to)); err != nil {
		note += fmt.Sprintf("\n⚠️ le journal des participants est resté sous %#q : %v", name, err)
	}
	if sc := scheduleOwning(h.st.SnapshotSchedules(), name); sc != "" {
		note += fmt.Sprintf("\n⚠️ l'horaire %#q possède cette session par son nom : sa prochaine fenêtre en rouvrira une sous %#q.", sc, name)
	}
	sess.Name = to
	h.sup.Start(sess)
	return fmt.Sprintf("✎ Session **%s** renommée en **%s**.%s", name, to, note), nil
}

func scheduleOwning(schedules []schedule.Schedule, session string) string {
	for i := range schedules {
		sc := schedules[i]
		if sc.Agent != "" && schedule.SessionName(sc) == session {
			return sc.Name
		}
	}
	return ""
}

func (h *Handler) sessionResumeRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	if err := h.st.SetArchived(name, false); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	sess.Archived = false
	h.sup.Start(sess)
	return fmt.Sprintf("⟲ Session **%s** resumed.", name), nil
}

func (h *Handler) sessionSwitchRun(ctx context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	vendor := in.Get("vendor")
	cmd, cmdSupplied := in.Lookup("cmd")
	cmdSupplied = cmdSupplied && cmd != ""
	handoff := in.Get("handoff")
	if handoff == "" {
		handoff = "none"
	}
	prior, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	modelID := prior.ModelID
	if v, supplied := in.Lookup("model"); supplied {
		modelID = v
	}
	if err := h.checkModel(vendor, modelID); err != nil {
		return "", err
	}
	if err := h.checkSpawnSource(cmdSupplied, modelID); err != nil {
		return "", err
	}
	targeted, persistErr := h.st.SetBackendTarget(name, vendor, cmd, modelID)
	if !targeted {
		return "", &SessionNotFoundError{Name: name}
	}
	if persistErr != nil {
		return "", fmt.Errorf("persist: %w", persistErr)
	}
	sess, _ := h.st.FindSession(name)
	switchNote := h.switchApprovalsNote(sess)
	if err := h.sup.Restart(sess); err != nil {
		return "", h.revertSwitch(name, prior, err)
	}
	return h.switchReply(ctx, name, vendor, handoff, switchNote), nil
}

func (h *Handler) switchApprovalsNote(sess state.Session) string {
	mode := sess.Approvals
	if mode == "" || mode == string(approval.ModeBypass) {
		return ""
	}
	gate := h.gateFor(sess.Vendor)
	if gate.enforced {
		return ""
	}
	why := gate.why
	if why == "" {
		why = "it exposes no approval channel"
	}
	return fmt.Sprintf("\n\n⚠️ approvals: %#q cannot enforce them (%s), so **%s** now runs ungated.", vendorLabel(sess.Vendor), why, sess.Name)
}

func (h *Handler) revertSwitch(name string, prior state.Session, restartErr error) error {
	if rbErr := h.rollbackSwitch(name, prior.Vendor, prior.Cmd, prior.ModelID, prior.ResumeToken); rbErr != nil {
		return fmt.Errorf("redémarrage backend: %w; rollback échoué, session %s hors service: %w", restartErr, name, rbErr)
	}
	return fmt.Errorf("redémarrage backend: %w (session %s restaurée sur %s)", restartErr, name, prior.Vendor)
}

func (h *Handler) switchReply(ctx context.Context, name, vendor, handoff, switchNote string) string {
	if handoff == "none" {
		return fmt.Sprintf("session %s re-ciblée sur %s (sans reprise)%s", name, vendor, switchNote)
	}
	seedText := buildHandoffSeed(
		state.ReadTranscript(state.TranscriptPath(h.partDir, name), sessionLogTranscriptCap),
		handoff,
	)
	if seedText != "" && h.seed != nil && !h.injectSeed(ctx, name, seedText) {
		return fmt.Sprintf("session %s re-ciblée sur %s (reprise %s non confirmée)%s", name, vendor, handoff, switchNote)
	}
	return fmt.Sprintf("session %s re-ciblée sur %s (reprise %s)%s", name, vendor, handoff, switchNote)
}

const seedRetryDelay = 150 * time.Millisecond

func (h *Handler) injectSeed(ctx context.Context, name, task string) bool {
	for range 20 {
		if h.seed(name, task) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(seedRetryDelay):
		}
	}
	return false
}

func (h *Handler) rollbackSwitch(name, oldVendor, oldCmd, oldModelID, oldToken string) error {
	_, persistErr := h.st.SetBackendTarget(name, oldVendor, oldCmd, oldModelID)
	persistErr = errors.Join(persistErr, h.st.SetResumeToken(name, oldToken))
	sess, _ := h.st.FindSession(name)
	return errors.Join(persistErr, h.sup.Restart(sess))
}

func (h *Handler) sessionListRun(_ context.Context, in contracts.Input) (string, error) {
	sessions := h.st.SnapshotSessions()
	if in.JSON {
		rows := make([]sessionJSON, 0, len(sessions))
		for i := range sessions {
			s := sessions[i]
			row := sessionJSONRow(s)
			h.attachUsage(&row)
			h.stampBudget(&row, s)
			if h.coord != nil {
				if v, ok := h.coord.CoordinationView(s.Name); ok {
					row.Coordination = &coordinationJSON{
						Role: v.Role, Lead: v.Lead, Reported: v.Reported,
						Expected: v.Expected, Complete: v.Complete,
					}
				}
			}
			rows = append(rows, row)
		}
		b, err := json.Marshal(rows)
		return string(b), err
	}
	if len(sessions) == 0 {
		return "No active sessions.", nil
	}
	var out strings.Builder
	out.WriteString("Active sessions:\n")
	for i := range sessions {
		s := sessions[i]
		out.WriteString(fmt.Sprintf("• **%s** (%s) %s\n", s.Name, s.Type, h.adminFor(s).ChannelRef(s.ChannelID)))
	}
	return out.String(), nil
}

func (h *Handler) sessionWhoRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	if in.JSON {
		row := sessionJSONRow(sess)
		h.attachUsage(&row)
		b, err := json.Marshal(row)
		return string(b), err
	}
	ids := state.ReadParticipants(state.ParticipantsPath(h.partDir, name))
	if len(ids) == 0 {
		return "Personne n'a encore écrit dans cette session.", nil
	}
	var out strings.Builder
	out.WriteString(fmt.Sprintf("Participants observed in **%s**:\n", name))
	for _, id := range ids {
		out.WriteString(fmt.Sprintf("• <@%s>\n", id))
	}
	return out.String(), nil
}

const sessionLogTranscriptCap = 200

const (
	nameParam       = "name"
	terminalPlace   = "terminal"
	categoryKind    = "category"
	forumKind       = "forum"
	nonNegativeHint = "use a non-negative integer"
)

func (h *Handler) sessionLogRun(_ context.Context, in contracts.Input) (string, error) {
	raw, ok := in.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		if in.JSON {
			return "[]", nil
		}
		return "No transcript yet.", nil
	}
	limit := sessionLogTranscriptCap
	if lraw, ok := in.Lookup("limit"); ok {
		if n, err := strconv.Atoi(lraw); err == nil && n > 0 {
			limit = n
		}
	}
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, name), limit)
	if in.JSON {
		if entries == nil {
			entries = []state.TranscriptEntry{}
		}
		b, err := json.Marshal(entries)
		return string(b), err
	}
	if len(entries) == 0 {
		return "No transcript yet.", nil
	}
	var out strings.Builder
	for _, e := range entries {
		out.WriteString(fmt.Sprintf("**%s** %s\n", e.Role, e.Text))
	}
	return out.String(), nil
}

func (h *Handler) warnIfFailed(err error, action string) {
	if err != nil {
		h.log.Warn("best effort step failed", "action", action, "err", err)
	}
}
