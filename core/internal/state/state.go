package state

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Herrscherd/herrscher/core/internal/schedule"
)

// HomeRef points at the category or forum that holds session channels.
type HomeRef struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "category" | "forum"
}

// Session is one bridged channel/post supervised by the daemon.
type Session struct {
	ID string `json:"id,omitempty"` // stable logical id, decoupled from Name and ChannelID
	// Incarnation is an opaque identity for this specific persisted session
	// object. Closing and recreating the same Name generates a different value.
	Incarnation string `json:"incarnation,omitempty"`
	Name        string `json:"name"`
	ChannelID   string `json:"channelID"`
	Type        string `json:"type"` // "text" | "forum"
	Cmd         string `json:"cmd"`
	Backend     string `json:"backend,omitempty"` // bridge backend ("" or "stream" = stream-json default; "oneshot" = per-message cmd)
	Vendor      string `json:"vendor,omitempty"`  // agent backend vendor ("claude", "codex", "cursor")
	// ModelID is the catalog identifier of the chosen model. Unlike Cmd, which is
	// an opaque invocation string, it lets a resume look up the model's ROUTE —
	// so we know whether gateway credentials need to be re-injected. Empty for
	// sessions created before the catalog existed.
	ModelID  string `json:"modelId,omitempty"`
	Worktree string `json:"worktree,omitempty"` // abs path; empty for a shared session
	Dir      string `json:"dir,omitempty"`      // bridge working dir; empty = inherit launcher cwd (pwd fallback)
	Project  string `json:"project,omitempty"`  // workspace sub-dir the session started from
	Agent    string `json:"agent,omitempty"`    // durable agent this session was provisioned from ("" = none)
	// MemoryProject and MemoryAgent are the memory roots this session files what
	// it learns under. They are separate from Project and Agent on purpose: those
	// two place the session — a workspace sub-directory, a provisioned worktree —
	// while these only say where knowledge goes. Empty means the session
	// contributes to no root, which is every session created before this existed.
	MemoryProject string `json:"memoryProject,omitempty"`
	MemoryAgent   string `json:"memoryAgent,omitempty"`
	// ProjectPinned marks MemoryProject as a human's choice. A host guess is left
	// unpinned so the session's first prompt may revise it, once.
	ProjectPinned bool `json:"projectPinned,omitempty"`

	// ResumeToken is the backend's opaque resume id, folded in from each turn's
	// reply so a restart can resume the conversation with --resume. Empty =
	// start fresh.
	ResumeToken string `json:"resumeToken,omitempty"`

	// Archived marks a session closed-but-kept (session archive): its row,
	// transcript and ResumeToken are retained so /resume can revive it, but the
	// boot loop does not auto-supervise it. Absent/false = live as today.
	Archived bool `json:"archived,omitempty"`

	// Owned marks a channel this session created, and may therefore tidy away
	// when it closes. A session started in a conversation that already existed
	// binds to it instead, and that channel belongs to the people who were
	// already talking in it — closing the session must leave it alone. Absent on
	// a row written before this field existed, which is the safe direction: the
	// worst outcome is a channel left behind.
	Owned bool `json:"owned,omitempty"`

	// Learning config (P1 write side, opt-in). Extractor names a registered
	// curation extractor; empty keeps the plain Curator (no learning). Journal
	// is the call-journal path Consolidate reads (worktree-relative is fine).
	// ConsolidateEvery runs Consolidate every N turns (0 = manual only).
	Extractor        string `json:"extractor,omitempty"`
	Journal          string `json:"journal,omitempty"`
	ConsolidateEvery int    `json:"consolidateEvery,omitempty"`

	// Gateways binds the session to a set of gateway kinds (e.g. "chat",
	// "terminal"). Empty means "legacy": a pre-multi-gateway session whose
	// binding the host resolves from the gateways actually built (see IsLegacy).
	Gateways []string `json:"gateways,omitempty"`

	Participants []string `json:"participants,omitempty"` // observed authors (cache; journal is source of truth)

	// Parent names the lead session that delegated this one (result-back P3).
	// Empty = no parent. The coordinator reads it to find the delivery target
	// of this session's completion report (Report).
	Parent string `json:"parent,omitempty"`

	// Budget caps (0 = uncapped), persisted from contracts.CreateSession.
	CostCap        float64 `json:"cost_cap,omitempty"`
	TokenCap       uint64  `json:"token_cap,omitempty"`
	CohortCostCap  float64 `json:"cohort_cost_cap,omitempty"`
	CohortTokenCap uint64  `json:"cohort_token_cap,omitempty"`
	// PausedReason is non-empty when the session was halted by a budget cap:
	// "cost" | "tokens" | "cohort_cost" | "cohort_tokens". Cleared on resume.
	PausedReason string `json:"paused_reason,omitempty"`
}

