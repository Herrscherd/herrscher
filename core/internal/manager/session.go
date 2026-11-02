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
	usageFor func(state.Session) (float64, uint64),
) (float64, uint64) {
	byName := make(map[string]state.Session, len(all))
	children := make(map[string][]string, len(all))
	for _, s := range all {
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
		c, tk := usageFor(s)
		totalCost += c
		totalTokens += tk
		for _, ch := range children[name] {
			walk(ch)
		}
	}
	walk(root.Name)
	return totalCost, totalTokens
}

func aggregateUsage(entries []state.TranscriptEntry) (*usageJSON, string) {
	if len(entries) == 0 {
		return nil, ""
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
	return u, task
}

func (h *Handler) attachUsage(row *sessionJSON) {
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, row.Name), 0)
	if u, task := aggregateUsage(entries); u != nil {
		row.Usage = u
		if task != "" {
			row.Task = task
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
	patch := budgetPatch{}
	if v, ok := in.Lookup("cost_cap"); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", &InvalidValueError{Field: "cost_cap", Value: v}
		}
		patch.costCap = fptr(f)
	}
	if v, ok := in.Lookup("token_cap"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", &InvalidValueError{Field: "token_cap", Value: v}
		}
		patch.tokenCap = uptr(uint64(n))
	}
	if v, ok := in.Lookup("cohort_cost_cap"); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", &InvalidValueError{Field: "cohort_cost_cap", Value: v}
		}
		patch.cohortCostCap = fptr(f)
	}
	if v, ok := in.Lookup("cohort_token_cap"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", &InvalidValueError{Field: "cohort_token_cap", Value: v}
		}
		patch.cohortTokenCap = uptr(uint64(n))
	}
	usageFor := func(s state.Session) (float64, uint64) {
		entries := state.ReadTranscript(state.TranscriptPath(h.partDir, s.Name), 0)
		u, _ := aggregateUsage(entries)
		if u == nil {
			return 0, 0
		}
		return u.Cost, nonNegativeCount(u.TokensIn + u.TokensOut)
	}
	targets := []state.Session{sess}
	scope, _ := in.Lookup("scope")
	if scope == "cohort" {
		targets = cohortMembers(sess, h.st.SnapshotSessions())
	}
	for _, t := range targets {
		c, tk := usageFor(t)
		cc, ctk := cohortTotals(t, h.st.SnapshotSessions(), usageFor)
		updated := applyBudget(t, patch, c, tk)
		if updated.PausedReason == "cohort_cost" && (updated.CohortCostCap == 0 || cc < updated.CohortCostCap) {
			updated.PausedReason = ""
		}
		if updated.PausedReason == "cohort_tokens" && (updated.CohortTokenCap == 0 || ctk < updated.CohortTokenCap) {
			updated.PausedReason = ""
		}
		if err := h.st.SetBudget(updated.Name, updated.CostCap, updated.TokenCap, updated.CohortCostCap, updated.CohortTokenCap, updated.PausedReason); err != nil {
			return "", fmt.Errorf("persist budget for %q: %w", updated.Name, err)
		}
	}
	return fmt.Sprintf("✅ budget updated for %d session(s)", len(targets)), nil
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
		b += fmt.Sprintf("• Project: **%s** (`%s`)\n", filepath.Base(repo), repo)
	}
	switch {
	case worktree != "":
		b += "• Mode: isolated worktree\n"
		b += fmt.Sprintf("• Worktree: `%s`\n", worktree)
		b += fmt.Sprintf("• Branch: `%s`\n", branch)
	case shared:
		b += "• Mode: shared (main checkout)\n"
		b += "• Branch: none (runs on current branch)\n"
	default:
		b += "• Mode: shared (not a git repo)\n"
	}
	b += fmt.Sprintf("• Command: `%s`", cmd)
	if modelID != "" {
		b += fmt.Sprintf("\n• Model: `%s`", modelID)
	}
	return b
}

