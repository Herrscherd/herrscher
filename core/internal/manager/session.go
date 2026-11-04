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
	ID           string            `json:"id"`
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
		session := all[i]
		byName[session.Name] = session
		if session.Parent != "" && session.Parent != session.Name {
			children[session.Parent] = append(children[session.Parent], session.Name)
		}
	}
	root := target
	seen := map[string]bool{root.Name: true}
	for root.Parent != "" && root.Parent != root.Name {
		parent, ok := byName[root.Parent]
		if !ok || seen[parent.Name] {
			break
		}
		seen[parent.Name] = true
		root = parent
	}
	var totalCost float64
	var totalTokens uint64
	visited := make(map[string]bool)
	var walk func(name string)
	walk = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		member, ok := byName[name]
		if !ok {
			return
		}
		usage := usageFor(member)
		totalCost += usage.cost
		totalTokens += usage.tokens
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
	summary := &usageJSON{StartedAt: entries[0].Ts}
	var task string
	for _, entry := range entries {
		switch entry.Role {
		case "assistant":
			summary.Turns++
			summary.Cost += entry.Cost
			summary.TokensIn += entry.TokensIn
			summary.TokensOut += entry.TokensOut
			summary.CacheRead += entry.CacheRead
			summary.CacheCreate += entry.CacheCreate
		case "user":
			task = entry.Text
		}
	}
	return transcriptUsage{usage: summary, task: task}
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

func applyBudget(session state.Session, patch budgetPatch, curCost float64, curTokens uint64) state.Session {
	if patch.costCap != nil {
		session.CostCap = *patch.costCap
	}
	if patch.tokenCap != nil {
		session.TokenCap = *patch.tokenCap
	}
	if patch.cohortCostCap != nil {
		session.CohortCostCap = *patch.cohortCostCap
	}
	if patch.cohortTokenCap != nil {
		session.CohortTokenCap = *patch.cohortTokenCap
	}
	if session.PausedReason == "cost" && (session.CostCap == 0 || curCost < session.CostCap) {
		session.PausedReason = ""
	}
	if session.PausedReason == "tokens" && (session.TokenCap == 0 || curTokens < session.TokenCap) {
		session.PausedReason = ""
	}
	return session
}

func (h *Handler) sessionSetBudgetRun(_ context.Context, input contracts.Input) (string, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	sess, ok := h.st.FindSession(name)
	if !ok {
		return "", &SessionNotFoundError{Name: name}
	}
	patch, err := parseBudgetPatch(input)
	if err != nil {
		return "", err
	}
	targets := []state.Session{sess}
	if scope, _ := input.Lookup("scope"); scope == "cohort" {
		targets = cohortMembers(sess, h.st.SnapshotSessions())
	}
	for i := range targets {
		if applyErr := h.applyBudgetTo(targets[i], patch); applyErr != nil {
			return "", applyErr
		}
	}
	return fmt.Sprintf("✅ budget updated for %d session(s)", len(targets)), nil
}

func parseBudgetPatch(input contracts.Input) (budgetPatch, error) {
	var patch budgetPatch
	if err := setFloatCap(input, "cost_cap", &patch.costCap); err != nil {
		return budgetPatch{}, err
	}
	if err := setCountCap(input, "token_cap", &patch.tokenCap); err != nil {
		return budgetPatch{}, err
	}
	if err := setFloatCap(input, "cohort_cost_cap", &patch.cohortCostCap); err != nil {
		return budgetPatch{}, err
	}
	if err := setCountCap(input, "cohort_token_cap", &patch.cohortTokenCap); err != nil {
		return budgetPatch{}, err
	}
	return patch, nil
}

func setFloatCap(input contracts.Input, key string, dst **float64) error {
	raw, ok := input.Lookup(key)
	if !ok || raw == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return &InvalidValueError{Field: key, Value: raw}
	}
	*dst = fptr(parsed)
	return nil
}

