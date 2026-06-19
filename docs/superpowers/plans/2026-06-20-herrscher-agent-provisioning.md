# Herrscher Agent + Provisioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give herrscher a durable **Agent** entity and an **agent-aware session-provisioning seam** so that `session create --agent <name>` materializes the agent's persona + MCP + settings into the session worktree, with zero changes to the backend argv.

**Architecture:** A new generic `core/internal/agent` package owns agent homes on disk (`<stateDir>/agents/<name>/`) holding `SOUL.md` + `mcp.json` + `settings.json`, and a `Materialize(worktree)` that copies them into the worktree as `.claude/CLAUDE.md` + `.mcp.json` + `.claude/settings.json` (the files Claude Code auto-reads when cwd = worktree). The `manager.Handler` gains `agent create`/`agent list` commands and a `--agent` param on `session create` that invokes `Materialize` right after the worktree is created. The Agent model is domain-neutral — Neublox is just a profile that passes a Roblox `--soul` and `--mcp "neublox serve …"`.

**Tech Stack:** Go (stdlib only: `encoding/json`, `os`, `path/filepath`, `strings`, `sort`). Module root `/home/shan/dev/herrscher`, module path `github.com/Herrscherd/herrscher`. Command surface via `github.com/Herrscherd/herrscher-contracts` (`contracts.Cmd`/`Input` builder). Tests are Go `testing` with `t.TempDir()`, run from the repo root.

---

## Background — what already exists (verified in code)

- **Command catalog** — `core/internal/manager/commands.go` declares every command via the `contracts.New(ns, verb).Help(…).Param(name, help, required).Do(handler)` builder. Adding a command here reaches every UI (Discord/terminal) for free — they all dispatch through `contracts.SessionControl.Dispatch(argv)`.
- **Session create/close** — `core/internal/manager/session.go`. `sessionCreateRun` reads params via `in.Lookup("name")` / `in.Bool("shared")`, creates the worktree at line 97 (`path, err := h.wt.Create(repo, name)` → `worktree = path` at line 101), builds two `state.Session{…}` literals (lines 116 & 125), persists (`h.st.AddSession`), and starts the bridge. It already has worktree-rollback blocks (lines 111–113, 120–122) when channel creation fails. `sessionCloseRun` removes the worktree (line 153), so anything written into the worktree is disposable — the durable agent store must live **outside** it.
- **Backend runs in the worktree** — `herrscher-claude-backend/stream.go` sets `cmd.Dir = <worktree>`, so a `.mcp.json` / `.claude/` written into the worktree is auto-read. No argv change needed.
- **State** — `core/internal/state/state.go`. `type Session struct` (line 18) has `Name/ChannelID/Type/Cmd/Backend/Worktree/Project/Gateways/Participants`. Persistence is `Save()`/`saveLocked()` (atomic `MarshalIndent`, 0o600).
- **Handler wiring** — `core/internal/manager/handler.go`: `type Handler struct{ d, sup, wt, fg, up, st, defaultCmd, partDir }`; `NewHandler(d, sup, wt, fg, up, st, defaultCmd, partDir)`. Constructed in `core/host/cli.go:28` inside `buildRegistry`, where `partDir := filepath.Dir(o.StatePath)` (cli.go:23). `partDir` is the **state directory** (`~/.config/dctl/` by default, `$DCTL_STATE_DIR` override — see `serve.go:DefaultStatePath`); participant journals already live at `<partDir>/participants/`. **Agent homes will live at `<partDir>/agents/`** — beside `state.json`, NOT in `~/.herrscher/`.
- **Name safety** — `core/internal/manager/validate.go`: `slugify(name)` → safe slug; `sessionNameRe = ^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$` is the final guard. Reused for agent names.

## File Structure

- **Create** `core/internal/agent/agent.go` — `Agent` type + `Materialize(worktree)` + file-name/token constants. Pure FS, stdlib only.
- **Create** `core/internal/agent/store.go` — `Store` (`NewStore/Create/Get/List/Root`) + `CreateSpec` + the `mcp.json`/`settings.json` builders + default persona. Pure FS, stdlib only.
- **Create** `core/internal/agent/agent_test.go` — Materialize tests.
- **Create** `core/internal/agent/store_test.go` — Store tests.
- **Modify** `core/internal/state/state.go` — add `Agent string` field to `Session`.
- **Modify** `core/internal/manager/handler.go` — add `agents *agent.Store` field + `NewHandler` param + `Agents()` accessor.
- **Modify** `core/internal/manager/handler_test.go` — update `newTestHandlerWithUpdater` to construct a real store.
- **Create** `core/internal/manager/agent.go` — `agentCreateRun` / `agentListRun` handlers.
- **Create** `core/internal/manager/agent_test.go` — handler tests + the end-to-end provisioning test.
- **Modify** `core/internal/manager/commands.go` — declare `agent create`/`agent list`; add `--agent` param to `session create`.
- **Modify** `core/internal/manager/session.go` — read `agent`, guard, invoke `Materialize`, persist `Agent` on the session.
- **Modify** `core/host/cli.go` — construct the store and pass it to `NewHandler`.

