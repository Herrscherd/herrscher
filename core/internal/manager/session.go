package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// Forge/git clone runs inline on the dispatch path, so bound it: an unreachable
// host or a hung CLI must not wedge the daemon. Clone gets a generous ceiling
// for large repos.
const cloneTimeout = 10 * time.Minute

// maxSessions caps how many sessions can be live at once. Each session mints a
// worktree and a supervised child process, so an unbounded create loop (e.g. a
// script driving Dispatch) could exhaust process/disk resources.
const maxSessions = 64

type sessionJSON struct {
	// Id is the addressable handle by-name ops accept; Name is already the
	// persisted slug, so id == name. Emitted so a consumer can match a session by
	// id (Neublox's get_session) without re-deriving the slug.
	Id          string   `json:"id"`
	Incarnation string   `json:"incarnation"`
	Name        string   `json:"name"`
	Agent       string   `json:"agent"`
	Project     string   `json:"project"`
	Status      string   `json:"status"`
	Worktree    string   `json:"worktree"`
	Gateways    []string `json:"gateways"`
	// omitempty so a root session omits the key entirely, decoding to a real
	// "no parent" (null/None) rather than an empty-string parent named "".
	Parent string `json:"parent,omitempty"`
	// Coordination is the session's join state, present only for sessions in a
	// coordination cohort (omitempty → solo sessions omit the key entirely).
	Coordination *coordinationJSON `json:"coordination,omitempty"`
	// Resumable is true when the session carries a backend resume token, i.e. an
	// archived session that /resume (Neublox: Op::ResumeSession) can revive with
	// its stored conversation. Mirrors herrscher-contracts SessionInfo.Resumable.
	Resumable bool `json:"resumable"`
	// Usage rolls the session's transcript up into a live activity aggregate
	// (turns, token breakdown, prompt-cache, cost, start time). Nil — omitted —
	// when the session has never run a turn, so a fresh/idle session honestly
	// carries no usage rather than a zeroed one.
	Usage *usageJSON `json:"usage,omitempty"`
	// Task is the session's real current objective: the text of its last user
	// turn. Empty (omitted) when nothing has been dispatched yet, so the app can
	// render an honest empty state instead of echoing the session name.
	Task string `json:"task,omitempty"`
}

// usageJSON is the wire shape of a session's rolled-up usage. Fields are snake_case
// to match the transcript wire (tokens_in, cache_read); the Neublox daemon's
// SessionUsage aliases them and re-serializes camelCase for the app.
type usageJSON struct {
	Cost        float64 `json:"cost,omitempty"`
	TokensIn    int     `json:"tokens_in,omitempty"`
	TokensOut   int     `json:"tokens_out,omitempty"`
	CacheRead   int     `json:"cache_read,omitempty"`
	CacheCreate int     `json:"cache_create,omitempty"`
	Turns       int     `json:"turns,omitempty"`
	StartedAt   string  `json:"started_at,omitempty"`
	// Budget caps in effect for this session (0 = uncapped) + the reason it is
	// currently paused, if any. Carried so the app can render progress bands.
	CostCap        float64 `json:"cost_cap,omitempty"`
	TokenCap       uint64  `json:"token_cap,omitempty"`
	CohortCostCap  float64 `json:"cohort_cost_cap,omitempty"`
	CohortTokenCap uint64  `json:"cohort_token_cap,omitempty"`
	PausedReason   string  `json:"paused_reason,omitempty"`
}

// aggregateUsage folds a session's transcript into its usage aggregate and its
// current objective. Turns/tokens/cost sum over assistant entries; StartedAt is
// the first entry's timestamp; task is the last user entry's text. Returns
// (nil, "") for an empty transcript so an idle session omits usage entirely.
// cohortTotals sums usage across the parent forest that `target` belongs to:
// the root ancestor and every session transitively reachable from it via Parent.
// usageFor returns (cost, tokens) for a single session. Cycles are cut with a
// visited set. A solo session (no parent, no children) folds to just itself.
func cohortTotals(target state.Session, all []state.Session,
	usageFor func(state.Session) (float64, uint64)) (float64, uint64) {
	byName := make(map[string]state.Session, len(all))
	children := make(map[string][]string, len(all))
	for _, s := range all {
		byName[s.Name] = s
		if s.Parent != "" && s.Parent != s.Name {
			children[s.Parent] = append(children[s.Parent], s.Name)
		}
	}
	// Climb to the root ancestor.
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
	// Walk the whole subtree from root.
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

// attachUsage reads the session's transcript and stamps its usage aggregate and
// current objective onto row. Best-effort: a missing transcript leaves row's
// Usage nil (honest "no activity yet"). Kept off sessionJSONRow, which is pure
// and Handler-free, since the transcript lives under h.partDir.
func (h *Handler) attachUsage(row *sessionJSON) {
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, row.Name), 0)
	if u, task := aggregateUsage(entries); u != nil {
		row.Usage = u
		if task != "" {
			row.Task = task
		}
	}
}