// BoundGateways returns the explicit gateway kinds this session is bound to, or
// nil when it is legacy (no stored set). The state package stays platform-blind:
// it never names a concrete gateway. A legacy session's effective binding is
// resolved by the host against the gateways actually built (see IsLegacy).
func (s Session) BoundGateways() []string {
	return append([]string(nil), s.Gateways...)
}

// IsLegacy reports whether this session predates explicit gateway binding: it
// carries a channel but no stored gateway set. The host binds such a session to
// the primary (non-terminal) gateways present, reproducing the original
// single-gateway behavior without the core naming that gateway.
func (s Session) IsLegacy() bool {
	return len(s.Gateways) == 0 && s.ChannelID != ""
}

// State is the daemon's persisted configuration. All access is mutex-guarded.
type State struct {
	mu              sync.Mutex `json:"-"`
	path            string     `json:"-"`
	Home            HomeRef    `json:"home"`
	Repo            string     `json:"repo,omitempty"`      // legacy single-repo root; defaults to daemon cwd
	Workspace       string     `json:"workspace,omitempty"` // abs path to the workspace root; preferred over Repo
	Source          string     `json:"source,omitempty"`    // abs path to the herrscher source checkout (for /service update)
	Sessions        []Session  `json:"sessions"`
	StatusMessageID string     `json:"statusMessageID,omitempty"` // cached id of the status embed
	InstanceID      string     `json:"instanceID,omitempty"`      // per-daemon namespace for global resources; "" = legacy
	// Schedules holds the proactive schedules. They live here rather than in a
	// store of their own because state.json is already rewritten on every turn
	// (each reply folds its resume token back in): putting them here does not
	// change the write profile, and they inherit the existing mutex and atomic
	// write. See schedules.go for the accessors.
	Schedules []schedule.Schedule `json:"schedules,omitempty"`
}

// NewState returns an empty state bound to path (not yet written).
func NewState(path string) *State { return &State{path: path} }

// ApplyDefaults seeds declarative config.json values into the in-memory state
// for any field not already set, WITHOUT persisting. This keeps config.json the
// source of truth for unset fields while a live /set (which persists to
// state.json) always wins: persisted state > config > empty. Because it never
// writes, removing a value from config.json takes effect on the next restart.
func (s *State) ApplyDefaults(home *HomeRef, workspace, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Home.ID == "" && home != nil && home.ID != "" {
		s.Home = *home
	}
	if s.Workspace == "" {
		s.Workspace = workspace
	}
	if s.Source == "" {
		s.Source = source
	}
}

// LoadState reads state from path; a missing file yields an empty state.
func LoadState(path string) (*State, error) {
	s := NewState(path)
	buf, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(buf, s); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range s.Sessions {
		if s.Sessions[i].ID != "" {
			seen[s.Sessions[i].ID] = true
		}
	}
	for i := range s.Sessions {
		if s.Sessions[i].ID == "" {
			s.Sessions[i].ID = newSessionID(s.Sessions[i].Name, seen)
			seen[s.Sessions[i].ID] = true
		}
	}
	if s.ensureSessionIncarnationsLocked() {
		if err := s.saveLocked(); err != nil {
			return nil, fmt.Errorf("persist session incarnation backfill: %w", err)
		}
	}
	return s, nil
}