func (h *Handler) sessionCreateRun(ctx context.Context, in contracts.Input) (string, error) {
	raw, ok := in.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		return "", &InvalidValueError{Field: nameParam, Value: raw, Hint: "use letters, digits, - or _ (max 64, no /, spaces or ..)"}
	}
	if _, exists := h.st.FindSession(name); exists {
		return "", &SessionExistsError{Name: name}
	}
	if n := len(h.st.SnapshotSessions()); n >= maxSessions {
		return "", fmt.Errorf("%w (%d): close a session before creating another", ErrSessionLimit, maxSessions)
	}
	terminalOnly := in.Bool("terminal_only")
	home := h.st.Home
	admin := h.d
	if terminalOnly && h.td != nil {
		home = state.HomeRef{ID: terminalPlace, Type: terminalPlace}
		admin = h.td
	}
	adopted, _ := in.Lookup("channel_id")
	if under, ok := in.Lookup("under"); ok && under != "" {
		if adopted != "" {
			return "", ErrConflictingChannel
		}
		if terminalOnly {
			return "", ErrConflictingPlacement
		}
		kind, err := admin.Kind(ctx, under)
		if err != nil {
			return "", fmt.Errorf("inspect channel: %w", err)
		}
		if kind != categoryKind && kind != forumKind {
			return "", &ChannelKindError{Channel: under, Kind: kind, Need: "under must be a category or forum (to run in an existing conversation, pass channel_id)"}
		}
		home = state.HomeRef{ID: under, Type: kind}
	}
	if home.ID == "" && adopted == "" {
		return "", ErrNoHome
	}
	cmd := h.defaultCmd
	cmdExplicit := false
	if c, ok := in.Lookup("cmd"); ok && c != "" {
		cmd = c
		cmdExplicit = true
	}
	backend, _ := in.Lookup("backend")
	if backend == "" {
		backend = "stream"
	}
	agentName, _ := in.Lookup("agent")
	vendor, _ := in.Lookup("vendor")
	modelID, _ := in.Lookup("model")
	modelID = h.resolveModel(modelID, vendor, cmdExplicit)
	if err := h.checkModel(vendor, modelID); err != nil {
		return "", err
	}
	if err := h.checkSpawnSource(cmdExplicit, modelID); err != nil {
		return "", err
	}
	parent, _ := in.Lookup("parent")
	memProject, _ := in.Lookup("memory_project")
	if memProject != "" && !projectRe.MatchString(memProject) {
		return "", &InvalidValueError{Field: "memory_project", Value: memProject, Hint: "use a single name (no /, spaces, or ..)"}
	}
	memAgent, _ := in.Lookup("memory_agent")
	projectPinned := in.Bool("project_pinned")
	extractor, _ := in.Lookup("extractor")
	journal, _ := in.Lookup("journal")
	consolidateEvery := 0
	if v, ok := in.Lookup("consolidate_every"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", &InvalidValueError{Field: "consolidate_every", Value: v, Hint: nonNegativeHint}
		}
		consolidateEvery = n
	}
	gwList, _ := in.Lookup("gateways")
	gateways := ParseGateways(gwList, terminalOnly, h.defaultGateways)
	var costCap, cohortCostCap float64
	var tokenCap, cohortTokenCap uint64
	if v, ok := in.Lookup("cost_cap"); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", &InvalidValueError{Field: "cost_cap", Value: v}
		}
		costCap = f
	}
	if v, ok := in.Lookup("token_cap"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", &InvalidValueError{Field: "token_cap", Value: v, Hint: nonNegativeHint}
		}
		tokenCap = uint64(n)
	}
	if v, ok := in.Lookup("cohort_cost_cap"); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", &InvalidValueError{Field: "cohort_cost_cap", Value: v}
		}
		cohortCostCap = f
	}
	if v, ok := in.Lookup("cohort_token_cap"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", &InvalidValueError{Field: "cohort_token_cap", Value: v, Hint: nonNegativeHint}
		}
		cohortTokenCap = uint64(n)
	}
	hostName, _ := in.Lookup("host")
	hostName = strings.TrimSpace(hostName)
	if hostName == "" && agentName != "" {
		if a, ok := h.agents.Get(agentName); ok {
			hostName = a.Host
		}
	}
	if hostName == state.LocalHost {
		hostName = ""
	}
	mode := strings.TrimSpace(in.Get("approvals"))
	switch approval.Mode(mode) {
	case "", approval.ModeAsk, approval.ModeBypass, approval.ModeStrict:
	default:
		return "", &UnknownApprovalModeError{Mode: mode}
	}
	approvalsNote := ""
	if hostName != "" && h.hs != nil {
		if err := h.hs.Ready(hostName); err != nil {
			return "", err
		}
	}
	wt, ws, err := h.worktreesOn(hostName)
	if err != nil {
		return "", err
	}
	project, _ := in.Lookup("project")
	if ws != "" {
		if spec, ok := in.Lookup("clone"); ok && spec != "" {
			if hostName != "" {
				return "", fmt.Errorf("%w %q: create the project locally first, then create the session with --host", ErrCloneUnsupported, hostName)
			}
			cctx, cancel := context.WithTimeout(ctx, cloneTimeout)
			dir, err := h.fg.Clone(cctx, spec, ws)
			cancel()
			if err != nil {
				return "", fmt.Errorf("clone: %w", err)
			}
			project = filepath.Base(dir)
		}
		if project != "" && !projectRe.MatchString(project) {
			return "", &InvalidValueError{Field: "project", Value: project, Hint: "use a single name (no /, spaces, or ..)"}
		}
	}
	repo := repoFor(ws, project)
	if h.hs != nil {
		if err := h.hs.EnsureProject(ctx, hostName, project, repo); err != nil {
			return "", err
		}
	}
	shared := in.Bool("shared")
	var worktree string
	reusedWorktree := false
	if !shared {
		base := ""
		if b, ok := in.Lookup("base"); ok {
			base = b
		}
		reusedWorktree = wt.PreExisting(repo, name)
		path, err := wt.Create(repo, name, base)
		if err != nil {
			return "", fmt.Errorf("worktree: %w", err)
		}
		worktree = path
	}
	scratch := false
	rollbackWorktree := func() {
		if reusedWorktree {
			return
		}
		if scratch {
			if rmErr := wt.RemoveScratch(repo, name); rmErr != nil {
				h.log.Warn("scratch rollback failed", "session", name, "err", rmErr)
			}
			return
		}
		if rmErr := wt.Remove(repo, name, true); rmErr != nil {
			h.log.Warn("worktree rollback failed", "session", name, "err", rmErr)
		}
	}
	if agentName != "" {
		if shared {
			return "", &SharedAgentError{Agent: agentName}
		}
		if worktree == "" {
			if _, statErr := os.Stat(wt.ScratchPath(repo, name)); statErr == nil {
				reusedWorktree = true
			}
			path, err := wt.Scratch(repo, name)
			if err != nil {
				return "", fmt.Errorf("provision agent %q: %w", agentName, err)
			}
			worktree, scratch = path, true
		}
		a, found := h.agents.Get(agentName)
		if !found {
			rollbackWorktree()
			return "", &UnknownAgentError{Name: agentName}
		}
		if vendor == "" {
			vendor = a.Backend
		}
		if !cmdExplicit && a.Cmd != "" {
			cmd = a.Cmd
		}
		if err := h.materializeOn(ctx, hostName, a, worktree, mode != string(approval.ModeBypass)); err != nil {
			rollbackWorktree()
			return "", fmt.Errorf("provision agent %q: %w", agentName, err)
		}
	}
	if mode != "" && mode != string(approval.ModeBypass) {
		gates, why := h.gateFor(vendor)
		switch {
		case !gates:
			if why == "" {
				why = "it exposes no approval channel"
			}
			approvalsNote = fmt.Sprintf("\n\n⚠️ approvals: `%s` cannot enforce them (%s), so **%s** runs ungated.", vendorLabel(vendor), why, name)
		case shared || worktree == "":
			approvalsNote = fmt.Sprintf("\n\n⚠️ approvals: a shared session has no isolated worktree to gate, so **%s** runs ungated.", name)
		case hostName != "" && agentName == "":
			approvalsNote = fmt.Sprintf("\n\n⚠️ approvals: a remote session without an agent has no materialization channel, so **%s** runs ungated.", name)
		case agentName == "":
			if err := agent.MaterializeHook(worktree, agent.SelfBin()); err != nil {
				h.log.Warn("materialize hook failed", "session", name, "err", err)
				approvalsNote = fmt.Sprintf("\n\n⚠️ approvals: the hook could not be written (%v), so **%s** runs ungated.", err, name)
			}
		}
	}
	title := h.st.QualifiedName(name)
	runDir := worktree
	if runDir == "" {
		runDir = repo
	}
	var sess state.Session
	switch {
	case adopted != "":
		sess = state.Session{Name: name, ChannelID: adopted, Type: "text", Cmd: cmd, Backend: backend, Vendor: vendor, ModelID: modelID, Worktree: worktree, Scratch: scratch, Dir: runDir, Project: project, Agent: agentName, MemoryProject: memProject, MemoryAgent: memAgent, ProjectPinned: projectPinned, Parent: parent, Gateways: gateways, Extractor: extractor, Journal: journal, ConsolidateEvery: consolidateEvery, CostCap: costCap, TokenCap: tokenCap, CohortCostCap: cohortCostCap, CohortTokenCap: cohortTokenCap, Host: hostName}
	case home.Type == categoryKind, home.Type == terminalPlace:
		chID, err := admin.CreateUnder(ctx, home.ID, title)
		if err != nil {
			rollbackWorktree()
			return "", fmt.Errorf("create channel: %w", err)
		}
		sess = state.Session{Owned: true, Name: name, ChannelID: chID, Type: "text", Cmd: cmd, Backend: backend, Vendor: vendor, ModelID: modelID, Worktree: worktree, Scratch: scratch, Dir: runDir, Project: project, Agent: agentName, MemoryProject: memProject, MemoryAgent: memAgent, ProjectPinned: projectPinned, Parent: parent, Gateways: gateways, Extractor: extractor, Journal: journal, ConsolidateEvery: consolidateEvery, CostCap: costCap, TokenCap: tokenCap, CohortCostCap: cohortCostCap, CohortTokenCap: cohortTokenCap, Host: hostName}
	case home.Type == forumKind:
		chID, err := admin.ForumPost(ctx, home.ID, title, "Session **"+title+"** started.")
		if err != nil {
			rollbackWorktree()
			return "", fmt.Errorf("create forum post: %w", err)
		}
		sess = state.Session{Owned: true, Name: name, ChannelID: chID, Type: forumKind, Cmd: cmd, Backend: backend, Vendor: vendor, ModelID: modelID, Worktree: worktree, Scratch: scratch, Dir: runDir, Project: project, Agent: agentName, MemoryProject: memProject, MemoryAgent: memAgent, ProjectPinned: projectPinned, Parent: parent, Gateways: gateways, Extractor: extractor, Journal: journal, ConsolidateEvery: consolidateEvery, CostCap: costCap, TokenCap: tokenCap, CohortCostCap: cohortCostCap, CohortTokenCap: cohortTokenCap, Host: hostName}
	default:
		return "", &UnsupportedHomeTypeError{Type: home.Type}
	}
	sess.Approvals = mode
	if err := h.st.AddSession(sess); err != nil {
		rollbackWorktree()
		if note := h.tidyChannel(ctx, sess); note != "" {
			h.log.Warn("channel rollback", "session", name, "note", note)
		}
		return "", fmt.Errorf("persist: %w", err)
	}
	h.sup.Start(sess)
	banner := sessionBanner(repo, name, worktree, wt.Branch(name), cmd, modelID, shared)
	h.warnIfFailed(admin.Send(ctx, sess.ChannelID, banner), "send banner for "+name)
	return fmt.Sprintf("✅ Running on %s.\n\n%s%s", admin.ChannelRef(sess.ChannelID), banner, approvalsNote), nil
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
		note += fmt.Sprintf("\n⚠️ le transcript est resté sous `%s` : %v", name, err)
	}
	if err := state.MoveSessionFile(state.ParticipantsPath(h.partDir, name), state.ParticipantsPath(h.partDir, to)); err != nil {
		note += fmt.Sprintf("\n⚠️ le journal des participants est resté sous `%s` : %v", name, err)
	}
	if sc := scheduleOwning(h.st.SnapshotSchedules(), name); sc != "" {
		note += fmt.Sprintf("\n⚠️ l'horaire `%s` possède cette session par son nom : sa prochaine fenêtre en rouvrira une sous `%s`.", sc, name)
	}
	sess.Name = to
	h.sup.Start(sess)
	return fmt.Sprintf("✎ Session **%s** renommée en **%s**.%s", name, to, note), nil
}