// budgetPatch carries the caps a set-budget command wants to change. A nil
// pointer means "leave unchanged"; a non-nil pointer to 0 means "uncap".
type budgetPatch struct {
	costCap        *float64
	tokenCap       *uint64
	cohortCostCap  *float64
	cohortTokenCap *uint64
}

func fptr(f float64) *float64 { return &f }
func uptr(u uint64) *uint64   { return &u }

// applyBudget writes the patched caps onto the session, then re-evaluates the
// pause: if the session's current usage no longer exceeds any active cap, the
// PausedReason clears (this is the resume mechanism). curCost/curTokens are the
// session's current folded usage.
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
	// Re-evaluate the session-level pause against the new caps. Cohort re-eval
	// is done by the caller when scope == cohort (it holds all members).
	if s.PausedReason == "cost" && (s.CostCap == 0 || curCost < s.CostCap) {
		s.PausedReason = ""
	}
	if s.PausedReason == "tokens" && (s.TokenCap == 0 || curTokens < s.TokenCap) {
		s.PausedReason = ""
	}
	return s
}

// sessionSetBudgetRun sets or clears budget caps on a live session, or on every
// member of its cohort when scope=="cohort". Only params actually supplied
// (non-empty) are patched; raising a cap past current usage clears a matching
// PausedReason so the session resumes.
func (h *Handler) sessionSetBudgetRun(ctx context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	sess, ok := h.st.FindSession(name)
	if !ok {
		return "", fmt.Errorf("session %q not found", name)
	}
	patch := budgetPatch{}
	if v, ok := in.Lookup("cost_cap"); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", fmt.Errorf("invalid cost_cap %q", v)
		}
		patch.costCap = fptr(f)
	}
	if v, ok := in.Lookup("token_cap"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", fmt.Errorf("invalid token_cap %q", v)
		}
		patch.tokenCap = uptr(uint64(n))
	}
	if v, ok := in.Lookup("cohort_cost_cap"); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", fmt.Errorf("invalid cohort_cost_cap %q", v)
		}
		patch.cohortCostCap = fptr(f)
	}
	if v, ok := in.Lookup("cohort_token_cap"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", fmt.Errorf("invalid cohort_token_cap %q", v)
		}
		patch.cohortTokenCap = uptr(uint64(n))
	}
	usageFor := func(s state.Session) (float64, uint64) {
		entries := state.ReadTranscript(state.TranscriptPath(h.partDir, s.Name), 0)
		u, _ := aggregateUsage(entries)
		if u == nil {
			return 0, 0
		}
		return u.Cost, uint64(u.TokensIn + u.TokensOut)
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
		// Cohort-level pause re-eval against the (possibly) new cohort caps.
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

// stampBudget copies the session's persisted caps + paused reason onto the row's
// usage. If the row has no usage yet (no transcript), it synthesises an empty one
// so caps + paused_reason still reach the app.
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

// coordinationJSON is the wire shape of a session's join state in session list.
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

// sessionBanner renders the shared context body posted on session create.
// worktree=="" means no isolated worktree was made; shared distinguishes an
// explicit shared:true run (main checkout) from a non-git fallback. branch is
// the real (possibly instanceID-namespaced) branch produced by the worktreer.
func sessionBanner(repo, name, worktree, branch, cmd string, shared bool) string {
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
		b += "• Branch: — (runs on current branch)\n"
	default:
		b += "• Mode: shared (not a git repo)\n"
	}
	b += fmt.Sprintf("• Command: `%s`", cmd)
	return b
}