// newSessionID returns a stable id for a session. Name is already unique and
// git-safe, so it seeds a readable id; a numeric disambiguator keeps ids stable
// if a name is later reused.
func newSessionID(name string, existing map[string]bool) string {
	base := "s_" + name
	id := base
	for n := 1; existing[id]; n++ {
		id = fmt.Sprintf("%s_%d", base, n)
	}
	return id
}

func newSessionIncarnation(existing map[string]bool) string {
	for {
		id := "inc_" + rand.Text()
		if !existing[id] {
			return id
		}
	}
}

// ensureSessionIncarnationsLocked assigns a unique incarnation to every
// session. The caller must hold s.mu.
func (s *State) ensureSessionIncarnationsLocked() bool {
	incarnations := map[string]bool{}
	dirty := false
	for i := range s.Sessions {
		incarnation := s.Sessions[i].Incarnation
		if incarnation == "" || incarnations[incarnation] {
			incarnation = newSessionIncarnation(incarnations)
			s.Sessions[i].Incarnation = incarnation
			dirty = true
		}
		incarnations[incarnation] = true
	}
	return dirty
}

// Save atomically writes state to its path.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *State) saveLocked() error {
	s.ensureSessionIncarnationsLocked()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// FindSession returns the session with name (and whether it exists).
func (s *State) FindSession(name string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ss := range s.Sessions {
		if ss.Name == name {
			return ss, true
		}
	}
	return Session{}, false
}

// AddSession adds a session, erroring if the name is taken, and persists.
func (s *State) AddSession(sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ss := range s.Sessions {
		if ss.Name == sess.Name {
			return fmt.Errorf("session %q already exists", sess.Name)
		}
	}
	if sess.ID == "" {
		seen := map[string]bool{}
		for _, e := range s.Sessions {
			seen[e.ID] = true
		}
		sess.ID = newSessionID(sess.Name, seen)
	}
	// AddSession always creates a new persisted object. Never let a caller
	// accidentally reuse the identity of a removed session with the same name.
	sess.Incarnation = ""
	s.Sessions = append(s.Sessions, sess)
	return s.saveLocked()
}

// RemoveSession drops the session named name and persists.
func (s *State) RemoveSession(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Sessions[:0]
	for _, ss := range s.Sessions {
		if ss.Name != name {
			out = append(out, ss)
		}
	}
	s.Sessions = out
	return s.saveLocked()
}

// SetResumeToken records the backend resume token for the named session,
// persisting only when it changes. Turns report the same id, so this avoids
// rewriting state.json every turn. A missing session or an unchanged token is a
// no-op.
func (s *State) SetResumeToken(name, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].Name == name {
			if s.Sessions[i].ResumeToken == token {
				return nil
			}
			s.Sessions[i].ResumeToken = token
			return s.saveLocked()
		}
	}
	return nil
}

// MemoryRoots answers which project and agent this session files what it learns
// under. A memory root wins over the placement field of the same name: Project
// may be steering the session into a workspace sub-directory and Agent may be
// demanding an isolated worktree, and neither says where knowledge goes. Every
// path that scopes memory — the supervisor spawning a bridge, the daemon running
// a one-shot seed — asks here, so the two cannot drift.
func (s Session) MemoryRoots() (project, agent string) {
	project, agent = s.MemoryProject, s.MemoryAgent
	if project == "" {
		project = s.Project
	}
	if agent == "" {
		agent = s.Agent
	}
	return project, agent
}

// SetProjectPinned records the memory project a session settled on and marks it
// chosen, so no later turn re-opens the question. Mirrors SetResumeToken's
// locking and persistence: a missing session, or a project already pinned to the
// same value, is a no-op rather than a rewrite of state.json.
func (s *State) SetProjectPinned(name, project string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].Name != name {
			continue
		}
		if s.Sessions[i].MemoryProject == project && s.Sessions[i].ProjectPinned {
			return nil
		}
		s.Sessions[i].MemoryProject = project
		s.Sessions[i].ProjectPinned = true
		return s.saveLocked()
	}
	return nil
}