func setCountCap(input contracts.Input, key string, dst **uint64) error {
	raw, ok := input.Lookup(key)
	if !ok || raw == "" {
		return nil
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return &InvalidValueError{Field: key, Value: raw}
	}
	*dst = uptr(nonNegativeCount(count))
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
	usage := aggregateUsage(entries).usage
	if usage == nil {
		return sessionUsage{}
	}
	return sessionUsage{cost: usage.Cost, tokens: nonNegativeCount(usage.TokensIn + usage.TokensOut)}
}

func releaseCohortPause(updated *state.Session, cohort sessionUsage) {
	if updated.PausedReason == "cohort_cost" && (updated.CohortCostCap == 0 || cohort.cost < updated.CohortCostCap) {
		updated.PausedReason = ""
	}
	if updated.PausedReason == "cohort_tokens" && (updated.CohortTokenCap == 0 || cohort.tokens < updated.CohortTokenCap) {
		updated.PausedReason = ""
	}
}

func (h *Handler) stampBudget(row *sessionJSON, session state.Session) {
	if session.CostCap == 0 && session.TokenCap == 0 && session.CohortCostCap == 0 &&
		session.CohortTokenCap == 0 && session.PausedReason == "" {
		return
	}
	if row.Usage == nil {
		row.Usage = &usageJSON{}
	}
	row.Usage.CostCap = session.CostCap
	row.Usage.TokenCap = session.TokenCap
	row.Usage.CohortCostCap = session.CohortCostCap
	row.Usage.CohortTokenCap = session.CohortTokenCap
	row.Usage.PausedReason = session.PausedReason
}

type coordinationJSON struct {
	Role     string `json:"role"`
	Lead     string `json:"lead"`
	Reported int    `json:"reported"`
	Expected int    `json:"expected"`
	Complete bool   `json:"complete"`
}

func sessionJSONRow(session state.Session) sessionJSON {
	gateways := session.BoundGateways()
	if gateways == nil {
		gateways = []string{}
	}
	status := "running"
	if session.PausedReason != "" {
		status = "paused"
	}
	if session.Archived {
		status = "archived"
	}
	return sessionJSON{
		ID: session.Name, Incarnation: session.Incarnation, Name: session.Name, Agent: session.Agent, Project: session.Project, Status: status,
		Worktree: session.Worktree, Gateways: gateways, Parent: session.Parent,
		Resumable: session.ResumeToken != "",
	}
}