func (h *Handler) sessionCreateRun(ctx context.Context, in contracts.Input) (string, error) {
	raw, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid name %q — use letters, digits, - or _ (max 64, no /, spaces or ..)", raw)
	}
	if _, exists := h.st.FindSession(name); exists {
		return "", fmt.Errorf("session %q already exists", name)
	}
	if n := len(h.st.SnapshotSessions()); n >= maxSessions {
		return "", fmt.Errorf("session limit reached (%d) — close a session before creating another", maxSessions)
	}
	terminalOnly := in.Bool("terminal_only")
	home := h.st.Home
	admin := h.d
	// A terminal-only session (the TUI's own tabs) is bound to the terminal
	// gateway, not the operator's home gateway: route it through the terminal
	// admin and a synthetic terminal home so it becomes a local `terminal/…`
	// channel with no remote thread — working whether a home is configured or
	// none is set at all.
	if terminalOnly && h.td != nil {
		home = state.HomeRef{ID: "terminal", Type: "terminal"}
		admin = h.td
	}
	if home.ID == "" {
		return "", fmt.Errorf("no home set — run `set home` first")
	}
	cmd := h.defaultCmd
	cmdExplicit := false
	if c, ok := in.Lookup("cmd"); ok && c != "" {
		cmd = c
		cmdExplicit = true
	}
	backend, _ := in.Lookup("backend")
	if backend == "" {
		backend = "stream" // default backend: persistent claude stream-json
	}
	agentName, _ := in.Lookup("agent")
	vendor, _ := in.Lookup("vendor")
	modelID, _ := in.Lookup("model")
	// Validate at creation: an unknown or policy-excluded id persisted here would
	// only surface much later, as an opaque spawn failure on the first turn.
	if err := h.checkModel(modelID); err != nil {
		return "", err
	}
	parent, _ := in.Lookup("parent")
	// P1 learning (opt-in): extractor names a registered curation extractor; the
	// journal/cadence feed its Consolidate. Persisted on the session and threaded
	// to the bridge by the supervisor, like project/agent scope.
	extractor, _ := in.Lookup("extractor")
	journal, _ := in.Lookup("journal")
	consolidateEvery := 0
	if v, ok := in.Lookup("consolidate_every"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", fmt.Errorf("invalid consolidate_every %q — use a non-negative integer", v)
		}
		consolidateEvery = n
	}
	gwList, _ := in.Lookup("gateways")
	gateways := ParseGateways(gwList, terminalOnly, h.defaultGateways)
	// Budget caps (0 = uncapped). Mirrors consolidate_every's guarded parse.
	var costCap, cohortCostCap float64
	var tokenCap, cohortTokenCap uint64
	if v, ok := in.Lookup("cost_cap"); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", fmt.Errorf("invalid cost_cap %q", v)
		}
		costCap = f
	}
	if v, ok := in.Lookup("token_cap"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", fmt.Errorf("invalid token_cap %q — use a non-negative integer", v)
		}
		tokenCap = uint64(n)
	}
	if v, ok := in.Lookup("cohort_cost_cap"); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", fmt.Errorf("invalid cohort_cost_cap %q", v)
		}
		cohortCostCap = f
	}
	if v, ok := in.Lookup("cohort_token_cap"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", fmt.Errorf("invalid cohort_token_cap %q — use a non-negative integer", v)
		}
		cohortTokenCap = uint64(n)
	}
	ws := h.st.WorkspaceRoot()
	// Project is the logical label the caller assigns; Neublox filters workspace
	// snapshots by it, so it must be recorded even in cwd mode. It doubles as a
	// workspace sub-dir (steering the repo path) ONLY when a workspace root is
	// configured — see repoFor. Always capture it up front; the clone path below
	// overrides it with the cloned dir's basename.
	project, _ := in.Lookup("project")
	if ws != "" {
		if spec, ok := in.Lookup("clone"); ok && spec != "" {
			cctx, cancel := context.WithTimeout(ctx, cloneTimeout)
			dir, err := h.fg.Clone(cctx, spec, ws)
			cancel()
			if err != nil {
				return "", fmt.Errorf("clone: %w", err)
			}
			project = filepath.Base(dir)
		}
		// A session without a project is no longer an error: it runs rooted at the
		// workspace root (repoFor with an empty project), and with no workspace root
		// at the daemon's cwd — i.e. the directory `herrscher` was launched from.
		// This is what lets the TUI auto-create its `main` tab and lets tabs be
		// opened pwd-relative. A given project is still validated.
		if project != "" && !projectRe.MatchString(project) {
			return "", fmt.Errorf("invalid project %q — use a single name (no /, spaces, or ..)", project)
		}
	}
	repo := repoFor(ws, project)
	// Worktree isolation by default; shared:true runs in the main checkout.
	shared := in.Bool("shared")
	var worktree string
	if !shared {
		base := ""
		if b, ok := in.Lookup("base"); ok {
			base = b
		}
		path, err := h.wt.Create(repo, name, base)
		if err != nil {
			return "", fmt.Errorf("worktree: %w", err)
		}
		worktree = path // "" means non-git fallback
	}
	// rollbackWorktree removes the worktree we just made when a later step fails;
	// the removal error is logged but never masks the original failure.
	rollbackWorktree := func() {
		if rmErr := h.wt.Remove(repo, name, true); rmErr != nil {
			fmt.Fprintf(os.Stderr, "herrscher: worktree rollback for %q failed: %v\n", name, rmErr)
		}
	}
	// Agent provisioning: an agent companion needs a disposable, isolated worktree
	// (session close removes it), so reject shared/non-git, then materialize the
	// agent's persona + MCP + settings into it before anything outward (channel)
	// is created.
	if agentName != "" {
		if shared || worktree == "" {
			return "", fmt.Errorf("session create with agent %q needs an isolated git worktree (use a git repo and drop shared:true)", agentName)
		}
		a, found := h.agents.Get(agentName)
		if !found {
			rollbackWorktree()
			return "", fmt.Errorf("unknown agent %q — create it with `agent create %s`", agentName, agentName)
		}
		if vendor == "" {
			vendor = a.Backend
		}
		if !cmdExplicit && a.Cmd != "" {
			cmd = a.Cmd
		}
		if err := a.Materialize(worktree); err != nil {
			rollbackWorktree()
			return "", fmt.Errorf("provision agent %q: %w", agentName, err)
		}
	}
	// Logical name stays the state/worktree key; the qualified name namespaces
	// the channel title so daemons sharing a home stay distinguishable.
	title := h.st.QualifiedName(name)
	// runDir is the child bridge's working directory: the isolated worktree when
	// one was made, else the resolved repo (workspace root, optionally /project).
	// Empty means "inherit the launcher's cwd" — the pwd fallback for a session
	// created with no workspace root configured.
	runDir := worktree
	if runDir == "" {
		runDir = repo
	}
	var sess state.Session
	switch home.Type {
	case "category", "terminal":
		chID, err := admin.CreateUnder(ctx, home.ID, title)
		if err != nil {
			rollbackWorktree()
			return "", fmt.Errorf("create channel: %w", err)
		}
		sess = state.Session{Name: name, ChannelID: chID, Type: "text", Cmd: cmd, Backend: backend, Vendor: vendor, ModelID: modelID, Worktree: worktree, Dir: runDir, Project: project, Agent: agentName, Parent: parent, Gateways: gateways, Extractor: extractor, Journal: journal, ConsolidateEvery: consolidateEvery, CostCap: costCap, TokenCap: tokenCap, CohortCostCap: cohortCostCap, CohortTokenCap: cohortTokenCap}
	case "forum":
		chID, err := admin.ForumPost(ctx, home.ID, title, "Session **"+title+"** started.")
		if err != nil {
			rollbackWorktree()
			return "", fmt.Errorf("create forum post: %w", err)
		}
		sess = state.Session{Name: name, ChannelID: chID, Type: "forum", Cmd: cmd, Backend: backend, Vendor: vendor, ModelID: modelID, Worktree: worktree, Dir: runDir, Project: project, Agent: agentName, Parent: parent, Gateways: gateways, Extractor: extractor, Journal: journal, ConsolidateEvery: consolidateEvery, CostCap: costCap, TokenCap: tokenCap, CohortCostCap: cohortCostCap, CohortTokenCap: cohortTokenCap}
	default:
		return "", fmt.Errorf("home type %q unsupported", home.Type)
	}
	if err := h.st.AddSession(sess); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	if err := h.sup.Start(sess); err != nil {
		return "", fmt.Errorf("start bridge: %w", err)
	}
	banner := sessionBanner(repo, name, worktree, h.wt.Branch(name), cmd, shared)
	_ = admin.Send(ctx, sess.ChannelID, banner) // best-effort; reply is source of truth
	return fmt.Sprintf("✅ Running on %s.\n\n%s", admin.ChannelRef(sess.ChannelID), banner), nil
}