> Run all `go` commands from `/home/shan/dev/herrscher`.

---

## Task 1: Agent store — Create / Get / List + seeded templates

**Files:**
- Create: `core/internal/agent/store.go`
- Create: `core/internal/agent/agent.go` (constants only in this task; `Materialize` lands in Task 2)
- Test: `core/internal/agent/store_test.go`

- [ ] **Step 1: Write the failing tests**

Create `core/internal/agent/store_test.go`:

```go
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreCreateSeedsFiles(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "roblox", Soul: "You are Roblox.", MCP: "neublox serve --project {{WORKTREE}}"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "roblox" {
		t.Fatalf("name = %q", a.Name)
	}

	soul, err := os.ReadFile(filepath.Join(a.Home, "SOUL.md"))
	if err != nil || string(soul) != "You are Roblox." {
		t.Fatalf("SOUL.md = %q err=%v", soul, err)
	}

	mcp, err := os.ReadFile(filepath.Join(a.Home, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcp, &cfg); err != nil {
		t.Fatalf("mcp.json invalid: %v\n%s", err, mcp)
	}
	srv, ok := cfg.MCPServers["neublox"]
	if !ok || srv.Type != "stdio" || srv.Command != "neublox" {
		t.Fatalf("neublox server wrong: %+v", cfg.MCPServers)
	}
	if strings.Join(srv.Args, " ") != "serve --project {{WORKTREE}}" {
		t.Fatalf("args = %v", srv.Args)
	}

	settings, err := os.ReadFile(filepath.Join(a.Home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), `"enableAllProjectMcpServers": true`) {
		t.Fatalf("settings missing enable flag:\n%s", settings)
	}
	if !strings.Contains(string(settings), "mcp__neublox__*") {
		t.Fatalf("settings missing mcp allow:\n%s", settings)
	}
}

func TestStoreCreateDefaultSoulAndNoMCP(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	soul, _ := os.ReadFile(filepath.Join(a.Home, "SOUL.md"))
	if !strings.Contains(string(soul), "companion") {
		t.Fatalf("default soul not seeded:\n%s", soul)
	}
	mcp, _ := os.ReadFile(filepath.Join(a.Home, "mcp.json"))
	if !strings.Contains(string(mcp), `"mcpServers"`) || strings.Contains(string(mcp), "stdio") {
		t.Fatalf("expected empty mcpServers, got:\n%s", mcp)
	}
}

func TestStoreCreateDuplicate(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CreateSpec{Name: "x"}); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestStoreGetAndList(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, ok := s.Get("nope"); ok {
		t.Fatal("Get on missing should be false")
	}
	if _, err := s.Create(CreateSpec{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CreateSpec{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("List = %+v (want sorted a,b)", got)
	}
	if a, ok := s.Get("a"); !ok || a.Name != "a" {
		t.Fatalf("Get a = %+v ok=%v", a, ok)
	}
}

func TestStoreCreateRejectsBadName(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, n := range []string{"", "a/b", "..", "../x"} {
		if _, err := s.Create(CreateSpec{Name: n}); err == nil {
			t.Fatalf("name %q should be rejected", n)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/agent/...`
Expected: FAIL — build error (`undefined: NewStore`, `undefined: CreateSpec`).

- [ ] **Step 3: Write the constants file**

Create `core/internal/agent/agent.go`:

```go
// Package agent models a durable companion agent: a persistent home directory
// holding the agent's persona (SOUL.md), its MCP server declaration (mcp.json),
// and its Claude settings (settings.json). The agent is materialized into a
// disposable session worktree by Agent.Materialize, which copies those files
// into the worktree as the files Claude Code auto-reads when its cwd is the
// worktree (.claude/CLAUDE.md, .mcp.json, .claude/settings.json). The model is
// domain-neutral: callers (e.g. Neublox's Roblox profile) supply the persona and
// MCP server; the package only stores and materializes them.
package agent

// File names inside an agent home (the durable source of truth).
const (
	soulFile     = "SOUL.md"
	mcpFile      = "mcp.json"
	settingsFile = "settings.json"
)

// worktreeToken is replaced with the absolute worktree path when an agent is
// materialized, so an agent's mcp.json can point a server at the session's
// working directory without knowing it in advance.
const worktreeToken = "{{WORKTREE}}"

// Agent is a durable companion: a name and the home directory that stores its
// persona and provisioning files.
type Agent struct {
	Name string
	Home string // absolute path to the agent's home directory
}
```

- [ ] **Step 4: Write the store**

Create `core/internal/agent/store.go`:

```go
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store owns the directory holding every agent home: <root>/<name>/.
type Store struct{ root string }

// NewStore returns a Store rooted at root (created lazily on first Create).
func NewStore(root string) *Store { return &Store{root: root} }

// Root returns the directory under which agent homes live.
func (s *Store) Root() string { return s.root }

// CreateSpec declares a new agent. Soul is the persona text (SOUL.md); MCP is an
// optional stdio MCP server command line ("neublox serve --project {{WORKTREE}}")
// whose first token names the server and is its command.
type CreateSpec struct {
	Name string
	Soul string
	MCP  string
}

// defaultSoul is the persona seeded when CreateSpec.Soul is empty. It is a
// neutral companion persona; profiles pass their own via CreateSpec.Soul.
const defaultSoul = `# Companion

You are a durable companion agent. You keep working on this project across
sessions, remember what matters, and act carefully inside your worktree.
`

// mcpServer / mcpConfig model a Claude Code .mcp.json stdio server entry.
type mcpServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type mcpConfig struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

// parseMCP splits a command line ("neublox serve --project {{WORKTREE}}") into a
// stdio server entry. The first token is both the server name and the command.
// Whitespace-split only (no quoting) — sufficient for tokenless CLI args.
func parseMCP(cmdline string) (name string, srv mcpServer, ok bool) {
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return "", mcpServer{}, false
	}
	return fields[0], mcpServer{Type: "stdio", Command: fields[0], Args: fields[1:]}, true
}

// buildSettings renders the zero-prompt Claude settings: project MCP servers
// auto-enabled, file edits auto-accepted (a headless backend can answer no
// prompt), and (when present) the agent's MCP namespace allow-listed. The
// worktree is disposable and isolated, so a permissive mode is safe.
func buildSettings(serverName string) ([]byte, error) {
	allow := []string{}
	if serverName != "" {
		allow = append(allow, "mcp__"+serverName+"__*")
	}
	allow = append(allow, "Bash", "Edit", "Write")
	type perms struct {
		DefaultMode string   `json:"defaultMode"`
		Allow       []string `json:"allow"`
	}
	type cfg struct {
		EnableAllProjectMCPServers bool  `json:"enableAllProjectMcpServers"`
		Permissions                perms `json:"permissions"`
	}
	return json.MarshalIndent(cfg{
		EnableAllProjectMCPServers: true,
		Permissions:                perms{DefaultMode: "acceptEdits", Allow: allow},
	}, "", "  ")
}

// Create writes a new agent home and seeds its three source files. It errors if
// the name is unsafe or the agent already exists.
func (s *Store) Create(spec CreateSpec) (Agent, error) {
	name := spec.Name
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return Agent{}, fmt.Errorf("invalid agent name %q", name)
	}
	home := filepath.Join(s.root, name)
	if _, err := os.Stat(home); err == nil {
		return Agent{}, fmt.Errorf("agent %q already exists", name)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return Agent{}, fmt.Errorf("create agent home: %w", err)
	}

	soul := spec.Soul
	if soul == "" {
		soul = defaultSoul
	}

	serverName := ""
	servers := map[string]mcpServer{}
	if srvName, srv, ok := parseMCP(spec.MCP); ok {
		serverName = srvName
		servers[srvName] = srv
	}
	mcpBuf, err := json.MarshalIndent(mcpConfig{MCPServers: servers}, "", "  ")
	if err != nil {
		return Agent{}, fmt.Errorf("render mcp.json: %w", err)
	}
	settingsBuf, err := buildSettings(serverName)
	if err != nil {
		return Agent{}, fmt.Errorf("render settings.json: %w", err)
	}

	files := []struct {
		name string
		data []byte
	}{
		{soulFile, []byte(soul)},
		{mcpFile, mcpBuf},
		{settingsFile, settingsBuf},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(home, f.name), f.data, 0o644); err != nil {
			return Agent{}, fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	return Agent{Name: name, Home: home}, nil
}

// Get returns the agent named name, or false if no such home directory exists.
func (s *Store) Get(name string) (Agent, bool) {
	home := filepath.Join(s.root, name)
	info, err := os.Stat(home)
	if err != nil || !info.IsDir() {
		return Agent{}, false
	}
	return Agent{Name: name, Home: home}, true
}

// List returns every agent home under the store root, sorted by name. A missing
// root yields an empty list (no agents created yet).
func (s *Store) List() ([]Agent, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Agent
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, Agent{Name: e.Name(), Home: filepath.Join(s.root, e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/agent/...`
Expected: PASS (all 5 store tests).

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/internal/agent/
git add core/internal/agent/store.go core/internal/agent/agent.go core/internal/agent/store_test.go
git commit -m "feat(agent): durable agent store with seeded persona/mcp/settings"
```

---

## Task 2: Materialize the agent into a worktree

**Files:**
- Modify: `core/internal/agent/agent.go`
- Test: `core/internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing tests**