func sessionBanner(repo, name, worktree, branch, cmd, modelID string, shared bool) string {
	banner := fmt.Sprintf("🚀 Session **%s** ready.\n", name)
	if repo == "" {
		banner += "• Project: **(cwd)**\n"
	} else {
		banner += fmt.Sprintf("• Project: **%s** (%#q)\n", filepath.Base(repo), repo)
	}
	switch {
	case worktree != "":
		banner += "• Mode: isolated worktree\n"
		banner += fmt.Sprintf("• Worktree: %#q\n", worktree)
		banner += fmt.Sprintf("• Branch: %#q\n", branch)
	case shared:
		banner += "• Mode: shared (main checkout)\n"
		banner += "• Branch: none (runs on current branch)\n"
	default:
		banner += "• Mode: shared (not a git repo)\n"
	}
	banner += fmt.Sprintf("• Command: %#q", cmd)
	if modelID != "" {
		banner += fmt.Sprintf("\n• Model: %#q", modelID)
	}
	return banner
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

func (h *Handler) sessionCreateRun(ctx context.Context, input contracts.Input) (string, error) {
	req, err := h.parseCreateRequest(ctx, input)
	if err != nil {
		return "", err
	}
	if readyErr := h.checkHostReady(req.hostName); readyErr != nil {
		return "", readyErr
	}
	place, err := h.placeSession(ctx, input, &req)
	if err != nil {
		return "", err
	}
	approvalsNote := h.approvalsNote(ctx, req, place.space)
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
		h.rollbackPlacement(ctx, place, req.name)
		return "", err
	}
	sess.ChannelID, sess.Owned, sess.Type = slot.id, slot.owned, slot.typ
	sess.Approvals = req.mode
	if err := h.st.AddSession(sess); err != nil {
		h.rollbackPlacement(ctx, place, req.name)
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

func (h *Handler) placeSession(ctx context.Context, input contracts.Input, req *createRequest) (placement, error) {
	worktrees, wtRoot, err := h.worktreesOn(req.hostName)
	if err != nil {
		return placement{}, err
	}
	project, err := h.resolveProject(ctx, input, req.hostName, wtRoot)
	if err != nil {
		return placement{}, err
	}
	repo := repoFor(wtRoot, project)
	if h.hs != nil {
		if projectErr := h.hs.EnsureProject(ctx, req.hostName, project, repo); projectErr != nil {
			return placement{}, projectErr
		}
	}
	space, err := ensureWorkspace(ctx, worktrees, repo, *req)
	if err != nil {
		return placement{}, err
	}
	if req.runtime.agent != "" {
		if provisionErr := h.provisionAgent(ctx, req, worktrees, repo, &space); provisionErr != nil {
			return placement{}, provisionErr
		}
	}
	return placement{wt: worktrees, repo: repo, project: project, space: space}, nil
}

func (h *Handler) rollbackPlacement(ctx context.Context, place placement, name string) {
	h.rollbackWorkspace(ctx, place.wt, place.repo, name, place.space)
}

func (h *Handler) parseCreateRequest(ctx context.Context, input contracts.Input) (createRequest, error) {
	raw, ok := input.Lookup(nameParam)
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
	terminalOnly := input.Bool("terminal_only")
	adopted, _ := input.Lookup("channel_id")
	home, admin, err := h.chooseHome(ctx, input, adopted, terminalOnly)
	if err != nil {
		return createRequest{}, err
	}
	runtime, err := h.chooseRuntime(input)
	if err != nil {
		return createRequest{}, err
	}
	options, err := h.parseSessionOptions(input, terminalOnly)
	if err != nil {
		return createRequest{}, err
	}
	mode := strings.TrimSpace(input.Get("approvals"))
	if err := validateApprovalsMode(mode); err != nil {
		return createRequest{}, err
	}
	base, _ := input.Lookup("base")
	return createRequest{
		name:     name,
		base:     base,
		home:     home,
		admin:    admin,
		adopted:  adopted,
		runtime:  runtime,
		options:  options,
		hostName: h.resolveHost(input, runtime.agent),
		mode:     mode,
		shared:   input.Bool("shared"),
	}, nil
}

func (h *Handler) chooseHome(ctx context.Context, input contracts.Input, adopted string, terminalOnly bool) (state.HomeRef, channelAdmin, error) {
	home, admin := h.st.Home, h.d
	if terminalOnly && h.td != nil {
		home = state.HomeRef{ID: terminalPlace, Type: terminalPlace}
		admin = h.td
	}
	under, ok := input.Lookup("under")
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

func (h *Handler) chooseRuntime(input contracts.Input) (runtimeChoice, error) {
	var runtime runtimeChoice
	runtime.cmd = h.defaultCmd
	if c, ok := input.Lookup("cmd"); ok && c != "" {
		runtime.cmd = c
		runtime.explicit = true
	}
	runtime.backend, _ = input.Lookup("backend")
	if runtime.backend == "" {
		runtime.backend = "stream"
	}
	runtime.agent, _ = input.Lookup("agent")
	runtime.vendor, _ = input.Lookup("vendor")
	model, _ := input.Lookup("model")
	runtime.model = h.resolveModel(model, runtime.vendor, runtime.explicit)
	if err := h.checkModel(runtime.vendor, runtime.model); err != nil {
		return runtimeChoice{}, err
	}
	if err := h.checkSpawnSource(runtime.explicit, runtime.model); err != nil {
		return runtimeChoice{}, err
	}
	return runtime, nil
}

func (h *Handler) parseSessionOptions(input contracts.Input, terminalOnly bool) (sessionOptions, error) {
	var options sessionOptions
	options.parent, _ = input.Lookup("parent")
	options.memProject, _ = input.Lookup("memory_project")
	if options.memProject != "" && !projectRe.MatchString(options.memProject) {
		return sessionOptions{}, &InvalidValueError{Field: "memory_project", Value: options.memProject, Hint: "use a single name (no /, spaces, or ..)"}
	}
	options.memAgent, _ = input.Lookup("memory_agent")
	options.projectPinned = input.Bool("project_pinned")
	options.extractor, _ = input.Lookup("extractor")
	options.journal, _ = input.Lookup("journal")
	every, err := optionalNonNegative(input, "consolidate_every")
	if err != nil {
		return sessionOptions{}, err
	}
	options.consolidateEvery = every
	gwList, _ := input.Lookup("gateways")
	options.gateways = ParseGateways(gwList, terminalOnly, h.defaultGateways)
	caps, err := parseBudgetCaps(input)
	if err != nil {
		return sessionOptions{}, err
	}
	options.caps = caps
	return options, nil
}

func parseBudgetCaps(input contracts.Input) (budgetCaps, error) {
	var caps budgetCaps
	var err error
	if caps.cost, err = optionalFloat(input, "cost_cap"); err != nil {
		return budgetCaps{}, err
	}
	tokens, err := optionalNonNegative(input, "token_cap")
	if err != nil {
		return budgetCaps{}, err
	}
	caps.tokens = nonNegativeCount(tokens)
	if caps.cohortCost, err = optionalFloat(input, "cohort_cost_cap"); err != nil {
		return budgetCaps{}, err
	}
	cohortTokens, err := optionalNonNegative(input, "cohort_token_cap")
	if err != nil {
		return budgetCaps{}, err
	}
	caps.cohortTokens = nonNegativeCount(cohortTokens)
	return caps, nil
}

func optionalFloat(input contracts.Input, key string) (float64, error) {
	value, ok := input.Lookup(key)
	if !ok || value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, &InvalidValueError{Field: key, Value: value}
	}
	return parsed, nil
}

func optionalNonNegative(input contracts.Input, key string) (int, error) {
	value, ok := input.Lookup(key)
	if !ok || value == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0, &InvalidValueError{Field: key, Value: value, Hint: nonNegativeHint}
	}
	return count, nil
}

func (h *Handler) resolveHost(input contracts.Input, agentName string) string {
	hostName, _ := input.Lookup("host")
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
	case "", approval.ModeAsk, approval.ModeAcceptEdits, approval.ModePlan, approval.ModeAuto, approval.ModeBypass, approval.ModeStrict, approval.ModeSmart:
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

func (h *Handler) resolveProject(ctx context.Context, input contracts.Input, hostName, wtRoot string) (string, error) {
	project, _ := input.Lookup("project")
	if wtRoot == "" {
		return project, nil
	}
	if spec, ok := input.Lookup("clone"); ok && spec != "" {
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

func ensureWorkspace(ctx context.Context, worktrees Worktrees, repo string, req createRequest) (workspace, error) {
	if req.shared {
		return workspace{}, nil
	}
	reused := worktrees.PreExisting(ctx, repo, req.name)
	path, err := worktrees.Create(ctx, repo, req.name, req.base)
	if err != nil {
		return workspace{}, fmt.Errorf("worktree: %w", err)
	}
	return workspace{path: path, reused: reused}, nil
}

func (h *Handler) provisionAgent(ctx context.Context, req *createRequest, worktrees Worktrees, repo string, space *workspace) error {
	agentName := req.runtime.agent
	if req.shared {
		return &SharedAgentError{Agent: agentName}
	}
	if space.path == "" {
		if _, statErr := os.Stat(worktrees.ScratchPath(repo, req.name)); statErr == nil {
			space.reused = true
		}
		path, err := worktrees.Scratch(repo, req.name)
		if err != nil {
			return fmt.Errorf("provision agent %q: %w", agentName, err)
		}
		space.path, space.scratch = path, true
	}
	agentSpec, found := h.agents.Get(agentName)
	if !found {
		h.rollbackWorkspace(ctx, worktrees, repo, req.name, *space)
		return &UnknownAgentError{Name: agentName}
	}
	if req.runtime.vendor == "" {
		req.runtime.vendor = agentSpec.Backend
	}
	if !req.runtime.explicit && agentSpec.Cmd != "" {
		req.runtime.cmd = agentSpec.Cmd
	}
	if err := h.materializeOn(ctx, req.hostName, agentSpec, space.path, req.mode != string(approval.ModeBypass)); err != nil {
		h.rollbackWorkspace(ctx, worktrees, repo, req.name, *space)
		return fmt.Errorf("provision agent %q: %w", agentName, err)
	}
	return nil
}

func (h *Handler) rollbackWorkspace(ctx context.Context, worktrees Worktrees, repo, name string, space workspace) {
	if space.reused {
		return
	}
	if space.scratch {
		if rmErr := worktrees.RemoveScratch(repo, name); rmErr != nil {
			h.log.Warn("scratch rollback failed", "session", name, "err", rmErr)
		}
		return
	}
	if rmErr := worktrees.Remove(context.WithoutCancel(ctx), repo, name, true); rmErr != nil {
		h.log.Warn("worktree rollback failed", "session", name, "err", rmErr)
	}
}

func (h *Handler) approvalsNote(ctx context.Context, req createRequest, space workspace) string {
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
		if err := agent.MaterializeHook(ctx, space.path, agent.SelfBin()); err != nil {
			h.log.Warn("materialize hook failed", "session", name, "err", err)
			return fmt.Sprintf("\n\n⚠️ approvals: the hook could not be written (%v), so **%s** runs ungated.", err, name)
		}
	}
	return ""
}

func newSessionRecord(req createRequest, space workspace, project, runDir string) state.Session {
	runtime, options := req.runtime, req.options
	return state.Session{
		Name: req.name, Cmd: runtime.cmd, Backend: runtime.backend, Vendor: runtime.vendor, ModelID: runtime.model,
		Worktree: space.path, Scratch: space.scratch, Dir: runDir, Project: project, Agent: runtime.agent,
		MemoryProject: options.memProject, MemoryAgent: options.memAgent, ProjectPinned: options.projectPinned,
		Parent: options.parent, Gateways: options.gateways, Extractor: options.extractor, Journal: options.journal,
		ConsolidateEvery: options.consolidateEvery, CostCap: options.caps.cost, TokenCap: options.caps.tokens,
		CohortCostCap: options.caps.cohortCost, CohortTokenCap: options.caps.cohortTokens, Host: req.hostName,
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

func (h *Handler) sessionCloseRun(ctx context.Context, input contracts.Input) (string, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	wtRoot, repoRoot, err := h.worktreesOn(sess.Host)
	if err != nil {
		return "", err
	}
	h.warnIfFailed(h.sup.Stop(name), "stop "+name)
	repo := repoFor(repoRoot, sess.Project)
	if sess.Worktree != "" && sess.Scratch {
		if err := wtRoot.RemoveScratch(repo, name); err != nil {
			h.sup.Start(sess)
			return "", err
		}
	}
	if sess.Worktree != "" && !sess.Scratch {
		force := input.Bool("force")
		if err := wtRoot.Remove(ctx, repo, name, force); err != nil {
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

func (h *Handler) sessionArchiveRun(ctx context.Context, input contracts.Input) (string, error) {
	name, ok := input.Lookup(nameParam)
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

func (h *Handler) sessionRenameRun(_ context.Context, input contracts.Input) (string, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	raw, ok := input.Lookup("to")
	if !ok {
		return "", ErrMissingTo
	}
	target := slugify(raw)
	if target == "" || !sessionNameRe.MatchString(target) {
		return "", &InvalidValueError{Field: nameParam, Value: raw, Hint: "use letters, digits, - or _ (max 64, no /, spaces or ..)"}
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	if target == name {
		return fmt.Sprintf("session **%s** porte déjà ce nom", name), nil
	}
	if _, taken := h.st.FindSession(target); taken {
		return "", &SessionExistsError{Name: target}
	}
	h.warnIfFailed(h.sup.Stop(name), "stop "+name)
	if err := h.st.RenameSession(name, target); err != nil {
		h.sup.Start(sess)
		return "", fmt.Errorf("persist: %w", err)
	}
	note := ""
	if err := state.MoveSessionFile(state.TranscriptPath(h.partDir, name), state.TranscriptPath(h.partDir, target)); err != nil {
		note += fmt.Sprintf("\n⚠️ le transcript est resté sous %#q : %v", name, err)
	}
	if err := state.MoveSessionFile(state.ParticipantsPath(h.partDir, name), state.ParticipantsPath(h.partDir, target)); err != nil {
		note += fmt.Sprintf("\n⚠️ le journal des participants est resté sous %#q : %v", name, err)
	}
	if sc := scheduleOwning(h.st.SnapshotSchedules(), name); sc != "" {
		note += fmt.Sprintf("\n⚠️ l'horaire %#q possède cette session par son nom : sa prochaine fenêtre en rouvrira une sous %#q.", sc, name)
	}
	sess.Name = target
	h.sup.Start(sess)
	return fmt.Sprintf("✎ Session **%s** renommée en **%s**.%s", name, target, note), nil
}

func scheduleOwning(schedules []schedule.Schedule, session string) string {
	for i := range schedules {
		sched := schedules[i]
		if sched.Agent != "" && schedule.SessionName(sched) == session {
			return sched.Name
		}
	}
	return ""
}

func (h *Handler) sessionResumeRun(_ context.Context, input contracts.Input) (string, error) {
	name, ok := input.Lookup(nameParam)
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

func (h *Handler) sessionSwitchRun(ctx context.Context, input contracts.Input) (string, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	vendor := input.Get("vendor")
	cmd, cmdSupplied := input.Lookup("cmd")
	cmdSupplied = cmdSupplied && cmd != ""
	handoff := input.Get("handoff")
	if handoff == "" {
		handoff = "none"
	}
	prior, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	modelID := prior.ModelID
	if v, supplied := input.Lookup("model"); supplied {
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

func (h *Handler) sessionListRun(_ context.Context, input contracts.Input) (string, error) {
	sessions := h.st.SnapshotSessions()
	if input.JSON {
		rows := make([]sessionJSON, 0, len(sessions))
		for i := range sessions {
			session := sessions[i]
			row := sessionJSONRow(session)
			h.attachUsage(&row)
			h.stampBudget(&row, session)
			if h.coord != nil {
				if view, ok := h.coord.CoordinationView(session.Name); ok {
					row.Coordination = &coordinationJSON{
						Role: view.Role, Lead: view.Lead, Reported: view.Reported,
						Expected: view.Expected, Complete: view.Complete,
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
		fmt.Fprintf(&out, "• **%s** (%s) %s\n", s.Name, s.Type, h.adminFor(s).ChannelRef(s.ChannelID))
	}
	return out.String(), nil
}

func (h *Handler) sessionWhoRun(_ context.Context, input contracts.Input) (string, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", &SessionNotFoundError{Name: name}
	}
	if input.JSON {
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
	fmt.Fprintf(&out, "Participants observed in **%s**:\n", name)
	for _, id := range ids {
		fmt.Fprintf(&out, "• <@%s>\n", id)
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

func (h *Handler) sessionLogRun(_ context.Context, input contracts.Input) (string, error) {
	raw, ok := input.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		if input.JSON {
			return "[]", nil
		}
		return "No transcript yet.", nil
	}
	limit := sessionLogTranscriptCap
	if lraw, ok := input.Lookup("limit"); ok {
		if n, err := strconv.Atoi(lraw); err == nil && n > 0 {
			limit = n
		}
	}
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, name), limit)
	if input.JSON {
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
		fmt.Fprintf(&out, "**%s** %s\n", e.Role, e.Text)
	}
	return out.String(), nil
}

func (h *Handler) warnIfFailed(err error, action string) {
	if err != nil {
		h.log.Warn("best effort step failed", "action", action, "err", err)
	}
}