// adminFor returns the channel admin that owns sess's channel: the terminal
// admin for synthetic `terminal/…` channels (when a terminal gateway is bound),
// else the home gateway's admin. This keeps close/list routing symmetric with
// create.
func (h *Handler) adminFor(sess state.Session) channelAdmin {
	if h.td != nil && strings.HasPrefix(sess.ChannelID, "terminal/") {
		return h.td
	}
	return h.d
}

func (h *Handler) sessionCloseRun(ctx context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", fmt.Errorf("no session %q", name)
	}
	_ = h.sup.Stop(name)
	if sess.Worktree != "" {
		force := in.Bool("force")
		repo := repoFor(h.st.WorkspaceRoot(), sess.Project)
		if err := h.wt.Remove(repo, name, force); err != nil {
			return "", fmt.Errorf("%w — commit, or close with force:true to discard (branch session/%s is kept)", err, name)
		}
	}
	if err := h.adminFor(sess).Archive(ctx, sess.ChannelID); err != nil {
		return "", fmt.Errorf("archive: %w", err)
	}
	if err := h.st.RemoveSession(name); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	_ = state.RemoveParticipantJournal(state.ParticipantsPath(h.partDir, name))
	_ = state.RemoveTranscript(state.TranscriptPath(h.partDir, name))
	return fmt.Sprintf("🗄️ Session **%s** closed.", name), nil
}

