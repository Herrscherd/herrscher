# A TUI session that learns — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** make the session a plain `herrscher` launch opens file what it learns under a project and an agent root, instead of learning nothing at all.

**Architecture:** the terminal gateway resolves a memory project from the directory it launched in and sends it, along with an extractor and a cadence, on the `CreateSession` it already makes. Those travel as new memory-only fields — `MemoryProject`, `MemoryAgent` — which never touch where the session lives. A session whose project was only guessed is left unpinned, and the bridge settles it once against the projects the vault already knows, on the first prompt, then rides the existing `reply{done}` piggyback home so the daemon can write it down.

**Tech Stack:** Go 1.25, four modules (`herrscher-contracts`, `herrscher-orchestrator`, `herrscher`, and the vault plugin `herrscher-obsidian-memory`, which is deliberately untouched). Table-driven `testing`, no test framework.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-15-tui-session-learning-design.md`. Read it before Task 1.
- **Learning never breaks a turn.** Every new failure mode degrades to today's behaviour and is logged at warn, never fatal.
- **A memory root is not a location.** No new field may reach `repoFor`, the worktree decision, or agent provisioning.
- The isolated-worktree rule for `Agent` (`core/internal/manager/session.go:551`) is not relaxed.
- `contracts.Orchestrator` gains no method. `SetScope` is discovered by type assertion, like `Start(context.Context)` in `core/bridge/bridge.go:52`.
- Version floors: `herrscher-contracts` v0.4.0, `herrscher-orchestrator` v0.2.0. `herrscher` bumps both in Task 10.
- Every new terminal setting is `Required: false` and carries a `Default`, so a build with no environment behaves as the table in the spec describes.
- `learn=false` restores today's behaviour exactly: the three fields `openDefaultSession` sends today, and nothing more.

## File Structure

**herrscher-contracts** (`/home/shan/dev/herrscher-contracts`)
- `session_control.go` — `CreateSession` gains `MemoryProject`, `MemoryAgent`, `ProjectPinned`.
- `event.go` — `Event` gains `Project`.
- `memory_scope.go` — `NormalizeScope`, the exported folding `ProjectKey` already applies.
- `event_project_test.go`, `memory_scope_export_test.go` — new.

**herrscher-orchestrator** (`/home/shan/dev/herrscher-orchestrator`)
- `orchestrator.go` — `Curator.scopeMu`, `scopeOf()`, `SetScope`.
- `conscious.go`, `promote.go`, `learner.go` — every `scope` read goes through `scopeOf()`.
- `setscope_test.go` — new.

**herrscher** (this worktree)
- `core/scope/scope.go` — new leaf package: `ProjectFromDir`, `MatchProject`. Depends on stdlib + contracts only, because both the terminal plugin and the bridge binary need it and neither may import the other.
- `core/internal/state/state.go` — three session fields, `SetProjectPinned`.
- `core/internal/manager/commands.go` + `session.go` — three create params.
- `core/internal/supervisor/supervisor.go` — argv prefers the memory roots; adds `--project-pinned`.
- `core/host/hub.go` — `hub.create` maps the three fields; the `runSessionIdentified` call site moves to `sessionSink`.
- `core/host/turnloop.go` — `sessionSink` replaces two positional callbacks; `e.Project` is persisted beside `e.Resume`.
- `core/bridge/bridge.go` — `Options` gains `Scope`, `LaunchProject`, `ProjectPinned`, `MemoryAgent`.
- `core/bridge/hub.go` — `scopePin`, threaded into `runOneTurn`.
- `bridge.go` (root) — `--project-pinned`, and the concrete vault-backed resolver.
- `plugins/terminal/terminal.go` — the settings bag, and what `openDefaultSession` sends.

## Order of work

Tasks 1, 2 and 3 are independent and may run at the same time. Task 4 is independent too. Everything after depends on those. Task 10 closes the loop.

```
1 contracts ──┬── 6 hub.create ──┐
2 orchestrator┤                  ├── 9 terminal ── 10 bump + docs
3 core/scope ─┤   7 turnloop ────┤
4 state ── 5 manager+supervisor ─┘
              └── 8 bridge pin ──┘
```

---

### Task 0: A go.work so the four modules see each other

**Files:**
- Create: `go.work` at this worktree's root (never committed)
- Modify: the worktree's git exclude file

This worktree lives outside `/home/shan/dev`, so `/home/shan/dev/go.work` does not apply to it and `go build` resolves `herrscher-contracts` from the module cache at v0.3.0. Every later task edits contracts and expects `herrscher` to see it.

- [ ] **Step 1: Write the workspace file**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
cat > go.work <<'EOF'
go 1.25

use (
	.
	/home/shan/dev/herrscher-contracts
	/home/shan/dev/herrscher-orchestrator
)
EOF
```

- [ ] **Step 2: Keep it out of the repository**

It is a machine-local build convenience, not a fact about the project, so it is excluded rather than gitignored — `.gitignore` is shared, `info/exclude` is not.

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
printf 'go.work\ngo.work.sum\n' >> "$(git rev-parse --git-path info/exclude)"
git status --short
```

Expected: empty output. If `go.work` shows as untracked, the exclude did not take.

- [ ] **Step 3: Prove the wiring**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go build ./... && echo BUILD-OK
```

Expected: `BUILD-OK`.

Nothing to commit in this task.

---

### Task 1: contracts — the fields a learning session travels on

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/session_control.go` (the `CreateSession` struct, around line 82)
- Modify: `/home/shan/dev/herrscher-contracts/event.go` (the `Event` struct, around line 26)
- Modify: `/home/shan/dev/herrscher-contracts/memory_scope.go` (after `AgentKey`, line 32)
- Test: `/home/shan/dev/herrscher-contracts/event_project_test.go` (create)
- Test: `/home/shan/dev/herrscher-contracts/memory_scope_export_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `CreateSession.MemoryProject string`, `CreateSession.MemoryAgent string`, `CreateSession.ProjectPinned bool`, `Event.Project string` (json `project,omitempty`), `func NormalizeScope(name string) string`.

- [ ] **Step 1: Write the failing tests**

`/home/shan/dev/herrscher-contracts/event_project_test.go`:

```go
package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

// A settled project rides home on the terminal reply, the same way Resume does.
func TestEventCarriesTheSettledProject(t *testing.T) {
	b, err := json.Marshal(Event{T: "reply", Done: true, Project: "neublox"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"project":"neublox"`) {
		t.Fatalf("reply did not carry the project: %s", b)
	}
	var back Event
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Project != "neublox" {
		t.Fatalf("Project = %q, want %q", back.Project, "neublox")
	}
}