func scheduleOwning(schedules []schedule.Schedule, session string) string {
	for _, sc := range schedules {
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
	oldVendor, oldCmd, oldModelID, oldToken := prior.Vendor, prior.Cmd, prior.ModelID, prior.ResumeToken
	targeted, persistErr := h.st.SetBackendTarget(name, vendor, cmd, modelID)
	if !targeted {
		return "", &SessionNotFoundError{Name: name}
	}
	if persistErr != nil {
		return "", fmt.Errorf("persist: %w", persistErr)
	}
	sess, _ := h.st.FindSession(name)
	switchNote := ""
	if m := sess.Approvals; m != "" && m != string(approval.ModeBypass) {
		if gates, why := h.gateFor(sess.Vendor); !gates {
			if why == "" {
				why = "it exposes no approval channel"
			}
			switchNote = fmt.Sprintf("\n\n⚠️ approvals: `%s` cannot enforce them (%s), so **%s** now runs ungated.", vendorLabel(sess.Vendor), why, name)
		}
	}
	if err := h.sup.Restart(sess); err != nil {
		if rbErr := h.rollbackSwitch(name, oldVendor, oldCmd, oldModelID, oldToken); rbErr != nil {
			return "", fmt.Errorf("redémarrage backend: %w; rollback échoué, session %s hors service: %w", err, name, rbErr)
		}
		return "", fmt.Errorf("redémarrage backend: %w (session %s restaurée sur %s)", err, name, oldVendor)
	}
	if handoff == "none" {
		return fmt.Sprintf("session %s re-ciblée sur %s (sans reprise)%s", name, vendor, switchNote), nil
	}
	seedText := buildHandoffSeed(
		state.ReadTranscript(state.TranscriptPath(h.partDir, name), sessionLogTranscriptCap),
		handoff,
	)
	if seedText != "" && h.seed != nil && !h.injectSeed(ctx, name, seedText) {
		return fmt.Sprintf("session %s re-ciblée sur %s (reprise %s non confirmée)%s", name, vendor, handoff, switchNote), nil
	}
	return fmt.Sprintf("session %s re-ciblée sur %s (reprise %s)%s", name, vendor, handoff, switchNote), nil
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
		for _, s := range sessions {
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
	for _, s := range sessions {
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