// sessionArchiveRun closes-but-keeps a session: it stops the supervised child
// and archives the channel, but keeps the row + transcript + ResumeToken and
// marks it archived so /resume can revive it (with backend context and
// scrollback). The worktree is kept — archive is non-destructive.
func (h *Handler) sessionArchiveRun(ctx context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", fmt.Errorf("no session %q", name)
	}
	_ = h.sup.Stop(name)
	if err := h.adminFor(sess).Archive(ctx, sess.ChannelID); err != nil {
		return "", fmt.Errorf("archive: %w", err)
	}
	if err := h.st.SetArchived(name, true); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return fmt.Sprintf("📦 Session **%s** archived — resume it from /resume.", name), nil
}

// sessionResumeRun revives an archived session: it clears the archived flag and
// restarts the supervised child. The session control socket is re-established by
// the reconcile that follows every hub.Dispatch (reconcile brings non-archived
// persisted sessions live); the supervised backend child is restarted here,
// mirroring how sessionArchiveRun stops it. Modeled on hub.Resume
// (core/host/hub.go) but host-agnostic. Name is NOT slugified: FindSession is the
// guard (an existing session's name is already a persisted slug), exactly like
// sessionArchiveRun.
func (h *Handler) sessionResumeRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", fmt.Errorf("no session %q", name)
	}
	if err := h.st.SetArchived(name, false); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	sess.Archived = false
	if err := h.sup.Start(sess); err != nil {
		return "", fmt.Errorf("start: %w", err)
	}
	return fmt.Sprintf("⟲ Session **%s** resumed.", name), nil
}

// sessionSwitchRun re-targets a live session's vendor/model/effort under the
// same id. It always rewrites the persisted backend (SetBackendTarget, which
// also clears the resume token so the new backend starts a fresh thread). With
// handoff != "none" it also stops the running child, restarts it on the new
// Cmd/Vendor, and injects a context seed built from the prior transcript so the
// new backend picks up the conversation. Name is NOT slugified: FindSession is
// the guard, exactly like sessionResumeRun.
func (h *Handler) sessionSwitchRun(ctx context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	vendor := in.Get("vendor")
	cmd := in.Get("cmd")
	handoff := in.Get("handoff")
	if handoff == "" {
		handoff = "none"
	}
	prior, exists := h.st.FindSession(name)
	if !exists {
		return "", fmt.Errorf("no session %q", name)
	}
	// An ABSENT --model keeps the session's current model; only an explicitly
	// supplied value retargets it (and an explicit empty value clears it back to
	// the legacy cmd-carries-the-model path). Re-targeting just --cmd must not
	// silently drop a gateway session off the routed path — that would degrade
	// away from the safe direction.
	modelID := prior.ModelID
	if v, supplied := in.Lookup("model"); supplied {
		if err := h.checkModel(v); err != nil {
			return "", err
		}
		modelID = v
	}
	// Capture the prior backend so we can roll back if the restart fails: rolling
	// back to the old vendor makes the old resume token valid again.
	oldVendor, oldCmd, oldModelID, oldToken := prior.Vendor, prior.Cmd, prior.ModelID, prior.ResumeToken
	if !h.st.SetBackendTarget(name, vendor, cmd, modelID) {
		return "", fmt.Errorf("no session %q", name)
	}
	sess, _ := h.st.FindSession(name) // re-read: Vendor/Cmd/ModelID/ResumeToken just changed
	// Replace the targeted bridge synchronously for ALL handoff modes so the old
	// process is gone before the new Cmd/Vendor starts. Only seeding is optional.
	if err := h.sup.Restart(sess); err != nil {
		if rbErr := h.rollbackSwitch(name, oldVendor, oldCmd, oldModelID, oldToken); rbErr != nil {
			return "", fmt.Errorf("redémarrage backend: %w; rollback échoué, session %s hors service: %v", err, name, rbErr)
		}
		return "", fmt.Errorf("redémarrage backend: %w (session %s restaurée sur %s)", err, name, oldVendor)
	}
	if handoff == "none" {
		return fmt.Sprintf("session %s re-ciblée sur %s (sans reprise)", name, vendor), nil
	}
	seedText := buildHandoffSeed(
		state.ReadTranscript(state.TranscriptPath(h.partDir, name), sessionLogTranscriptCap),
		handoff,
	)
	if seedText != "" && h.seed != nil && !h.injectSeed(ctx, name, seedText) {
		// Restart succeeded but the seed never landed: report honestly rather
		// than claiming a handoff the new backend never received.
		return fmt.Sprintf("session %s re-ciblée sur %s (reprise %s non confirmée)", name, vendor, handoff), nil
	}
	return fmt.Sprintf("session %s re-ciblée sur %s (reprise %s)", name, vendor, handoff), nil
}