// Every event that settles nothing must stay byte-identical to what it is today,
// because every gateway and every recorded transcript reads this wire.
func TestEventWithoutAProjectSaysNothing(t *testing.T) {
	b, err := json.Marshal(Event{T: "reply", Done: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "project") {
		t.Fatalf("an unsettled reply mentioned a project: %s", b)
	}
}
```

`/home/shan/dev/herrscher-contracts/memory_scope_export_test.go`:

```go
package contracts

import "testing"

// NormalizeScope exists so a host deriving a scope name from a directory or a
// prompt produces exactly the segment ProjectKey would, and never splits one
// scope into two vault files.
func TestNormalizeScopeAgreesWithProjectKey(t *testing.T) {
	for _, name := range []string{"Neublox", "herrscher docs", "  A/B  ", "é"} {
		if got, want := "projects/"+NormalizeScope(name), ProjectKey(name); got != want {
			t.Fatalf("NormalizeScope(%q) → %q, ProjectKey → %q", name, got, want)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

```bash
cd /home/shan/dev/herrscher-contracts && go test ./... -run 'TestEvent(Carries|Without)|TestNormalizeScope'
```

Expected: FAIL — `unknown field Project in struct literal` and `undefined: NormalizeScope`.

- [ ] **Step 3: Add the four fields**

In `session_control.go`, inside `CreateSession`, immediately after the `Agent string` line:

```go
	// MemoryProject and MemoryAgent name the shared and private memory roots this
	// session files what it learns under, and nothing else. They are deliberately
	// separate from Project and Agent, which place the session: Project steers the
	// workspace sub-directory the bridge runs in, and Agent demands an isolated
	// worktree be provisioned into. A session that only wants somewhere to put what
	// it learned should not have to move house to get it.
	MemoryProject string
	MemoryAgent   string
	// ProjectPinned marks MemoryProject as a human's choice rather than the host's
	// guess. Only a guess may be revised by the session's first prompt.
	ProjectPinned bool
```

In `event.go`, inside `Event`, immediately after the `Resume string` field:

```go
	// Project carries the memory project this session settled on, piggybacked on
	// the terminal reply{done} so the daemon can persist it — the same path Resume
	// takes. Empty when the turn settled nothing, which is every turn of every
	// session whose project a human already chose.
	Project string `json:"project,omitempty"`
```

In `memory_scope.go`, immediately after `AgentKey`:

```go
// NormalizeScope exposes the folding ProjectKey and AgentKey apply. A host that
// derives a scope name from somewhere else — a directory, a prompt — uses it to
// produce exactly the segment the key would, so the two can never disagree about
// whether Neublox and neublox are one project or two.
func NormalizeScope(name string) string { return normalizeScopeName(name) }
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /home/shan/dev/herrscher-contracts && go test ./... && gofmt -l .
```

Expected: `ok  github.com/Herrscherd/herrscher-contracts`, and `gofmt -l` prints nothing.

- [ ] **Step 5: Commit and tag**

```bash
cd /home/shan/dev/herrscher-contracts
git checkout -b feat/memory-roots
git add -A
git commit -m "feat(contracts): a memory root is not a location

CreateSession.Project already means a workspace sub-directory and Agent
already means a provisioned worktree. Learning needs neither, so it gets
MemoryProject and MemoryAgent, which say where knowledge is filed and
nothing else, plus the pin that says whether a human chose the project.
Event.Project brings the answer home on the reply Resume already rides."
git push -u origin feat/memory-roots
```

Tagging waits for the branch to land on `master`; Task 10 does it. Until then every consumer builds through the `go.work` from Task 0.

---

### Task 2: orchestrator — re-rooting memory mid-session

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/orchestrator.go` (the `Curator` struct at line 39; add after `SetStaleness`, line 81)
- Modify: `/home/shan/dev/herrscher-orchestrator/conscious.go:64,65,85,86,87`
- Modify: `/home/shan/dev/herrscher-orchestrator/orchestrator.go:95,96`
- Modify: `/home/shan/dev/herrscher-orchestrator/promote.go:95,99,122,129`
- Modify: `/home/shan/dev/herrscher-orchestrator/learner.go:402,404` (and add `SetScope` near `Consolidate`, line 314)
- Test: `/home/shan/dev/herrscher-orchestrator/setscope_test.go` (create)

**Interfaces:**
- Consumes: `contracts.MemoryScope` (unchanged, already in v0.3.0). This task does **not** depend on Task 1.
- Produces: `func (c *Curator) SetScope(s contracts.MemoryScope)` and `func (l *Learner) SetScope(s contracts.MemoryScope)`. Task 8 discovers them as `interface{ SetScope(contracts.MemoryScope) }`.

- [ ] **Step 1: Write the failing test**

`/home/shan/dev/herrscher-orchestrator/setscope_test.go`:

```go
package orchestrator

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

// A session that launched under a guessed project must be able to file the rest
// of what it learns somewhere else, once the conversation says where.
func TestSetScopeRedirectsWhatIsRemembered(t *testing.T) {
	mem := newFake()
	c := NewScoped(mem, "s", contracts.MemoryScope{Project: contracts.ProjectKey("herrscher")})
	c.React(context.Background(), "<remember>the launch guess</remember>")
	c.SetScope(contracts.MemoryScope{Project: contracts.ProjectKey("neublox")})
	c.React(context.Background(), "<remember>what it really was</remember>")

	var before, after bool
	for key := range mem.nodes {
		before = before || strings.HasPrefix(key, "projects/herrscher/")
		after = after || strings.HasPrefix(key, "projects/neublox/")
	}
	if !before {
		t.Fatal("the fact remembered before the re-scope should stay where it was filed")
	}
	if !after {
		t.Fatalf("nothing was filed under the new project: %v", mem.nodes)
	}
}

// SetScope arrives from the turn goroutine while the idle loop may be walking
// the old roots. Run under -race: the point of the test is that it is quiet.
func TestSetScopeIsSafeAlongsideAConsolidate(t *testing.T) {
	l := NewLearner(newFake(), "s", contracts.MemoryScope{Project: contracts.ProjectKey("a")}, nil, "", 0) // ex=nil, journal="", every=0
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = l.Consolidate(context.Background())
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			l.SetScope(contracts.MemoryScope{Project: contracts.ProjectKey("b")})
		}
	}()
	wg.Wait()
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd /home/shan/dev/herrscher-orchestrator && go test ./... -race -run TestSetScope
```

Expected: FAIL — `c.SetScope undefined (type *Curator has no field or method SetScope)`.

- [ ] **Step 3: Add the mutex, the accessor and the two setters**

In `orchestrator.go`, add `"sync"` to the imports, and inside the `Curator` struct replace the `scope` line with:

```go
	// scope is guarded by scopeMu because SetScope can re-root a live session
	// from the turn goroutine while a background idle Consolidate is walking the
	// roots it is about to replace. Every read goes through scopeOf.
	scopeMu sync.RWMutex
	scope   contracts.MemoryScope // P1: shared project + private agent roots ({} = none)
```

After `SetStaleness` (line 81):

```go
// scopeOf reads the memory scope under scopeMu.
func (c *Curator) scopeOf() contracts.MemoryScope {
	c.scopeMu.RLock()
	defer c.scopeMu.RUnlock()
	return c.scope
}

// SetScope re-roots memory mid-session. The host calls it once, when a session's
// first prompt settles a project its launch could only guess at; what was already
// filed stays filed, because a fact recorded under the guess is still true.
func (c *Curator) SetScope(s contracts.MemoryScope) {
	c.scopeMu.Lock()
	c.scope = s
	c.scopeMu.Unlock()
}
```

In `learner.go`, next to `Consolidate` (line 314):

```go
// SetScope re-roots memory, waiting for any consolidation in flight to finish
// first, so one pass never files half its findings under the old project and the
// other half under the new one. Lock order is mu → scopeMu, the same direction as
// mu → stampMu, so the three can never deadlock.
func (l *Learner) SetScope(s contracts.MemoryScope) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Curator.SetScope(s)
}
```

- [ ] **Step 4: Route every read through the accessor**

There are exactly 13 reads. Take them one file at a time; each is a mechanical `X.scope` → `X.scopeOf()`, except where the same function reads it twice, which should bind it once to a local.

`conscious.go`, in `recall` (line 63-66):

```go
	var hits []contracts.Node
	if scope := c.scopeOf(); scope.Project != "" {
		hits, _ = contracts.RecallRelevant(ctx, c.mem, scope, query, recallK)
	} else {
		hits, _ = c.mem.Search(ctx, contracts.Query{Text: query, Ranked: true, Limit: recallK})
	}
```

`conscious.go`, in `remember` (line 85-88):

```go
	if scope := c.scopeOf(); scope.Project != "" {
		node.Key = scope.Project + "/notes/" + slug(title)
		_ = contracts.RecordShared(ctx, c.mem, scope, node)
		return
	}
```

`orchestrator.go`, in `Context` (line 95-96):

```go
	if scope := c.scopeOf(); scope.Project != "" {
		if sg, err := contracts.RecallScoped(ctx, c.mem, scope, 1); err == nil {
```

`promote.go` (lines 95, 99): bind `scope := l.scopeOf()` at the top of that function and use `scope.Project` / `scope` in place of `l.scope.Project` / `l.scope`.

`promote.go` (lines 122, 129): bind `scope := l.scopeOf()` at the top of `Promote`, then `if l.promoteMinAge <= 0 || l.mem == nil || scope.Project == "" || scope.Agent == ""` and `prefix := scope.Agent + "/"`.

`learner.go` (lines 402, 404): bind `scope := l.scopeOf()` above the branch, then `contracts.RecordPrivate(ctx, l.mem, scope, c.Node)` and `contracts.RecordShared(ctx, l.mem, scope, c.Node)`.

Then confirm none are left:

```bash
cd /home/shan/dev/herrscher-orchestrator && grep -rn '\.scope\b' --include='*.go' . | grep -v _test.go | grep -v scopeMu | grep -v 'c.scope =' | grep -v 'scope:'
```

Expected: no output.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd /home/shan/dev/herrscher-orchestrator && go test ./... -race && gofmt -l .
```

Expected: `ok  github.com/Herrscherd/herrscher-orchestrator`, `gofmt -l` silent.

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
git checkout -b feat/set-scope
git add -A
git commit -m "feat: let a session be re-rooted once it knows what it is about

A session that launched on a guess has to be able to file the rest of what
it learns where the conversation says, not where the directory did. SetScope
waits for a consolidation in flight, so no pass is split across two projects."
git push -u origin feat/set-scope
```

---

### Task 3: core/scope — where a session's work belongs

**Files:**
- Create: `core/scope/scope.go`
- Test: `core/scope/scope_test.go`

**Interfaces:**
- Consumes: `contracts.NormalizeScope` from Task 1.
- Produces: `func ProjectFromDir(dir string) string` and `func MatchProject(prompt string, known []string) string`, both in package `scope`, imported as `github.com/Herrscherd/herrscher/core/scope`.

- [ ] **Step 1: Write the failing test**

`core/scope/scope_test.go`:

```go
package scope

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepo makes a real repository named name under t.TempDir and returns its
// path. Real git, because the whole point of ProjectFromDir is what git answers.
func gitRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("commit", "-q", "--allow-empty", "-m", "root")
	return dir
}

func TestProjectFromDirNamesTheRepository(t *testing.T) {
	repo := gitRepo(t, "Herrscher")
	if got := ProjectFromDir(repo); got != "herrscher" {
		t.Fatalf("ProjectFromDir(repo) = %q, want %q", got, "herrscher")
	}
}

func TestProjectFromDirAnswersTheSameFromASubdirectory(t *testing.T) {
	repo := gitRepo(t, "herrscher")
	sub := filepath.Join(repo, "core", "scope")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ProjectFromDir(sub); got != "herrscher" {
		t.Fatalf("ProjectFromDir(sub) = %q, want %q", got, "herrscher")
	}
}

// Three worktrees of one repository are three conversations about one thing.
// Splitting their memory three ways would defeat the point of having any.
func TestProjectFromDirFoldsWorktreesIntoTheirRepository(t *testing.T) {
	repo := gitRepo(t, "herrscher")
	wt := filepath.Join(filepath.Dir(repo), "detached-elsewhere")
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "side", wt)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	if got := ProjectFromDir(wt); got != "herrscher" {
		t.Fatalf("ProjectFromDir(worktree) = %q, want %q", got, "herrscher")
	}
}

func TestProjectFromDirIsSilentOutsideARepository(t *testing.T) {
	if got := ProjectFromDir(t.TempDir()); got != "" {
		t.Fatalf("ProjectFromDir(non-repo) = %q, want empty", got)
	}
	if got := ProjectFromDir(""); got != "" {
		t.Fatalf("ProjectFromDir(\"\") = %q, want empty", got)
	}
}

// A directory whose name carries nothing nameable must not become the project
// "scope" — the normaliser's own fallback. It must become no project at all.
func TestProjectFromDirRefusesAnUnnameableDirectory(t *testing.T) {
	if got := ProjectFromDir(gitRepo(t, "...")); got != "" {
		t.Fatalf("ProjectFromDir(unnameable) = %q, want empty", got)
	}
}

func TestMatchProject(t *testing.T) {
	known := []string{"herrscher", "neublox", "herrscher-docs"}
	for _, tc := range []struct {
		name, prompt, want string
	}{
		{"names one", "je bosse sur neublox aujourd'hui", "neublox"},
		{"names none", "on continue là où on en était", ""},
		{"case folds", "NEUBLOX est cassé", "neublox"},
		{"earliest named wins", "neublox puis herrscher", "neublox"},
		{"whole words only", "neubloxide est un autre projet", ""},
		{"longest at the same place wins", "herrscher-docs a besoin d'une page", "herrscher-docs"},
		{"nothing known, nothing matched", "neublox", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := known
			if tc.name == "nothing known, nothing matched" {
				k = nil
			}
			if got := MatchProject(tc.prompt, k); got != tc.want {
				t.Fatalf("MatchProject(%q) = %q, want %q", tc.prompt, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/scope/
```

Expected: FAIL — `no required module provides package .../core/scope` or `undefined: ProjectFromDir`.

- [ ] **Step 3: Write the implementation**

`core/scope/scope.go`:

```go
// Package scope answers which memory project a piece of work belongs to. The
// terminal gateway asks at launch, from the directory the operator started in;
// the bridge asks again on a session's first prompt, against the projects the
// vault already knows. Neither of those two may import the other, so the rule
// lives here, in a leaf that depends on nothing but the standard library and the
// contracts that define what a scope name looks like.
package scope

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// gitTimeout bounds the single git call ProjectFromDir makes. A launch must not
// hang because a repository's objects live on a stalled network mount.
const gitTimeout = 2 * time.Second

// maxScopeLen mirrors the length projectRe allows in core/internal/manager, so a
// name this package hands out can always be persisted as a project.
const maxScopeLen = 128

// ProjectFromDir names the memory project work done in dir belongs to: the git
// repository dir is in, folded to a single stable scope segment. It answers ""
// when dir is in no repository, or when the repository's name carries nothing
// nameable — an unscoped session, which is what every session gets today, so ""
// is never a failure, only a silence.
//
// It resolves the repository's *common* git directory rather than the worktree,
// so every worktree of one repository answers with one project.
func ProjectFromDir(dir string) string {
	if dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return ""
	}
	// git answers relative to dir when it feels like it (".git" from a repo root).
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	common = filepath.Clean(common)
	// <root>/.git → <root>. A bare repository is named by its own directory,
	// conventionally <root>.git, so drop the suffix rather than the segment.
	root := common
	if filepath.Base(common) == ".git" {
		root = filepath.Dir(common)
	} else {
		root = strings.TrimSuffix(common, ".git")
	}
	return nameOf(filepath.Base(filepath.Clean(root)))
}

// MatchProject picks, among the projects the vault already knows, the one a
// prompt is about. It answers "" when the prompt names none of them, which means
// "keep whatever the session launched with".
//
// The rule is deliberately the dullest one that can work: a known project named
// as a whole word wins, the earliest mention breaks a tie, and the longest name
// breaks a tie at the same place, so "herrscher-docs" is not read as
// "herrscher". This is the piece of the design most likely to be wrong in the
// field. It takes its whole world as arguments precisely so the vault-side
// registry that replaces it changes nothing above this line.
func MatchProject(prompt string, known []string) string {
	if prompt == "" || len(known) == 0 {
		return ""
	}
	lower := strings.ToLower(prompt)
	best, at := "", -1
	for _, k := range known {
		n := nameOf(k)
		if n == "" {
			continue
		}
		i := indexWord(lower, n)
		if i < 0 {
			continue
		}
		if at < 0 || i < at || (i == at && len(n) > len(best)) {
			best, at = n, i
		}
	}
	return best
}

// nameOf folds raw into the scope segment contracts would use, or "" when raw
// holds nothing nameable. The empty answer matters: the normaliser's own
// fallback is the literal "scope", and a session filed under a project called
// "scope" is worse than one filed under no project at all.
func nameOf(raw string) string {
	nameable := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			nameable = true
			break
		}
	}
	if !nameable {
		return ""
	}
	n := contracts.NormalizeScope(raw)
	if len(n) > maxScopeLen {
		return ""
	}
	return n
}

// indexWord finds needle in hay on word boundaries, so "neublox" is found in
// "je bosse sur neublox" but not inside "neubloxide". It returns -1 for no match.
func indexWord(hay, needle string) int {
	for i := 0; i <= len(hay)-len(needle); {
		j := strings.Index(hay[i:], needle)
		if j < 0 {
			return -1
		}
		at := i + j
		if boundary(hay, at-1) && boundary(hay, at+len(needle)) {
			return at
		}
		i = at + 1
	}
	return -1
}

// boundary reports whether index i in s is off the end or holds something that
// is not part of a word. Off the end counts: the start and end of the prompt are
// boundaries.
func boundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	r := rune(s[i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/scope/ -v 2>&1 | tail -30
```

Expected: every test `--- PASS`, then `ok  github.com/Herrscherd/herrscher/core/scope`.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
git add core/scope
git commit -m "feat(scope): name the project a piece of work belongs to

Where you are proposes it and what you say may revise it, so the two rules
are two functions: one knows only the filesystem, the other only what the
vault already holds. Both are pure enough to replace from underneath."
```

---

### Task 4: state — the three fields, and the pin

**Files:**
- Modify: `core/internal/state/state.go` (the `Session` struct, after the `Agent` field around line 39; and after `SetResumeToken`, line 299)
- Test: `core/internal/state/project_pin_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `state.Session.MemoryProject string`, `state.Session.MemoryAgent string`, `state.Session.ProjectPinned bool`, `func (s *State) SetProjectPinned(name, project string) error`.

- [ ] **Step 1: Write the failing test**

`core/internal/state/project_pin_test.go` — `state_resume_test.go` is the exact
template, since this setter is the same shape:

```go
package state

import (
	"path/filepath"
	"testing"
)

func TestSetProjectPinnedPersistsAndPins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	if err := s.AddSession(Session{Name: "main", MemoryProject: "herrscher"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectPinned("main", "neublox"); err != nil {
		t.Fatal(err)
	}
	// Re-read from disk: the point is that the next start sees the settled project.
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.FindSession("main")
	if got.MemoryProject != "neublox" || !got.ProjectPinned {
		t.Fatalf("MemoryProject=%q ProjectPinned=%v, want neublox/true", got.MemoryProject, got.ProjectPinned)
	}
}

func TestSetProjectPinnedUnknownSessionIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := s.SetProjectPinned("ghost", "x"); err != nil {
		t.Fatalf("unknown session must be a silent no-op, got %v", err)
	}
}

func TestSetProjectPinnedUnchangedIsNoop(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	_ = s.AddSession(Session{Name: "main", MemoryProject: "neublox", ProjectPinned: true})
	if err := s.SetProjectPinned("main", "neublox"); err != nil {
		t.Fatalf("an already-settled project must be a no-op, got %v", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/internal/state/ -run TestSetProjectPinned
```

Expected: FAIL — `unknown field MemoryProject in struct literal` and `s.SetProjectPinned undefined`.

- [ ] **Step 3: Add the fields**

In `core/internal/state/state.go`, inside `Session`, immediately after the `Agent` line:

```go
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
```

- [ ] **Step 4: Add the setter**

Immediately after `SetResumeToken` (line 299):

```go
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
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/internal/state/
```

Expected: `ok  github.com/Herrscherd/herrscher/core/internal/state`.

- [ ] **Step 6: Commit**

```bash
git add core/internal/state
git commit -m "feat(state): remember where a session files what it learns

Three fields and a setter shaped like SetResumeToken, because the settled
project arrives the same way the resume token does — folded into a reply,
written down once, and read again by the next start."
```

---

### Task 5: manager and supervisor — the create params and the argv

**Files:**
- Modify: `core/internal/manager/commands.go:24` (after the `agent` param)
- Modify: `core/internal/manager/session.go` (after `parent, _ := in.Lookup("parent")`, line ~445; and the three `state.Session{…}` literals at lines 590, 597, 604)
- Modify: `core/internal/supervisor/supervisor.go:62-68`
- Test: `core/internal/manager/session_memory_roots_test.go` (create)
- Test: `core/internal/supervisor/supervisor_test.go` (add cases)

**Interfaces:**
- Consumes: `state.Session.{MemoryProject,MemoryAgent,ProjectPinned}` from Task 4.
- Produces: the `session create` params `memory_project`, `memory_agent`, `project_pinned`; the bridge flag `--project-pinned`.

- [ ] **Step 1: Write the failing tests**

`core/internal/manager/session_memory_roots_test.go` — the package's create tests
drive `h.sessionCreateRun` directly and read the row back with `st.FindSession`;
`session_learning_test.go` is the closest exemplar and this follows it exactly.

```go
package manager

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// A memory root lands on the row and changes nothing about where the session
// lives — that separation is the entire reason these fields are not Project.
func TestSessionCreatePersistsMemoryRoots(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true",
		"memory_project", "neublox",
		"memory_agent", "tui",
	)); err != nil {
		t.Fatal(err)
	}
	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatalf("session not persisted; sessions=%+v", st.SnapshotSessions())
	}
	if sess.MemoryProject != "neublox" || sess.MemoryAgent != "tui" {
		t.Fatalf("memory roots = %q/%q, want neublox/tui", sess.MemoryProject, sess.MemoryAgent)
	}
	if sess.ProjectPinned {
		t.Fatal("a project nobody pinned must stay revisable")
	}
	if sess.Project != "" {
		t.Fatalf("Project = %q — a memory root must not become a workspace sub-dir", sess.Project)
	}
}

// The regression that made these separate fields necessary. Routing the memory
// scope through Project would have sent this session to /workspaces/demo/neublox
// on any machine with a workspace root configured. Compare with
// TestSessionCreateNoProjectRootsAtWorkspace, which fixes the expected Dir.
func TestMemoryProjectDoesNotMoveTheSession(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if err := st.SetWorkspace("/workspaces/demo"); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true", "memory_project", "neublox",
	)); err != nil {
		t.Fatal(err)
	}
	sess, _ := st.FindSession("demo")
	if sess.Dir != "/workspaces/demo" {
		t.Fatalf("Dir = %q, want the workspace root — a memory root is not a location", sess.Dir)
	}
}

func TestSessionCreatePinsWhenAsked(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true",
		"memory_project", "neublox", "project_pinned", "true",
	)); err != nil {
		t.Fatal(err)
	}
	sess, _ := st.FindSession("demo")
	if !sess.ProjectPinned {
		t.Fatal("project_pinned did not pin")
	}
}

// A memory agent is not a provisioning directive, so it must not drag in the
// isolated-worktree rule that guards the real one.
func TestMemoryAgentDoesNotNeedAWorktree(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true", "memory_agent", "tui",
	)); err != nil {
		t.Fatalf("memory_agent on a shared session should be fine: %v", err)
	}
}