Create `core/internal/agent/agent_test.go`:

```go
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeWritesFilesWithSubstitution(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "roblox", Soul: "PERSONA", MCP: "neublox serve --project {{WORKTREE}}"})
	if err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	if err := a.Materialize(wt); err != nil {
		t.Fatal(err)
	}

	mcp, _ := os.ReadFile(filepath.Join(wt, ".mcp.json"))
	if strings.Contains(string(mcp), "{{WORKTREE}}") {
		t.Fatalf("token not substituted:\n%s", mcp)
	}
	if !strings.Contains(string(mcp), wt) {
		t.Fatalf("worktree path not injected:\n%s", mcp)
	}
	if !strings.Contains(string(mcp), `"neublox"`) {
		t.Fatalf("neublox server missing:\n%s", mcp)
	}

	settings, _ := os.ReadFile(filepath.Join(wt, ".claude", "settings.json"))
	if !strings.Contains(string(settings), "enableAllProjectMcpServers") {
		t.Fatalf("settings missing:\n%s", settings)
	}

	claude, _ := os.ReadFile(filepath.Join(wt, ".claude", "CLAUDE.md"))
	if string(claude) != "PERSONA" {
		t.Fatalf("CLAUDE.md = %q", claude)
	}
}

func TestMaterializeMissingHomeErrors(t *testing.T) {
	a := Agent{Name: "ghost", Home: filepath.Join(t.TempDir(), "ghost")}
	if err := a.Materialize(t.TempDir()); err == nil {
		t.Fatal("expected error materializing an agent with no home files")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/agent/... -run Materialize`
Expected: FAIL — `a.Materialize undefined`.

- [ ] **Step 3: Implement Materialize**