// injectSeed retries the live-session seed: the restarted bridge registers its
// driver asynchronously, so the first attempt can land before it is live. Mirrors
// the coordinator's seed-with-retry (bounded). Returns true once the seed lands,
// false if every attempt failed or the context was cancelled.
func (h *Handler) injectSeed(ctx context.Context, name, task string) bool {
	for attempt := 0; attempt < 20; attempt++ {
		if h.seed(name, task) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(150 * time.Millisecond):
		}
	}
	return false
}

// rollbackSwitch restores a session to its prior backend after a failed restart:
// it re-points the persisted target to the old vendor/cmd, restores the old resume
// token (valid again now that the vendor is back), and restarts the old backend so
// the thread is live again. A non-nil return means the rollback restart also failed
// and the session is down.
func (h *Handler) rollbackSwitch(name, oldVendor, oldCmd, oldModelID, oldToken string) error {
	h.st.SetBackendTarget(name, oldVendor, oldCmd, oldModelID) // also clears the token
	_ = h.st.SetResumeToken(name, oldToken)                    // restore it
	sess, _ := h.st.FindSession(name)
	return h.sup.Restart(sess)
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
	out := "Active sessions:\n"
	for _, s := range sessions {
		out += fmt.Sprintf("• **%s** (%s) %s\n", s.Name, s.Type, h.adminFor(s).ChannelRef(s.ChannelID))
	}
	return out, nil
}

func (h *Handler) sessionWhoRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return "", fmt.Errorf("no session %q", name)
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
	out := fmt.Sprintf("Participants observed in **%s**:\n", name)
	for _, id := range ids {
		out += fmt.Sprintf("• <@%s>\n", id)
	}
	return out, nil
}

// sessionLogTranscriptCap bounds how many transcript entries `session log`
// replays by default. Mirrors host.scrollbackCap (200), duplicated here because
// that const lives in the host package and is not importable from manager.
const sessionLogTranscriptCap = 200

// sessionLogRun emits a session's recorded transcript as scrollback. In JSON
// mode (what the Neublox daemon dispatches) it returns a JSON array of
// {ts,role,text,cost}, oldest-first, capped. A never-run session has no file →
// empty array, not an error (a fresh session legitimately has no history).
func (h *Handler) sessionLogRun(_ context.Context, in contracts.Input) (string, error) {
	raw, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	// Sanitize like `session create`: slugify + validate. A crafted name such as
	// "../../etc/passwd" thus can never traverse out of the transcripts dir, and
	// an invalid or never-recorded name simply has no history → empty, not error.
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
			entries = []state.TranscriptEntry{} // marshal [] not null
		}
		b, err := json.Marshal(entries)
		return string(b), err
	}
	if len(entries) == 0 {
		return "No transcript yet.", nil
	}
	out := ""
	for _, e := range entries {
		out += fmt.Sprintf("**%s** %s\n", e.Role, e.Text)
	}
	return out, nil
}