// …and it must not have relaxed it either.
func TestAgentStillNeedsAWorktree(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true", "agent", "x",
	)); err == nil {
		t.Fatal("agent on a shared session must still be refused")
	}
}

func TestInvalidMemoryProjectIsRefused(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true", "memory_project", "a/b",
	)); err == nil {
		t.Fatal("a memory project with a slash must be refused")
	}
}
```

In `core/internal/supervisor/supervisor_test.go`, alongside the existing extractor case:

```go
func TestBridgeArgsPrefersTheMemoryRoots(t *testing.T) {
	s := &Supervisor{}
	joined := strings.Join(s.bridgeArgs(state.Session{
		Name: "demo", MemoryProject: "neublox", MemoryAgent: "tui", ProjectPinned: true,
	}), " ")
	for _, want := range []string{"--project neublox", "--agent tui", "--project-pinned"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
}

// A session someone configured by hand keeps sending exactly what it sends today.
func TestBridgeArgsKeepsThePlacementFieldsWhenNoMemoryRootIsSet(t *testing.T) {
	s := &Supervisor{}
	joined := strings.Join(s.bridgeArgs(state.Session{Name: "demo", Project: "game", Agent: "scout"}), " ")
	if !strings.Contains(joined, "--project game") || !strings.Contains(joined, "--agent scout") {
		t.Fatalf("legacy scope flags changed: %s", joined)
	}
	if strings.Contains(joined, "--project-pinned") {
		t.Fatalf("nothing pinned this: %s", joined)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/internal/manager/ ./core/internal/supervisor/ -run 'MemoryRoots|MemoryProject|PinsWhenAsked|MemoryAgent|StillNeedsAWorktree|InvalidMemory|BridgeArgs'
```

Expected: FAIL — the params are unknown and `MemoryProject` is not on the row.

- [ ] **Step 3: Declare the params**

In `core/internal/manager/commands.go`, immediately after the `agent` param line:

```go
			Param("memory_project", "memory project this session files what it learns under (does NOT move the session: see project)", false).
			Param("memory_agent", "memory agent root for this session's private learned skills (does NOT provision an agent: see agent)", false).
			Param("project_pinned", "the memory project is a human's choice, not a guess: never revise it", false).
```

- [ ] **Step 4: Read and validate them**

In `core/internal/manager/session.go`, immediately after `parent, _ := in.Lookup("parent")`:

```go
	// Memory roots: where this session files what it learns, as opposed to where
	// it lives. Neither reaches repoFor, the worktree decision, or agent
	// provisioning — that separation is the whole reason they are separate fields.
	memProject, _ := in.Lookup("memory_project")
	if memProject != "" && !projectRe.MatchString(memProject) {
		return "", fmt.Errorf("invalid memory_project %q — use a single name (no /, spaces, or ..)", memProject)
	}
	memAgent, _ := in.Lookup("memory_agent")
	projectPinned := in.Bool("project_pinned")
```

- [ ] **Step 5: Put them on the row**

In each of the three `state.Session{…}` literals (lines 590, 597, 604), add after `Agent: agentName,`:

```go
MemoryProject: memProject, MemoryAgent: memAgent, ProjectPinned: projectPinned,
```

- [ ] **Step 6: Thread them to the bridge**

In `core/internal/supervisor/supervisor.go`, replace the `sess.Project` / `sess.Agent` block (lines 62-68) with:

```go
	// P1: thread the session's memory scope so the orchestrator recalls the
	// game's shared memory and this agent's private skills each turn. The bridge
	// flags are memory-only by definition, so a memory root wins over the
	// placement field of the same name: Project may be steering the session into
	// a workspace sub-directory, which says nothing about where knowledge goes.
	if p := sess.MemoryProject; p != "" {
		args = append(args, "--project", p)
	} else if sess.Project != "" {
		args = append(args, "--project", sess.Project)
	}
	if sess.Agent != "" {
		args = append(args, "--agent", sess.Agent)
	} else if sess.MemoryAgent != "" {
		args = append(args, "--agent", sess.MemoryAgent)
	}
	// Whether the bridge may revise the project on this session's first prompt.
	if sess.ProjectPinned {
		args = append(args, "--project-pinned")
	}
```

- [ ] **Step 7: Run the tests to verify they pass**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/internal/manager/ ./core/internal/supervisor/
```

Expected: `ok` for both packages.

- [ ] **Step 8: Commit**

```bash
git add core/internal/manager core/internal/supervisor
git commit -m "feat(session): create with a memory root, without moving house

memory_project and memory_agent land on the row and go straight out as the
bridge's scope flags, which were already memory-only. Nothing they touch
reaches repoFor, the worktree decision, or agent provisioning — and the
worktree rule guarding a real agent is asserted still to bite."
```

---

### Task 6: hub.create — map the three fields through

**Files:**
- Modify: `core/host/hub.go:263-296` (the `create` method)
- Modify: `core/host/hub_test.go` — `TestHubCreateMapsSpecToTypedInput` (line 97) and `TestHubCreateOmitsUnsetFields` (line 125)

**Interfaces:**
- Consumes: `contracts.CreateSession.{MemoryProject,MemoryAgent,ProjectPinned}` (Task 1); the params from Task 5.
- Produces: nothing new. This is the conduit.

- [ ] **Step 1: Extend the two tests that already guard this mapping**

The package already asserts the whole spec→Input mapping in one place, and the
omission of unset fields in another. Adding a third test would let those two go
stale; extend them instead.

In `TestHubCreateMapsSpecToTypedInput`, add the three fields to the spec literal:

```go
	if _, err := h.Create(context.Background(), contracts.CreateSession{
		Name: "main", Project: "alpha", Gateways: []string{"chat", "terminal"},
		TerminalOnly: true, Shared: true, Agent: "bishop", ConsolidateEvery: 3, Base: "session/a",
		MemoryProject: "neublox", MemoryAgent: "tui", ProjectPinned: true,
	}); err != nil {
		t.Fatal(err)
	}
```

and, after the `consolidate_every` assertion:

```go
	// A memory root travels as its own flag, never folded into project/agent,
	// because the command reads those two as a location and a provisioning ask.
	if got.Get("memory_project") != "neublox" || got.Get("memory_agent") != "tui" {
		t.Fatalf("memory roots not mapped: %+v", got.Args)
	}
	if !got.Bool("project_pinned") {
		t.Fatalf("project_pinned not mapped: %+v", got.Args)
	}
```

In `TestHubCreateOmitsUnsetFields`, extend the key list:

```go
	for _, k := range []string{"project", "gateways", "agent", "shared", "terminal_only", "consolidate_every", "base", "memory_project", "memory_agent", "project_pinned"} {
```

- [ ] **Step 2: Run them to verify they fail**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/host/ -run TestHubCreate
```

Expected: FAIL — `memory roots not mapped`.

- [ ] **Step 3: Map them**

In `core/host/hub.go`, in `create`, immediately after `setStr("agent", spec.Agent)`:

```go
	setStr("memory_project", spec.MemoryProject)
	setStr("memory_agent", spec.MemoryAgent)
```

and immediately after the `if spec.Shared {…}` block:

```go
	if spec.ProjectPinned {
		args["project_pinned"] = "true"
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/host/ -run TestHubCreate
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/host/hub.go core/host/hub_test.go
git commit -m "feat(hub): carry the memory roots through to session create"
```

---

### Task 7: turnloop — write down the project the way we write down the resume

**Files:**
- Modify: `core/host/turnloop.go` — driver fields at 123-129, the `reply{done}` branch at 625-632, `recordEntry` at 238-241, `RunSession` at 881, `runSessionIdentified` at 885-897
- Modify: `core/host/hub.go:111-130` (the call site)
- Modify: `core/host/turnloop_resume_test.go:14,29` — the only tests that set the callback by hand
- Modify: `core/host/driventap_test.go:42`, `core/host/turnloop_test.go:361,548,579`
- Test: `core/host/turnloop_project_test.go` (create)

**Interfaces:**
- Consumes: `contracts.Event.Project` (Task 1).
- Produces: `type sessionSink struct { Resume func(string); Project func(string); Transcript func(state.TranscriptEntry) }`, and the new signatures
  `func RunSession(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, sink sessionSink, gate budgetGate)`
  `func runSessionIdentified(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, sink sessionSink, emit func(contracts.Event), identity sessionIdentity, gate budgetGate)`.

The struct exists because `RunSession` already carries eleven positional parameters and a twelfth callback would make every call site unreadable. The three it replaces are the same kind of thing: a fact about a completed turn the daemon must write down.

- [ ] **Step 1: Write the failing test**

`core/host/turnloop_project_test.go` — `turnloop_resume_test.go` already drives
exactly this branch through `newSessionDriver` + `awaitTurn`, with no sockets and
no goroutines. The project write is the same branch, so it gets the same harness.

```go
package host

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// The settled project comes home on reply{done} beside the resume token, and
// both are written. One event, two durable facts.
func TestAwaitTurnPersistsSettledProject(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from)
	var gotResume, gotProject string
	d.sink.Resume = func(tok string) { gotResume = tok }
	d.sink.Project = func(p string) { gotProject = p }

	from <- contracts.Event{T: "reply", Text: "ok", Done: true, Resume: "sid-1", Project: "neublox"}
	if !d.awaitTurn(context.Background(), tokenGuard{}) {
		t.Fatal("awaitTurn should return true on reply{done}")
	}
	if gotResume != "sid-1" {
		t.Fatalf("resume = %q, want sid-1", gotResume)
	}
	if gotProject != "neublox" {
		t.Fatalf("project = %q, want neublox", gotProject)
	}
}

// Every turn after the first settles nothing, and must not rewrite the row.
func TestAwaitTurnSkipsAnEmptyProject(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from)
	called := false
	d.sink.Project = func(string) { called = true }

	from <- contracts.Event{T: "reply", Text: "ok", Done: true} // nothing settled
	_ = d.awaitTurn(context.Background(), tokenGuard{})
	if called {
		t.Fatal("a turn that settled nothing must not pin anything")
	}
}
```

Then update the two existing tests in `core/host/turnloop_resume_test.go`, which
set the callback field directly: `d.persistResume = …` becomes
`d.sink.Resume = …` at lines 14 and 29.

- [ ] **Step 2: Run it to verify it fails**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/host/ -run TestAwaitTurn
```

Expected: FAIL — `undefined: sessionSink`.

- [ ] **Step 3: Introduce the struct**

In `core/host/turnloop.go`, replace the `persistResume` and `record` field declarations (lines 122-129) with:

```go
	// sink is where a completed turn's durable side effects go. The three travel
	// together because they are the same kind of fact — something about the turn
	// the daemon has to write down — and because a nil field is how a caller with
	// nowhere to write (tests, the short-lived operator CLI path) says so.
	sink sessionSink
```

and add, just above the driver struct:

```go
// sessionSink collects the writes a completed turn owes the daemon: the
// backend's resume token, the memory project the session settled on, and the
// transcript entry. Any field may be nil, which disables that write.
type sessionSink struct {
	// Resume folds a completed turn's backend resume token into durable state so
	// a restart resumes the conversation instead of starting it over.
	Resume func(token string)
	// Project records the memory project the bridge settled on for this session,
	// once, on its first prompt.
	Project func(project string)
	// Transcript appends one entry to the session log.
	Transcript func(state.TranscriptEntry)
}
```

- [ ] **Step 4: Move the three uses over**

- Line 238-241 (`recordEntry`): `if d.record == nil` → `if d.sink.Transcript == nil`, and `d.record(state.TranscriptEntry{…})` → `d.sink.Transcript(state.TranscriptEntry{…})`.
- Lines 628-630, in the `reply{done}` branch, replace with:

```go
				if d.sink.Resume != nil && e.Resume != "" {
					d.sink.Resume(e.Resume)
				}
				// The project a first prompt settled comes home the same way, and
				// for the same reason: the bridge knows it and only the daemon can
				// write it down.
				if d.sink.Project != nil && e.Project != "" {
					d.sink.Project(e.Project)
				}
```

- Lines 881-897: swap the two positional callbacks for `sink sessionSink` in both signatures, pass it through, and replace the two assignments with `d.sink = sink`.

- [ ] **Step 5: Update the four call sites**

`core/host/hub.go:111` — the two inline closures become named fields, and the project write joins them:

```go
		runSessionIdentified(sctx, sess.Name, sess.ChannelID, bound, acc, state.ParticipantsPath(h.partDir, sess.Name), h.metrics, h.coordinator,
			// None of these can return an error to its caller, so the only thing
			// left to do with a failure is say it. All three are worth saying: a
			// lost resume token means the next restart silently starts the
			// conversation over, a lost project means the session re-guesses its
			// scope on every start, and a lost transcript entry means the session
			// log quietly has a hole in it. Failing here is a full disk or a
			// permission problem — rare, and exactly the kind of thing nobody
			// finds without a line in the log.
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
```

Keep whatever arguments follow on the existing call; only the two callbacks collapse into one.

`core/host/turnloop_test.go:361,548,579` — the three `RunSession` calls end in five
nils; the middle two of those are the callbacks, so each becomes:

```go
	go RunSession(ctx, "s1", "", []contracts.GatewaySet{{Gateway: a, Reader: a}}, acc, "", nil, nil, sessionSink{}, nil)
```

`core/host/driventap_test.go:42` — the same collapse, one line earlier in the argument list:

```go
	go runSessionIdentified(ctx, "driventap-test", "", nil, acc, "", nil, nil, sessionSink{},
		eventTap(func(_ string, e contracts.Event) { got <- e }, "driventap-test"),
		sessionIdentity{}, nil)
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go build ./... && go test ./core/host/
```

Expected: `ok  github.com/Herrscherd/herrscher/core/host`.

- [ ] **Step 7: Commit**

```bash
git add core/host
git commit -m "feat(turnloop): write down the project a session settled on

Same shape as the resume token, so it takes the same road: folded into
reply{done} by the bridge, written by the daemon. The two callbacks and the
transcript one become a sessionSink, because a twelfth positional parameter
would have made every call site unreadable."
```

---

### Task 8: the bridge — settle the project on the first prompt

**Files:**
- Modify: `core/bridge/bridge.go:25-36` (`Options`) and the `RunOneShot` call at line 74
- Modify: `core/bridge/hub.go:119-157` (`runHub`), 176-202 (`runHubTurns`, `runHubTurnsCtl`), 204-276 (`runOneTurn`)
- Modify: `bridge.go` at the repository root (the flag, the resolver, the Options literal)
- Test: `core/bridge/scope_pin_test.go` (create)
- Test: `bridge_scope_test.go` at the repository root (create)

**Interfaces:**
- Consumes: `contracts.Event.Project` (Task 1); `Curator.SetScope` (Task 2); `scope.MatchProject` (Task 3).
- Produces: `type ScopeResolver interface { Resolve(ctx context.Context, prompt string) string }`; the `Options` fields `Scope ScopeResolver`, `LaunchProject string`, `ProjectPinned bool`, `MemoryAgent string`; and `type vaultScopeResolver struct{ mem contracts.Memory; log *slog.Logger }` in package main.

The split of responsibility is deliberate: the binary owns memory, so it owns both the lookup and the provisioning of a new root; `core/bridge` owns the orchestrator, so it owns the `SetScope` type assertion. Neither reaches into the other's world.

- [ ] **Step 1: Write the failing test**

`core/bridge/scope_pin_test.go`:

```go
package bridge

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type fixedResolver struct {
	answer string
	asked  int
}

func (r *fixedResolver) Resolve(context.Context, string) string {
	r.asked++
	return r.answer
}

type scopedOrch struct {
	contracts.Orchestrator
	got contracts.MemoryScope
}

func (o *scopedOrch) SetScope(s contracts.MemoryScope) { o.got = s }

func TestPinSettlesOnceAndTellsTheOrchestrator(t *testing.T) {
	r := &fixedResolver{answer: "neublox"}
	o := &scopedOrch{}
	p := &scopePin{resolve: r, current: "herrscher", agent: "tui", orch: o}

	if got := p.settle(context.Background(), "je bosse sur neublox"); got != "neublox" {
		t.Fatalf("settle = %q, want neublox", got)
	}
	if o.got.Project != contracts.ProjectKey("neublox") || o.got.Agent != contracts.AgentKey("tui") {
		t.Fatalf("scope = %+v, want both roots", o.got)
	}
	if got := p.settle(context.Background(), "et maintenant herrscher"); got != "" {
		t.Fatalf("a second turn re-opened the question: %q", got)
	}
	if r.asked != 1 {
		t.Fatalf("resolver asked %d times, want 1", r.asked)
	}
}

// A prompt that names nothing leaves the scope alone — but still pins, so the
// question is asked once per session and not once per turn.
func TestPinKeepsTheLaunchCandidateWhenNothingIsNamed(t *testing.T) {
	o := &scopedOrch{}
	p := &scopePin{resolve: &fixedResolver{answer: ""}, current: "herrscher", agent: "tui", orch: o}
	if got := p.settle(context.Background(), "on continue"); got != "herrscher" {
		t.Fatalf("settle = %q, want the launch candidate back", got)
	}
	if o.got.Project != "" {
		t.Fatal("nothing changed, so nothing should have been re-rooted")
	}
}

// With no candidate and no match there is nothing to write down.
func TestPinStaysSilentWithNothingToSay(t *testing.T) {
	p := &scopePin{resolve: &fixedResolver{}, orch: &scopedOrch{}}
	if got := p.settle(context.Background(), "salut"); got != "" {
		t.Fatalf("settle = %q, want empty", got)
	}
}

// An orchestrator that cannot be re-rooted is not an error: the scope stays as
// built and the event still carries the project, so the row is right next start.
func TestPinToleratesAnOrchestratorThatCannotBeRerooted(t *testing.T) {
	p := &scopePin{resolve: &fixedResolver{answer: "neublox"}, current: "herrscher", orch: nil}
	if got := p.settle(context.Background(), "neublox"); got != "neublox" {
		t.Fatalf("settle = %q, want neublox", got)
	}
}

// A session whose project a human chose gets no pin at all, and every turn of it
// must go through settle without touching anything. This is the nil runHub builds
// when Options.ProjectPinned is set.
func TestNoPinSettlesNothing(t *testing.T) {
	var p *scopePin
	if got := p.settle(context.Background(), "neublox"); got != "" {
		t.Fatalf("settle = %q, want empty — a pinned session is never re-scoped", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./core/bridge/ -run TestPin
```

Expected: FAIL — `undefined: scopePin`.

- [ ] **Step 3: Add the resolver seam to Options**

In `core/bridge/bridge.go`, inside `Options`:

```go
	// Scope decides which memory project a session's conversation is actually
	// about, on its first prompt. It is injected because the answer lives in the
	// vault and the bridge package holds no memory of its own — which is also
	// what lets the turn driver be tested without one. Nil never settles
	// anything, and neither does a session whose project a human already chose.
	Scope ScopeResolver
	// LaunchProject is the memory project the session was created with, and
	// ProjectPinned says a human chose it rather than the host guessing from a
	// directory. Only a guess is ever revised.
	LaunchProject string
	ProjectPinned bool
	// MemoryAgent is the private memory root, carried so a re-rooted scope keeps
	// both halves of the tree instead of dropping the agent on the way.
	MemoryAgent string
```

and, above `Options`:

```go
// ScopeResolver answers which memory project a prompt is about, having ensured
// that project's root exists. "" means "none of the ones I know", which the
// caller reads as "keep the scope you launched with".
type ScopeResolver interface {
	Resolve(ctx context.Context, prompt string) string
}
```

- [ ] **Step 4: Add the pin and thread it through the turn**

In `core/bridge/hub.go`, below the `affordances` type (line 169):

```go
// scopePin is a session's one-shot answer to "what is this conversation about".
// It sits beside affordances rather than inside it because affordances are
// prompt blocks, and this is not one: it changes where memory is written.
type scopePin struct {
	resolve ScopeResolver
	current string // the project the session launched with ("" = none)
	agent   string // the private root, preserved across a re-rooting
	orch    contracts.Orchestrator
	settled bool
}

// settle answers the project this session should be recorded under, and returns
// "" once it has already answered — a session is asked once, not once per turn.
// A resolver that names nothing leaves the scope alone but still returns the
// launch candidate, so the daemon pins it and no later turn re-opens the
// question. It is best-effort in the orchestrator's sense: an orchestrator that
// cannot be re-rooted still gets the name into the event, so the row is right on
// the next start.
func (p *scopePin) settle(ctx context.Context, prompt string) string {
	if p == nil || p.settled {
		return ""
	}
	p.settled = true
	chosen := p.resolve.Resolve(ctx, prompt)
	if chosen == "" || chosen == p.current {
		return p.current
	}
	if s, ok := p.orch.(interface{ SetScope(contracts.MemoryScope) }); ok {
		scope := contracts.MemoryScope{Project: contracts.ProjectKey(chosen)}
		if p.agent != "" {
			scope.Agent = contracts.AgentKey(p.agent)
		}
		s.SetScope(scope)
	}
	return chosen
}
```

Then add `pin *scopePin` as the last parameter of `runHubTurnsCtl` and `runOneTurn`, and:

- `runHubTurns` (line 177) passes `nil`.
- `RunOneShot` in `bridge.go` (line 74) passes `nil` — one turn with no supervising daemon has nothing to pin.
- `runHub` (line 157) builds it:

```go
	var pin *scopePin
	if o.Scope != nil && !o.ProjectPinned {
		pin = &scopePin{resolve: o.Scope, current: o.LaunchProject, agent: o.MemoryAgent, orch: orch}
	}
	runHubTurnsCtl(ctx, in, conn, backend, orch, ctrl, eng, affordances{roster: o.Roster, caps: o.Capabilities}, pin)
```

- In `runOneTurn`, immediately before `memCtx = orch.Context(turnCtx)` — the order matters, because the first turn's own prompt must already be assembled against the scope it settles:

```go
	// Settle the memory scope before the context is built, so this very turn is
	// recalled against the project it belongs to rather than the one the
	// directory guessed.
	settledProject := pin.settle(turnCtx, ev.Text)
```

and at the `reply{done}` emit (line 276) add `Project: settledProject` to the `contracts.Event{…}` literal.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go build ./... && go test ./core/bridge/
```

Expected: `ok  github.com/Herrscherd/herrscher/core/bridge`.

- [ ] **Step 6: Wire the real resolver in the binary**

In `bridge.go` at the repository root, add the flag beside the others:

```go
	projectPinned := fs.Bool("project-pinned", false, "the memory project was chosen by a human, not guessed from a directory: never revise it")
```

Add, near `provisionScope` (line 179):

```go
// vaultScopeResolver answers which known project a prompt is about, and makes
// sure that project's root exists before saying so. It is the bridge's half of
// chantier 1's deliberately dull matching rule: everything it knows comes out of
// the vault, so the registry that replaces the rule replaces this and nothing
// above it.
type vaultScopeResolver struct {
	mem contracts.Memory
	log *slog.Logger
}

func (r vaultScopeResolver) Resolve(ctx context.Context, prompt string) string {
	nodes, err := r.mem.Search(ctx, contracts.Query{Kinds: []contracts.NodeKind{contracts.KindProject}})
	if err != nil {
		// A vault that cannot be read is a session that keeps the project its
		// launch guessed. Learning never breaks a turn.
		r.log.Warn("list known projects", "err", err)
		return ""
	}
	known := make([]string, 0, len(nodes))
	for _, n := range nodes {
		// The name is the key's last segment: projects/<name>. Title is a human
		// label and may be anything, so it is not what a scope is keyed by.
		known = append(known, strings.TrimPrefix(n.Key, "projects/"))
	}
	chosen := scope.MatchProject(prompt, known)
	if chosen == "" {
		return ""
	}
	if p, ok := r.mem.(contracts.Provisioner); ok {
		if err := p.EnsureProject(ctx, contracts.ProjectKey(chosen), chosen); err != nil {
			r.log.Warn("ensure project root", "project", chosen, "err", err)
		}
	}
	return chosen
}
```

`contracts.Provisioner` is `EnsureProject(ctx, key, title string) error` plus `EnsureAgent` with the same shape — the same assertion `provisionScope` (line 179) already makes.

Then extend the `bridge.Options` literal:

```go
	opts := bridge.Options{
		Channel:      *ch,
		HubSocket:    *hubSocket,
		Roster:       host.NewRoster(rosterRoot),
		Capabilities: os.Getenv(host.EnvCapabilities),
		LaunchProject: *project,
		ProjectPinned: *projectPinned,
		MemoryAgent:   *agent,
	}
	if mem != nil {
		opts.Scope = vaultScopeResolver{mem: mem, log: log}
	}
	return bridge.Run(ctx, newBackend, orch, opts)
```

Add `"strings"`, `"log/slog"` and `"github.com/Herrscherd/herrscher/core/scope"` to the imports as needed.

- [ ] **Step 7: Test the resolver against the vault**

`bridge_scope_test.go` at the repository root. The package already has
`recordingMem` in `bridge_provision_test.go`, which implements `Memory` and
`Provisioner` and records what it ensured — add one field to it rather than a
second fake:

```go
func (m *recordingMem) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return m.known, m.searchErr
}
```

with `known []contracts.Node` and `searchErr error` added to the struct. Then:

```go
package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestResolverPicksAKnownProjectAndEnsuresItsRoot(t *testing.T) {
	m := &recordingMem{known: []contracts.Node{
		{Key: "projects/herrscher", Kind: contracts.KindProject},
		{Key: "projects/neublox", Kind: contracts.KindProject},
	}}
	r := vaultScopeResolver{mem: m, log: slog.Default()}

	if got := r.Resolve(context.Background(), "je bosse sur neublox"); got != "neublox" {
		t.Fatalf("Resolve = %q, want neublox", got)
	}
	if len(m.projects) != 1 || m.projects[0] != [2]string{"projects/neublox", "neublox"} {
		t.Fatalf("the chosen root was not ensured: %+v", m.projects)
	}
}

func TestResolverIsSilentWhenThePromptNamesNothingKnown(t *testing.T) {
	m := &recordingMem{known: []contracts.Node{{Key: "projects/herrscher", Kind: contracts.KindProject}}}
	r := vaultScopeResolver{mem: m, log: slog.Default()}

	if got := r.Resolve(context.Background(), "on continue"); got != "" {
		t.Fatalf("Resolve = %q, want empty", got)
	}
	if len(m.projects) != 0 {
		t.Fatalf("nothing was chosen, so nothing should have been ensured: %+v", m.projects)
	}
}

// A vault that cannot be read is a session that keeps its launch guess. Learning
// never breaks a turn — least of all the first one.
func TestResolverSurvivesAnUnreadableVault(t *testing.T) {
	m := &recordingMem{searchErr: errors.New("vault gone")}
	r := vaultScopeResolver{mem: m, log: slog.Default()}

	if got := r.Resolve(context.Background(), "neublox"); got != "" {
		t.Fatalf("Resolve = %q, want empty", got)
	}
}
```

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test . -run TestResolver
```

Expected: PASS.

- [ ] **Step 8: Build and run the full suite**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'
```

Expected: no output after the filter.

- [ ] **Step 9: Commit**

```bash
git add core/bridge bridge.go bridge_scope_test.go bridge_provision_test.go
git commit -m "feat(bridge): settle a guessed project on the first prompt

The bridge is the one place holding both the vault and what was actually
said, so it is where the question gets answered — once, before the turn's
context is built, so the turn that settles the scope is already recalled
under it. The answer rides home on reply{done}, like the resume token."
```

---

### Task 9: the terminal plugin — a launch that learns

**Files:**
- Modify: `plugins/terminal/terminal.go` — the manifest at 35-42, `newGatewaySet` at 44-47, the `Terminal` struct at 52-58, `New()` at 254-261, `openDefaultSession` at 110-116, `bootstrapSession` at 168-186
- Test: `plugins/terminal/learning_test.go` (create)

**Interfaces:**
- Consumes: `contracts.CreateSession.{MemoryProject,MemoryAgent,ProjectPinned}` (Task 1); `scope.ProjectFromDir` (Task 3); `hub.create`'s mapping (Task 6).
- Produces: the six settings in the spec's table, and `openDefaultSession(ctx context.Context, c contracts.SessionControl, cfg contracts.PluginConfig) (string, error)`.

- [ ] **Step 1: Write the failing test**

`plugins/terminal/learning_test.go`:

```go
package terminal

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// cfgOf resolves the plugin's own declared settings against a fake environment,
// so the test exercises the defaults the manifest actually ships rather than a
// second copy of them.
func cfgOf(t *testing.T, env map[string]string) contracts.PluginConfig {
	t.Helper()
	cfg, err := contracts.Resolve(settings(), func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDefaultSessionAsksToLearn(t *testing.T) {
	c := &fakeSessionControl{} // already in terminal_test.go:207
	if _, err := openDefaultSession(context.Background(), c, cfgOf(t, nil)); err != nil {
		t.Fatal(err)
	}
	if len(c.created) != 1 {
		t.Fatalf("created %d sessions, want 1", len(c.created))
	}
	spec := c.created[0]
	if spec.Extractor != "llm" {
		t.Fatalf("Extractor = %q, want llm — without one nothing is ever distilled", spec.Extractor)
	}
	if spec.ConsolidateEvery != 10 {
		t.Fatalf("ConsolidateEvery = %d, want 10", spec.ConsolidateEvery)
	}
	if spec.MemoryAgent != "tui" {
		t.Fatalf("MemoryAgent = %q, want tui", spec.MemoryAgent)
	}
	if spec.ProjectPinned {
		t.Fatal("a project resolved from a directory is a guess, not a choice")
	}
	if !spec.TerminalOnly || !spec.Shared {
		t.Fatal("a learning session is still a terminal tab in the main checkout")
	}
}

func TestLearnFalseCreatesTodaysSession(t *testing.T) {
	c := &fakeSessionControl{}
	if _, err := openDefaultSession(context.Background(), c, cfgOf(t, map[string]string{"TERMINAL_LEARN": "false"})); err != nil {
		t.Fatal(err)
	}
	spec := c.created[0]
	if spec.Extractor != "" || spec.ConsolidateEvery != 0 || spec.MemoryAgent != "" || spec.MemoryProject != "" {
		t.Fatalf("learn=false must send exactly what it sends today, got %+v", spec)
	}
}

func TestAnExplicitProjectIsPinned(t *testing.T) {
	c := &fakeSessionControl{}
	cfg := cfgOf(t, map[string]string{"TERMINAL_PROJECT": "neublox"})
	if _, err := openDefaultSession(context.Background(), c, cfg); err != nil {
		t.Fatal(err)
	}
	if c.created[0].MemoryProject != "neublox" || !c.created[0].ProjectPinned {
		t.Fatalf("an operator who named the project must not be second-guessed: %+v", c.created[0])
	}
}

func TestPinAtLaunchNeverAsksTheFirstPrompt(t *testing.T) {
	c := &fakeSessionControl{}
	cfg := cfgOf(t, map[string]string{"TERMINAL_PROJECT_PIN": "launch"})
	if _, err := openDefaultSession(context.Background(), c, cfg); err != nil {
		t.Fatal(err)
	}
	if !c.created[0].ProjectPinned {
		t.Fatal("project-pin=launch must pin what the directory gave")
	}
}

func TestAMemoryAgentIsOverridable(t *testing.T) {
	c := &fakeSessionControl{}
	cfg := cfgOf(t, map[string]string{"TERMINAL_MEMORY_AGENT": "scout"})
	if _, err := openDefaultSession(context.Background(), c, cfg); err != nil {
		t.Fatal(err)
	}
	if c.created[0].MemoryAgent != "scout" {
		t.Fatalf("MemoryAgent = %q, want scout", c.created[0].MemoryAgent)
	}
}
```

`fakeSessionControl` is already in `plugins/terminal/terminal_test.go:186` and its `Create` appends every spec to `f.created`. Reuse it as it is — it needs no changes.

- [ ] **Step 2: Run it to verify it fails**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./plugins/terminal/ -run 'TestDefaultSessionAsks|TestLearnFalse|TestAnExplicit|TestPinAtLaunch|TestAMemoryAgent'
```

Expected: FAIL — `undefined: settings` and `too many arguments in call to openDefaultSession`.

- [ ] **Step 3: Declare the settings**

In `plugins/terminal/terminal.go`, above `init()`:

```go
// Setting keys. A launch reads them off the manifest like any plugin, so what
// the window does can be changed without touching the window.
const (
	setLearn      = "learn"
	setExtractor  = "extractor"
	setEvery      = "consolidate-every"
	setMemAgent   = "memory-agent"
	setProject    = "project"
	setProjectPin = "project-pin"

	// pinAtLaunch is the project-pin value that takes the directory's answer as
	// final. Anything else means the session's first prompt may revise it.
	pinAtLaunch = "launch"
)

// settings declares what a launch can be told. Every one is optional and carries
// a default, so a build with no environment behaves exactly as the table in
// docs/superpowers/specs/2026-08-15-tui-session-learning-design.md describes:
// the window learns, under the project it is standing in, as itself.
func settings() []contracts.Setting {
	return []contracts.Setting{
		{Key: setLearn, Env: "TERMINAL_LEARN", Default: "true",
			Help: "the session a launch opens learns: false restores a session that records nothing"},
		{Key: setExtractor, Env: "TERMINAL_EXTRACTOR", Default: "llm",
			Help: "registered curation extractor that distils the transcript"},
		{Key: setEvery, Env: "TERMINAL_CONSOLIDATE_EVERY", Default: "10",
			Help: "turns between consolidation passes (0 = manual/idle only)"},
		{Key: setMemAgent, Env: "TERMINAL_MEMORY_AGENT", Default: "tui",
			Help: "memory root for what the window learns as itself"},
		{Key: setProject, Env: "TERMINAL_PROJECT", Default: "",
			Help: "force the memory project and pin it, instead of resolving one"},
		{Key: setProjectPin, Env: "TERMINAL_PROJECT_PIN", Default: "first-turn",
			Help: "when the project is settled: first-turn (the prompt may revise it) | launch (the directory is final)"},
	}
}
```

and add `Config: settings(),` to the `contracts.Manifest{…}` literal in `init()`.

- [ ] **Step 4: Keep the config instead of discarding it**

`newGatewaySet` currently drops `cfg` on the floor:

```go
func newGatewaySet(ctx context.Context, cfg contracts.PluginConfig) (contracts.GatewaySet, error) {
	tm := New()
	tm.cfg = cfg
	return contracts.GatewaySet{Gateway: tm, Reader: tm, Admin: tm}, nil
}
```

Add the field to the `Terminal` struct:

```go
	// cfg is the resolved manifest configuration. It is kept because a launch
	// decides what its session should carry, and that decision is configuration.
	cfg contracts.PluginConfig
```

and pass it at the one call site, in `bootstrapSession` (line 179):

```go
		name, err := openDefaultSession(ctx, c, t.cfg)
```

- [ ] **Step 5: Say what a launch wants**

Replace `openDefaultSession` (keeping the existing doc comment above it, and extending it with the paragraph below):

```go
// …existing comment…
//
// It is also where a launch says what it wants to keep. A window that learns
// nothing is the whole of the moat problem: the tool the operator uses all day
// contributing nothing to what the next session knows. So unless told otherwise,
// the session it opens files what it learns under the project it is standing in
// and under its own agent root — a guess, deliberately left revisable, because
// the directory is where you are and not always what you are doing.
func openDefaultSession(ctx context.Context, c contracts.SessionControl, cfg contracts.PluginConfig) (string, error) {
	name := defaultSessionName(c.Sessions())
	spec := contracts.CreateSession{Name: name, TerminalOnly: true, Shared: true}
	if boolSetting(cfg, setLearn, true) {
		spec.MemoryAgent = cfg.Get(setMemAgent)
		spec.Extractor = cfg.Get(setExtractor)
		spec.ConsolidateEvery = intSetting(cfg, setEvery, 0)
		if p := cfg.Get(setProject); p != "" {
			// An operator who named the project is not guessing.
			spec.MemoryProject, spec.ProjectPinned = p, true
		} else {
			spec.MemoryProject = scope.ProjectFromDir(cwd())
			spec.ProjectPinned = spec.MemoryProject != "" && cfg.Get(setProjectPin) == pinAtLaunch
		}
	}
	if _, err := c.Create(ctx, spec); err != nil {
		return "", err
	}
	return name, nil
}

// cwd is the directory herrscher was launched in — what the operator means by
// "here". An unreadable one is not an error worth failing a launch over; it
// simply means the session starts with no project, exactly as it does today.
func cwd() string {
	d, err := os.Getwd()
	if err != nil {
		return ""
	}
	return d
}

// boolSetting reads a declared boolean setting, falling back to def for anything
// strconv does not recognise — a typo in an environment variable must not decide
// whether the window learns.
func boolSetting(cfg contracts.PluginConfig, key string, def bool) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(cfg.Get(key)))
	if err != nil {
		return def
	}
	return v
}

// intSetting reads a declared integer setting, falling back to def for anything
// unparseable or negative.
func intSetting(cfg contracts.PluginConfig, key string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(cfg.Get(key)))
	if err != nil || n < 0 {
		return def
	}
	return n
}
```

Add `"github.com/Herrscherd/herrscher/core/scope"` to the imports. `os`, `strconv` and `strings` are already there.

- [ ] **Step 6: Run the tests to verify they pass**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
go test ./plugins/terminal/... && go build ./...
```

Expected: `ok  github.com/Herrscherd/herrscher/plugins/terminal`.

- [ ] **Step 7: Commit**

```bash
git add plugins/terminal
git commit -m "feat(terminal): a launch that keeps what it learned

openDefaultSession sent three fields and got a session that recorded
nothing. It now says which project and which agent root the window files
under, and which extractor distils it — all six of them settings, and
learn=false is the one switch back to the session this used to open."
```

---

### Task 10: land the dependencies, bump, and write it down

**Files:**
- Modify: `/home/shan/dev/go.work` (add nothing — it already covers all three)
- Modify: `go.mod`, `go.sum` in this worktree
- Modify: `docs/reference/configuration.md` and its French counterpart (check `docs/` for the real paths, and `nav.js` if a new page is added)
- Delete: `go.work` from Task 0

- [ ] **Step 1: Land and tag contracts**

```bash
cd /home/shan/dev/herrscher-contracts
gh pr create --fill --base master && gh pr merge --squash --delete-branch
git checkout master && git pull
git tag v0.4.0 && git push origin v0.4.0
```

- [ ] **Step 2: Land and tag the orchestrator**

The orchestrator's own `go.mod` requires contracts; bump it first if `SetScope` needs anything from v0.4.0 (it does not — it uses `MemoryScope`, which is v0.3.0).

```bash
cd /home/shan/dev/herrscher-orchestrator
gh pr create --fill --base master && gh pr merge --squash --delete-branch
git checkout master && git pull
git tag v0.2.0 && git push origin v0.2.0
```

- [ ] **Step 3: Bump herrscher and drop the workspace**

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
rm -f go.work go.work.sum
go get github.com/Herrscherd/herrscher-contracts@v0.4.0
go get github.com/Herrscherd/herrscher-orchestrator@v0.2.0
go mod tidy
go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'
```

Expected: no output after the filter. If the build now fails where it passed under `go.work`, a tag is missing or wrong — do not re-add `go.work` to paper over it.

- [ ] **Step 4: Document the settings**

Find where gateway plugin settings are documented:

```bash
cd /home/shan/.superset/worktrees/b8801324-34e6-4dc7-a9de-e72d04ec8335/transparent-pentaceratops
grep -rn "DISCORD_TOKEN\|OBSIDIAN_VAULT" docs/ | head
```

Add the six `TERMINAL_*` variables to the same table, in both language versions, saying for each what it changes and what happens when it is unset. Lead with the sentence that matters: a launch learns by default, and `TERMINAL_LEARN=false` is how you stop it.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum docs
git commit -m "chore(deps): contracts v0.4.0, orchestrator v0.2.0

And write down the six TERMINAL_* settings a launch now reads, since a
default that changes what gets recorded is a default worth documenting."
```

- [ ] **Step 6: Check the thing actually works**

Not a test — the reason for all of the above. In a git repository, with a vault configured:

```bash
cd /home/shan/dev/herrscher && go run . 2>/dev/null
```

Then, in the window: say something, wait for ten turns or run a manual consolidation, and look in the vault for `projects/herrscher/` and `agents/tui/`. Nodes under both is the whole feature. Nothing under either, with no warning in `herrscher serve`'s stderr, means the extractor never resolved — check `TERMINAL_EXTRACTOR` names a registered one.

---

## Notes for whoever executes this

- Tasks 1, 2, 3 and 4 touch four disjoint sets of files and may run at the same time. 5 needs 4. 6 needs 1 and 5. 7 needs 1. 8 needs 1, 2 and 3. 9 needs 1, 3 and 6. 10 needs everything.
- Every test above uses a harness the package already has: `newTestHandler`/`args`/`FindSession` in manager, `hubWith` in host, `newSessionDriver`/`awaitTurn` in the turn loop, `fakeSessionControl` in the terminal plugin, `newFake` in the orchestrator, `recordingMem` at the root. Do not build a second one. Two of the tasks extend an existing test rather than adding a neighbour, for the same reason: a second test asserting the same mapping lets the first go stale.
- The one piece of this expected to be wrong in the field is `MatchProject`. It is a pure function taking its whole world as arguments so chantier 2 can replace it without touching anything above it. Resist making it cleverer here.