Append to `core/internal/agent/agent.go` (add the imports at the top of the file: `"fmt"`, `"os"`, `"path/filepath"`, `"strings"`):

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)
```

Then add the method:

```go
// Materialize provisions the agent into a session worktree by writing the three
// files Claude Code reads from its working directory:
//
//	<worktree>/.mcp.json             (from <home>/mcp.json)
//	<worktree>/.claude/settings.json (from <home>/settings.json)
//	<worktree>/.claude/CLAUDE.md     (from <home>/SOUL.md — the layered persona)
//
// Any worktreeToken in a source file is replaced with the worktree path.
func (a Agent) Materialize(worktree string) error {
	claudeDir := filepath.Join(worktree, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	copies := []struct{ src, dst string }{
		{filepath.Join(a.Home, mcpFile), filepath.Join(worktree, ".mcp.json")},
		{filepath.Join(a.Home, settingsFile), filepath.Join(claudeDir, "settings.json")},
		{filepath.Join(a.Home, soulFile), filepath.Join(claudeDir, "CLAUDE.md")},
	}
	for _, c := range copies {
		buf, err := os.ReadFile(c.src)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(c.src), err)
		}
		out := strings.ReplaceAll(string(buf), worktreeToken, worktree)
		if err := os.WriteFile(c.dst, []byte(out), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", c.dst, err)
		}
	}
	return nil
}
```

> Note: `agent.go` currently has no `import` block (Task 1 added only constants/types). Add the import block shown above directly under the `package agent` doc comment.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/agent/...`
Expected: PASS (all 7 agent tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/internal/agent/
git add core/internal/agent/agent.go core/internal/agent/agent_test.go
git commit -m "feat(agent): materialize agent home into a session worktree"
```

---

## Task 3: Add the `Agent` field to `state.Session`

**Files:**
- Modify: `core/internal/state/state.go:26`

- [ ] **Step 1: Add the field**

In `core/internal/state/state.go`, in `type Session struct`, add `Agent` immediately after the `Project` field (line 26):

Replace:

```go
	Worktree  string `json:"worktree,omitempty"` // abs path; empty for a shared session
	Project   string `json:"project,omitempty"`  // workspace sub-dir the session started from
```

with:

```go
	Worktree  string `json:"worktree,omitempty"` // abs path; empty for a shared session
	Project   string `json:"project,omitempty"`  // workspace sub-dir the session started from
	Agent     string `json:"agent,omitempty"`    // durable agent this session was provisioned from ("" = none)
```

- [ ] **Step 2: Verify the package still builds**

Run: `cd /home/shan/dev/herrscher && go build ./core/internal/state/... && go test ./core/internal/state/...`
Expected: PASS (no behavior change; field is additive and `omitempty`).

- [ ] **Step 3: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/internal/state/state.go
git commit -m "feat(state): record the agent a session was provisioned from"
```

---

## Task 4: Wire the agent store into the Handler

This task changes `NewHandler`'s signature, so it updates the call site (`cli.go`) and the test helper in the same commit to keep the build green. No new behavior yet.

**Files:**
- Modify: `core/internal/manager/handler.go`
- Modify: `core/host/cli.go:22-28`
- Modify: `core/internal/manager/handler_test.go:111-120`

- [ ] **Step 1: Update the Handler struct, constructor, and accessor**

Replace the entire body of `core/internal/manager/handler.go` with:

```go
package manager

import (
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// Handler holds the dependencies the session/service/agent commands act on.
// Commands (commands.go) turns its methods into declared contracts.Cmd values
// the CLI dispatches.
type Handler struct {
	d          discord
	sup        supervisor
	wt         worktrees
	fg         forges
	up         updater
	agents     *agent.Store
	st         *state.State
	defaultCmd string
	partDir    string // dir holding participants/<name>.log journals
}

// NewHandler builds a Handler. defaultCmd is the bridge command used when a
// session is created without an explicit cmd. partDir is the directory under
// which per-session participant journals live (participants/<name>.log). agents
// owns the durable agent homes used to provision sessions.
func NewHandler(d discord, sup supervisor, wt worktrees, fg forges, up updater, agents *agent.Store, st *state.State, defaultCmd, partDir string) *Handler {
	return &Handler{d: d, sup: sup, wt: wt, fg: fg, up: up, agents: agents, st: st, defaultCmd: defaultCmd, partDir: partDir}
}

// PartDir returns the participants journal directory (used by tests/wiring).
func (h *Handler) PartDir() string { return h.partDir }

// Agents returns the durable agent store (used by tests/wiring).
func (h *Handler) Agents() *agent.Store { return h.agents }
```

- [ ] **Step 2: Update the composition root**

In `core/host/cli.go`, add the import and construct the store. The import block at the top of the file must include:

```go
	"github.com/Herrscherd/herrscher/core/internal/agent"
```

Then replace lines 23–28 (inside `buildRegistry`):

```go
	partDir := filepath.Dir(o.StatePath)
	wt := worktree.NewWorktreer(ctx, instID)
	fg := forge.New()
	upCfg, _ := service.DefaultConfig()
	up := serviceUpdater{cfg: upCfg, st: st}
	hdl := manager.NewHandler(d.Admin, sup, wt, fg, up, st, o.DefaultCmd, partDir)
```

with:

```go
	partDir := filepath.Dir(o.StatePath)
	wt := worktree.NewWorktreer(ctx, instID)
	fg := forge.New()
	upCfg, _ := service.DefaultConfig()
	up := serviceUpdater{cfg: upCfg, st: st}
	agents := agent.NewStore(filepath.Join(partDir, "agents"))
	hdl := manager.NewHandler(d.Admin, sup, wt, fg, up, agents, st, o.DefaultCmd, partDir)
```

- [ ] **Step 3: Update the manager test helper**

In `core/internal/manager/handler_test.go`, add the import:

```go
	"github.com/Herrscherd/herrscher/core/internal/agent"
```

Then replace `newTestHandlerWithUpdater` (lines 111–120) with:

```go
func newTestHandlerWithUpdater(t *testing.T, homeType string) (*Handler, *fakeUpdater, *fakeDiscord, *fakeSup, *fakeWT, *fakeForge, *state.State) {
	t.Helper()
	d := &fakeDiscord{homeType: homeType}
	sup := &fakeSup{}
	wt := &fakeWT{path: "/wt/x"}
	fg := &fakeForge{}
	up := &fakeUpdater{version: "abc1234"}
	st := state.NewState(t.TempDir() + "/s.json")
	agents := agent.NewStore(t.TempDir())
	return NewHandler(d, sup, wt, fg, up, agents, st, "claude", t.TempDir()), up, d, sup, wt, fg, st
}
```

- [ ] **Step 4: Verify everything still builds and passes**

Run: `cd /home/shan/dev/herrscher && go build ./... && go test ./core/internal/manager/... ./core/host/...`
Expected: PASS — all existing manager/host tests green with the new wiring.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/internal/manager/handler.go core/internal/manager/handler_test.go core/host/cli.go
git add core/internal/manager/handler.go core/internal/manager/handler_test.go core/host/cli.go
git commit -m "feat(manager): inject the agent store into the command handler"
```

---

## Task 5: `agent create` / `agent list` commands

**Files:**
- Create: `core/internal/manager/agent.go`
- Modify: `core/internal/manager/commands.go`
- Test: `core/internal/manager/agent_test.go`

- [ ] **Step 1: Write the failing tests**

Create `core/internal/manager/agent_test.go`:

```go
package manager

import (
	"context"
	"strings"
	"testing"
)

func TestAgentCreateAndList(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")

	out, err := h.agentListRun(context.Background(), args())
	if err != nil || !strings.Contains(out, "No agents") {
		t.Fatalf("empty list: out=%q err=%v", out, err)
	}

	if _, err := h.agentCreateRun(context.Background(), args("name", "Roblox Dev", "soul", "PERSONA", "mcp", "neublox serve --project {{WORKTREE}}")); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.agents.Get("roblox-dev"); !ok {
		t.Fatalf("agent should exist under slug roblox-dev")
	}

	out, err = h.agentListRun(context.Background(), args())
	if err != nil || !strings.Contains(out, "roblox-dev") {
		t.Fatalf("list should mention agent: out=%q err=%v", out, err)
	}
}

func TestAgentCreateRejectsBadName(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	if _, err := h.agentCreateRun(context.Background(), args("name", "🙂")); err == nil {
		t.Fatal("expected rejection of unusable name")
	}
}

func TestAgentCreateMissingName(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	if _, err := h.agentCreateRun(context.Background(), args()); err == nil {
		t.Fatal("expected error when name missing")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/manager/... -run TestAgentCreate`
Expected: FAIL — `h.agentCreateRun undefined`, `h.agentListRun undefined`.

- [ ] **Step 3: Implement the handlers**

Create `core/internal/manager/agent.go`:

```go
package manager

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// agentCreateRun creates a durable companion agent: a home directory seeded with
// SOUL.md (persona), mcp.json (an optional stdio MCP server) and settings.json
// (zero-prompt). The agent is later materialized into a session worktree.
func (h *Handler) agentCreateRun(_ context.Context, in contracts.Input) (string, error) {
	raw, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid name %q — use letters, digits, - or _ (max 64)", raw)
	}
	soul, _ := in.Lookup("soul")
	mcp, _ := in.Lookup("mcp")
	a, err := h.agents.Create(agent.CreateSpec{Name: name, Soul: soul, MCP: mcp})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("🤖 Agent **%s** created at `%s`.", a.Name, a.Home), nil
}

// agentListRun lists the durable companion agents.
func (h *Handler) agentListRun(_ context.Context, _ contracts.Input) (string, error) {
	agents, err := h.agents.List()
	if err != nil {
		return "", err
	}
	if len(agents) == 0 {
		return "No agents. Create one with `agent create <name>`.", nil
	}
	out := "Agents:\n"
	for _, a := range agents {
		out += fmt.Sprintf("• **%s** (`%s`)\n", a.Name, a.Home)
	}
	return out, nil
}
```

- [ ] **Step 4: Declare the commands**

In `core/internal/manager/commands.go`, add the two `agent` commands to the returned slice — insert them immediately after the `session "who"` command block (after its `.Do(h.sessionWhoRun),` line, before `contracts.New("set", "home")`):

```go
		contracts.New("agent", "create").
			Help("create a durable companion agent (persona + MCP + zero-prompt settings)").
			Param("name", "agent name (slugified to a safe slug)", true).
			Param("soul", "persona text written to SOUL.md (layered as .claude/CLAUDE.md)", false).
			Param("mcp", "stdio MCP server command line, e.g. 'neublox serve --project {{WORKTREE}}'", false).
			Do(h.agentCreateRun),
		contracts.New("agent", "list").
			Help("list durable companion agents").
			Do(h.agentListRun),
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/manager/... -run TestAgent`
Expected: PASS (3 agent-handler tests).

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/internal/manager/agent.go core/internal/manager/commands.go core/internal/manager/agent_test.go
git add core/internal/manager/agent.go core/internal/manager/commands.go core/internal/manager/agent_test.go
git commit -m "feat(manager): add agent create/list commands"
```

---

## Task 6: `--agent` on `session create` + provisioning seam

**Files:**
- Modify: `core/internal/manager/commands.go`
- Modify: `core/internal/manager/session.go`
- Test: `core/internal/manager/agent_test.go` (append)

- [ ] **Step 1: Write the failing tests**

Append to `core/internal/manager/agent_test.go`. Add `os`, `path/filepath`, and the `agent`/`state` packages to its imports so the file's import block becomes:

```go
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)
```

Then append these tests:

```go
func TestSessionCreateWithAgentMaterializes(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	worktreeDir := t.TempDir()
	wt.path = worktreeDir
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if _, err := h.agents.Create(agent.CreateSpec{Name: "roblox", Soul: "PERSONA", MCP: "neublox serve --project {{WORKTREE}}"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "roblox")); err != nil {
		t.Fatal(err)
	}

	mcp, err := os.ReadFile(filepath.Join(worktreeDir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	if !strings.Contains(string(mcp), worktreeDir) || strings.Contains(string(mcp), "{{WORKTREE}}") {
		t.Fatalf(".mcp.json not substituted:\n%s", mcp)
	}
	if !strings.Contains(string(mcp), `"neublox"`) {
		t.Fatalf(".mcp.json missing neublox:\n%s", mcp)
	}

	settings, err := os.ReadFile(filepath.Join(worktreeDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	if !strings.Contains(string(settings), "enableAllProjectMcpServers") || !strings.Contains(string(settings), "mcp__neublox__*") {
		t.Fatalf("settings.json wrong:\n%s", settings)
	}

	claude, err := os.ReadFile(filepath.Join(worktreeDir, ".claude", "CLAUDE.md"))
	if err != nil || string(claude) != "PERSONA" {
		t.Fatalf("CLAUDE.md = %q err=%v", claude, err)
	}

	sess, _ := st.FindSession("demo")
	if sess.Agent != "roblox" {
		t.Fatalf("session.Agent = %q, want roblox", sess.Agent)
	}
}

func TestSessionCreateUnknownAgentRollsBack(t *testing.T) {
	h, d, _, wt, _, st := newTestHandler(t, "")
	worktreeDir := t.TempDir()
	wt.path = worktreeDir
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "ghost")); err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if len(wt.removed) != 1 {
		t.Fatalf("worktree should be rolled back: %+v", wt.removed)
	}
	if len(d.created) != 0 {
		t.Fatalf("no channel should be created: %+v", d.created)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("session must not persist")
	}
}

func TestSessionCreateAgentRequiresWorktree(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if _, err := h.agents.Create(agent.CreateSpec{Name: "roblox"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "roblox", "shared", "true")); err == nil {
		t.Fatal("expected error: agent session needs an isolated worktree")
	}
}

func TestSessionCreateNoAgentUnchanged(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	worktreeDir := t.TempDir()
	wt.path = worktreeDir
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(worktreeDir, ".mcp.json")); err == nil {
		t.Fatal("no agent → no provisioning files should be written")
	}
	sess, _ := st.FindSession("demo")
	if sess.Agent != "" {
		t.Fatalf("session.Agent should be empty, got %q", sess.Agent)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/manager/... -run TestSessionCreate`
Expected: FAIL — provisioning files absent / `session.Agent` empty / unknown-agent does not error (the `--agent` param is silently ignored today).

- [ ] **Step 3: Read the agent param in `sessionCreateRun`**

In `core/internal/manager/session.go`, add the `agent` lookup next to the other param reads. After the `backend` block (lines 65–68), insert:

```go
	agentName, _ := in.Lookup("agent")
```

So the region reads:

```go
	backend, _ := in.Lookup("backend")
	if backend == "" {
		backend = "stream" // default backend: persistent claude stream-json
	}
	agentName, _ := in.Lookup("agent")
```

- [ ] **Step 4: Add the provisioning seam after worktree creation**

Still in `core/internal/manager/session.go`, replace the worktree-creation block (lines 93–102):

```go
	repo := repoFor(ws, project)
	// Worktree isolation by default; shared:true runs in the main checkout.
	shared := in.Bool("shared")
	var worktree string
	if !shared {
		path, err := h.wt.Create(repo, name)
		if err != nil {
			return "", fmt.Errorf("worktree: %v", err)
		}
		worktree = path // "" means non-git fallback
	}
```

with:

```go
	repo := repoFor(ws, project)
	// Worktree isolation by default; shared:true runs in the main checkout.
	shared := in.Bool("shared")
	var worktree string
	if !shared {
		path, err := h.wt.Create(repo, name)
		if err != nil {
			return "", fmt.Errorf("worktree: %v", err)
		}
		worktree = path // "" means non-git fallback
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
			if rmErr := h.wt.Remove(repo, name, true); rmErr != nil {
				fmt.Fprintf(os.Stderr, "herrscher: worktree rollback for %q failed: %v\n", name, rmErr)
			}
			return "", fmt.Errorf("unknown agent %q — create it with `agent create %s`", agentName, agentName)
		}
		if err := a.Materialize(worktree); err != nil {
			if rmErr := h.wt.Remove(repo, name, true); rmErr != nil {
				fmt.Fprintf(os.Stderr, "herrscher: worktree rollback for %q failed: %v\n", name, rmErr)
			}
			return "", fmt.Errorf("provision agent %q: %v", agentName, err)
		}
	}
```

- [ ] **Step 5: Persist the agent on the session**

Still in `core/internal/manager/session.go`, set `Agent: agentName` in both `state.Session{…}` literals.

Replace the category literal (line 116):

```go
			sess = state.Session{Name: name, ChannelID: chID, Type: "text", Cmd: cmd, Backend: backend, Worktree: worktree, Project: project, Gateways: gateways}
```

with:

```go
			sess = state.Session{Name: name, ChannelID: chID, Type: "text", Cmd: cmd, Backend: backend, Worktree: worktree, Project: project, Agent: agentName, Gateways: gateways}
```

Replace the forum literal (line 125):

```go
			sess = state.Session{Name: name, ChannelID: chID, Type: "forum", Cmd: cmd, Backend: backend, Worktree: worktree, Project: project, Gateways: gateways}
```

with:

```go
			sess = state.Session{Name: name, ChannelID: chID, Type: "forum", Cmd: cmd, Backend: backend, Worktree: worktree, Project: project, Agent: agentName, Gateways: gateways}
```

> `os` and `fmt` are already imported in `session.go` — no import changes needed.

- [ ] **Step 6: Declare the `--agent` param**

In `core/internal/manager/commands.go`, add an `agent` param to the `session create` builder. Insert it after the `shared` param line (`.Param("shared", …, false).`), before `.Do(h.sessionCreateRun),`:

```go
				Param("agent", "provision the session from a durable agent (its persona + MCP + zero-prompt settings)", false).
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/manager/...`
Expected: PASS — the four new `TestSessionCreate*` agent tests plus all pre-existing manager tests.

- [ ] **Step 8: Commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/internal/manager/session.go core/internal/manager/commands.go core/internal/manager/agent_test.go
git add core/internal/manager/session.go core/internal/manager/commands.go core/internal/manager/agent_test.go
git commit -m "feat(session): provision a session from a durable agent via --agent"
```

---

## Task 7: Full build + test sweep

**Files:** none (verification only)

- [ ] **Step 1: Build everything**

Run: `cd /home/shan/dev/herrscher && go build ./...`
Expected: clean build, no output.

- [ ] **Step 2: Vet**

Run: `cd /home/shan/dev/herrscher && go vet ./core/...`
Expected: no findings.

- [ ] **Step 3: Run the full test suite**

Run: `cd /home/shan/dev/herrscher && go test ./core/...`
Expected: all packages PASS, including `core/internal/agent`, `core/internal/manager`, `core/internal/state`, `core/host`.

- [ ] **Step 4: Confirm formatting is clean**

Run: `cd /home/shan/dev/herrscher && gofmt -l core/`
Expected: no output (all files formatted).

- [ ] **Step 5: Manual smoke (optional, no Studio needed)**

```bash
cd /home/shan/dev/herrscher
export DCTL_STATE_DIR=$(mktemp -d)
# build the operator CLI if there is a cmd target, then:
#   <cli> agent create roblox --soul "You are a Roblox dev companion." --mcp "neublox serve --project {{WORKTREE}}"
#   <cli> agent list
# verify the agent home was created under $DCTL_STATE_DIR/agents/roblox/ with SOUL.md, mcp.json, settings.json
ls -R "$DCTL_STATE_DIR/agents" 2>/dev/null || echo "create an agent first"
```
Expected: `agents/roblox/{SOUL.md,mcp.json,settings.json}` present. (Provisioning into a worktree is exercised by the automated `TestSessionCreateWithAgentMaterializes`; the end-to-end-with-real-Studio path is P0 §5.4 / §6 manual checklist and is out of scope for this herrscher-side plan.)

---

## Spec back-port note (do after the plan executes)

The P0 spec (`/home/shan/dev/Neublox/docs/specs/P0-substrate-neublox-on-herrscher.md`) still writes the agent home as `~/.herrscher/agents/<name>/` (§3 diagram, §5.1). This plan uses the real herrscher convention: **`<stateDir>/agents/<name>/`** where `stateDir = filepath.Dir(StatePath)` = `~/.config/dctl/` by default, `$DCTL_STATE_DIR` override. Update P0 §3 and §5.1 to match. (Tracked in memory: `neublox-design-docs`.)

## Self-review notes

- **Spec coverage (P0 §5.1, §5.2, §6):** Agent entity + `agent create`/`agent list` → Tasks 1, 2, 5. `--agent` param threaded to `state.Session` → Tasks 3, 6. Provisioning injection right after worktree creation → Task 6 Step 4. The automated "materializes `.mcp.json` + `.claude/settings.json` + `.claude/CLAUDE.md` with expected content" test (§6) → Task 6 `TestSessionCreateWithAgentMaterializes`. Cleanup is unchanged (`sessionCloseRun` already removes the worktree; the durable store lives in the agent home outside it).
- **Decisions honored:** A (pure files, no backend argv change) → Materialize writes only worktree files. B (SOUL layered as `.claude/CLAUDE.md`) → Materialize maps `SOUL.md`→`.claude/CLAUDE.md`. C (zero-prompt: `enableAllProjectMcpServers` + `permissions.allow` + permissive `defaultMode`) → `buildSettings`. The exact headless permission mode (`acceptEdits` + allow-listed `Bash`) remains the P0 §8 open risk — tune against a real Studio.
- **Out of scope (correctly deferred):** memory scoping / `KindAgent` → P1; `neublox serve` (Rust) → separate Neublox plan; Studio dock → P2; orchestration → P3.
- **Type consistency:** `agent.Store` methods (`NewStore/Create/Get/List/Root`), `agent.CreateSpec{Name,Soul,MCP}`, `agent.Agent{Name,Home}` + `Materialize(worktree)`, and `state.Session.Agent` are used identically across Tasks 1–6. `NewHandler`'s new `agents` param is added in Task 4 before any task depends on it at runtime (Task 6).