// SetPausedReason records why a session halted on a budget cap ("" clears it,
// resuming the session). Mirrors SetResumeToken's locking + persistence.
func (s *State) SetPausedReason(name, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].Name == name {
			if s.Sessions[i].PausedReason == reason {
				return nil
			}
			s.Sessions[i].PausedReason = reason
			return s.saveLocked()
		}
	}
	return nil
}

// SetBudget persists the budget caps and paused reason for the named session
// in one write. Mirrors SetResumeToken/SetPausedReason's locking + persistence.
// Used by session set-budget so raising a cap (which clears PausedReason)
// survives restart. A missing session is a no-op.
func (s *State) SetBudget(name string, costCap float64, tokenCap uint64, cohortCostCap float64, cohortTokenCap uint64, pausedReason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].Name == name {
			s.Sessions[i].CostCap = costCap
			s.Sessions[i].TokenCap = tokenCap
			s.Sessions[i].CohortCostCap = cohortCostCap
			s.Sessions[i].CohortTokenCap = cohortTokenCap
			s.Sessions[i].PausedReason = pausedReason
			return s.saveLocked()
		}
	}
	return nil
}

// SetBackendTarget re-points a live session's backend: it rewrites Vendor and the
// opaque Cmd string (which carries model+effort) and clears the resume token,
// which is backend-specific and meaningless once the vendor/model changes. The
// caller restarts the supervised child so the new values take effect. Returns
// false when no session matches name.
func (s *State) SetBackendTarget(name, vendor, cmd, modelID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].Name == name {
			s.Sessions[i].Vendor = vendor
			s.Sessions[i].Cmd = cmd
			s.Sessions[i].ModelID = modelID
			s.Sessions[i].ResumeToken = ""
			s.saveLocked()
			return true
		}
	}
	return false
}

// SetArchived sets a session's archived flag and persists only on change. An
// unknown name is a no-op (best-effort, mirrors SetResumeToken).
func (s *State) SetArchived(name string, archived bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].Name == name {
			if s.Sessions[i].Archived == archived {
				return nil
			}
			s.Sessions[i].Archived = archived
			return s.saveLocked()
		}
	}
	return nil
}

// SetHome records the home ref and persists.
func (s *State) SetHome(h HomeRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Home = h
	return s.saveLocked()
}

// SetWorkspace records the workspace root and persists.
func (s *State) SetWorkspace(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Workspace = path
	return s.saveLocked()
}

// WorkspaceRoot returns the configured workspace, else the legacy Repo, else "".
func (s *State) WorkspaceRoot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Workspace != "" {
		return s.Workspace
	}
	return s.Repo
}

// SetSource records the herrscher source-checkout path and persists.
func (s *State) SetSource(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Source = path
	return s.saveLocked()
}

// SourceDir returns the configured herrscher source checkout, else "".
func (s *State) SourceDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Source
}

// SetStatusMessageID caches the status embed's message id and persists.
func (s *State) SetStatusMessageID(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.StatusMessageID = id
	return s.saveLocked()
}

// SnapshotSessions returns a copy of the current sessions.
func (s *State) SnapshotSessions() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Session(nil), s.Sessions...)
}

// QualifiedName maps a logical session name to the name used on global resources
// (channel title): "<InstanceID>__<name>". In legacy mode (empty InstanceID) it
// returns the bare logical name, preserving pre-namespacing behavior.
func (s *State) QualifiedName(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.InstanceID == "" {
		return name
	}
	return s.InstanceID + "__" + name
}

// SetInstanceID records the per-daemon instance id and persists. The id is meant
// to be frozen after first resolution; callers enforce that invariant.
func (s *State) SetInstanceID(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InstanceID = id
	return s.saveLocked()
}
