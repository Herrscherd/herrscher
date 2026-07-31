# G6 Bounded USER.md Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a bounded `USER.md` user-profile file to the agent home, materialized into the session worktree so the agent reads it, size-bounded by the G1 budget policy.

**Architecture:** A shared `contracts.EnforceBudget` helper expresses the rune-budget policy. `core/internal/agent` gains a `USER.md` home file, a `Store.SetUser` write path that enforces the budget, and `Materialize` support that writes `.claude/USER.md` + references it from `CLAUDE.md` (via Claude Code `@import`) and inlines it into `AGENTS.md` (for Codex). The host wires an `AGENT_USER_BUDGET` env override.

**Tech Stack:** Go 1.25; `github.com/Herrscherd/herrscher` (host, single module) + `github.com/Herrscherd/herrscher-contracts`.

## Global Constraints

- Budget is counted in **runes**, not bytes (`utf8.RuneCountInString`).
- Default USER.md budget = **1500** runes; a limit **`<= 0` disables** the check.
- The budget error is `*contracts.BudgetError`, matchable with `errors.As`.
- `USER.md` **absent is valid**: `Materialize` then emits `CLAUDE.md` and `AGENTS.md` byte-identical to today (soul verbatim) — regression-locked.
- Materialization strings are exact: `CLAUDE.md` ends with the line `@USER.md` (bare sibling — Claude Code resolves relative imports against CLAUDE.md's own `.claude/` dir, so no `.claude/` prefix); `AGENTS.md` inlines under a `# User` heading.
- **Ports only, policy not engine**: no new storage; budget lives in `contracts`.
- `SetUser` overwrites (reversible); it never hard-deletes.
- **`contracts.EnforceBudget` already ships in the released v0.2.9** (verified 2026-07-30 in `v0.2.9:budget.go`; the host already depends on v0.2.9). G6 needs **no contracts change, no new tag, no `go.work` overlay** — it is host-only. Tasks 0 and 1 below are already satisfied and are SKIPPED; execution starts at Task 2.
- `core/internal/agent` is part of the host module and ships with the host (no obsidian tag). Enforcement site in obsidian is **not** migrated in this slice.

---

## Task 0: Dev workspace overlay (main agent, setup) — SKIPPED (not needed)

**SKIPPED 2026-07-30:** `EnforceBudget` is already in the released contracts
v0.2.9 that the host depends on, so no local-HEAD overlay is required. The
original steps are retained below for the record only.



**Files:**
- Create: `<host-worktree>/go.work` (untracked, dev-only)

Not a TDD task — a one-time setup so the host module compiles against the local, not-yet-tagged contracts.

- [ ] **Step 1: Create the overlay**

```
go 1.25

use (
	.
	/home/shan/dev/herrscher-contracts
)
```

Write it at the host worktree root as `go.work`.

- [ ] **Step 2: Verify it is untracked**

Run: `git status --short go.work`
Expected: `?? go.work` (never commit it).

- [ ] **Step 3: Sanity build**

Run: `go build ./...`
Expected: builds (contracts resolves to local HEAD).

---

## Task 1: contracts.EnforceBudget helper — ALREADY SHIPPED (v0.2.9), SKIPPED

**ALREADY SHIPPED 2026-07-30:** `EnforceBudget` (with these exact semantics and
tests) is present in the released contracts v0.2.9. Verified in `v0.2.9:budget.go`.
No work needed; execution starts at Task 2. Original task text retained below for
the record only.

**Repo:** `/home/shan/dev/herrscher-contracts` (branch `feat/g6-user-budget` off `master`, currently at v0.2.9).

**Files:**
- Modify: `budget.go`
- Test: `budget_test.go`

**Interfaces:**
- Consumes: existing `BudgetError{Key string; Runes int; Limit int}`.
- Produces: `func EnforceBudget(key, body string, limit int) error` — returns `*BudgetError` when `body` exceeds `limit` runes; `limit <= 0` returns nil.

- [ ] **Step 1: Write the failing tests**

Add to `budget_test.go`:

```go
func TestEnforceBudgetOverReturnsBudgetError(t *testing.T) {
	// "é" is 2 bytes / 1 rune; 100 of them = 100 runes, 200 bytes.
	body := strings.Repeat("é", 100)
	err := EnforceBudget("user:alice", body, 50)
	var be *BudgetError
	if !errors.As(err, &be) {
		t.Fatalf("want *BudgetError, got %T (%v)", err, err)
	}
	if be.Runes != 100 || be.Limit != 50 || be.Key != "user:alice" {
		t.Fatalf("got Key=%q Runes=%d Limit=%d", be.Key, be.Runes, be.Limit)
	}
}

func TestEnforceBudgetUnderReturnsNil(t *testing.T) {
	if err := EnforceBudget("k", strings.Repeat("é", 40), 50); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestEnforceBudgetDisabledByNonPositiveLimit(t *testing.T) {
	for _, limit := range []int{0, -1} {
		if err := EnforceBudget("k", strings.Repeat("x", 999), limit); err != nil {
			t.Fatalf("limit %d should disable, got %v", limit, err)
		}
	}
}
```

Ensure the test file imports `errors` and `strings` (add if missing).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run TestEnforceBudget`
Expected: FAIL — `undefined: EnforceBudget`.

- [ ] **Step 3: Implement the helper**

In `budget.go`, change the import to include `unicode/utf8` (keep `fmt`) and add:

```go
// EnforceBudget returns a *BudgetError when body exceeds limit runes. A limit
// of zero or less disables the check and returns nil. key labels the rejected
// item in the returned error. Rune count, not byte length, is authoritative.
func EnforceBudget(key, body string, limit int) error {
	if limit <= 0 {
		return nil
	}
	if r := utf8.RuneCountInString(body); r > limit {
		return &BudgetError{Key: key, Runes: r, Limit: limit}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS, output pristine.

- [ ] **Step 5: Commit**

```bash
git add budget.go budget_test.go
git commit -m "feat(memory): add EnforceBudget helper for rune-budget policy (G6)"
```

---

## Task 2: USER.md home model + bounded SetUser

**Repo:** host worktree, branch `feat/g6-user-budget` off `master`.

**Files:**
- Modify: `core/internal/agent/agent.go` (const block)
- Modify: `core/internal/agent/store.go`
- Test: `core/internal/agent/store_test.go`

**Interfaces:**
- Consumes: `contracts.EnforceBudget` (Task 1).
- Produces: `Store.SetUser(name, text string) error`; `Store.SetUserBudget(runes int)`; `CreateSpec.User string`; const `userFile = "USER.md"`; const `defaultUserBudget = 1500`.

- [ ] **Step 1: Write the failing tests**

Add to `store_test.go`:

```go
func TestSetUserRejectsOverBudget(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "alice", Soul: "s"}); err != nil {
		t.Fatal(err)
	}
	s.SetUserBudget(50)
	err := s.SetUser("alice", strings.Repeat("é", 100)) // 100 runes / 200 bytes
	var be *contracts.BudgetError
	if !errors.As(err, &be) {
		t.Fatalf("want *contracts.BudgetError, got %T (%v)", err, err)
	}
	if be.Runes != 100 || be.Limit != 50 {
		t.Fatalf("got Runes=%d Limit=%d", be.Runes, be.Limit)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "alice", "USER.md")); !os.IsNotExist(err) {
		t.Fatal("USER.md must not be written when over budget")
	}
}

func TestSetUserWritesUnderBudget(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "alice", Soul: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUser("alice", "prefers Go"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(s.Root(), "alice", "USER.md"))
	if err != nil || string(got) != "prefers Go" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestSetUserZeroBudgetDisablesCheck(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "alice", Soul: "s"}); err != nil {
		t.Fatal(err)
	}
	s.SetUserBudget(0)
	if err := s.SetUser("alice", strings.Repeat("x", 5000)); err != nil {
		t.Fatalf("zero budget should disable, got %v", err)
	}
}

func TestSetUserMissingAgentErrors(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SetUser("ghost", "x"); err == nil {
		t.Fatal("want error for missing agent")
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "ghost")); !os.IsNotExist(err) {
		t.Fatal("SetUser must not create a home")
	}
}
```

Ensure `store_test.go` imports `errors`, `os`, `path/filepath`, `strings`, and `github.com/Herrscherd/herrscher-contracts` (add any missing).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/internal/agent/ -run TestSetUser`
Expected: FAIL — `s.SetUser undefined` / `s.SetUserBudget undefined`.

- [ ] **Step 3: Implement**

In `agent.go`, add to the file-name const block:

```go
	userFile     = "USER.md"
```

In `store.go`:

Add the import `github.com/Herrscherd/herrscher-contracts` (aliased `contracts` per existing convention in the repo).

Add the default constant near the top:

```go
// defaultUserBudget bounds USER.md in runes; SetUserBudget overrides it.
const defaultUserBudget = 1500
```

Change the `Store` type and constructor:

```go
// Store owns the directory holding every agent home: <root>/<name>/.
type Store struct {
	root       string
	userBudget int
}

// NewStore returns a Store rooted at root (created lazily on first Create).
func NewStore(root string) *Store { return &Store{root: root, userBudget: defaultUserBudget} }

// SetUserBudget overrides the per-agent USER.md rune budget. A value <= 0
// disables the check (see contracts.EnforceBudget).
func (s *Store) SetUserBudget(runes int) { s.userBudget = runes }
```

Add `User string` to `CreateSpec` (below `Soul`). In `Create`, after the
`settingsBuf` block and before assembling `files`, nothing changes; extend the
seed by appending USER.md when supplied — add this to the optional-files section
(alongside backend/cmd/tags):

```go
	if spec.User != "" {
		files = append(files, struct {
			name string
			data []byte
		}{userFile, []byte(spec.User)})
	}
```

Add `SetUser`, mirroring `SetSoul` but budget-checked before writing:

```go
// SetUser overwrites <home>/USER.md for an existing agent, rejecting content
// over the store's user budget (contracts.EnforceBudget) before touching disk.
// It never creates a home — an absent agent is an error.
func (s *Store) SetUser(name, text string) error {
	if !validateName(name) {
		return fmt.Errorf("invalid agent name %q", name)
	}
	home := filepath.Join(s.root, name)
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return fmt.Errorf("no agent %q", name)
	}
	if err := contracts.EnforceBudget(name, text, s.userBudget); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, userFile), []byte(text), 0o644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/internal/agent/`
Expected: PASS (new + existing), output pristine.

- [ ] **Step 5: Commit**

```bash
git add core/internal/agent/agent.go core/internal/agent/store.go core/internal/agent/store_test.go
git commit -m "feat(agent): bounded USER.md home file + SetUser (G6)"
```

---

## Task 3: Materialize USER.md into the worktree

**Repo:** host worktree, branch `feat/g6-user-budget`.

**Files:**
- Modify: `core/internal/agent/agent.go` (`Materialize`)
- Test: `core/internal/agent/agent_test.go`

**Interfaces:**
- Consumes: `userFile` const (Task 2), `worktreeToken`.
- Produces: `Materialize` writes `.claude/USER.md` and augments `CLAUDE.md`/`AGENTS.md` when a home `USER.md` exists.

- [ ] **Step 1: Write the failing tests**

Add to `agent_test.go` (follow the file's existing home-setup helper style; if none, create the home dir and write `SOUL.md`/`mcp.json`/`settings.json` inline as the existing tests do):

```go
func TestMaterializeWithUserProfile(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "SOUL.md"), []byte("# Soul"), 0o644)
	os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644)
	os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(home, "USER.md"), []byte("user works at {{WORKTREE}}"), 0o644)

	wt := t.TempDir()
	a := Agent{Name: "alice", Home: home}
	if err := a.Materialize(wt); err != nil {
		t.Fatal(err)
	}

	user, err := os.ReadFile(filepath.Join(wt, ".claude", "USER.md"))
	if err != nil {
		t.Fatalf("read .claude/USER.md: %v", err)
	}
	if string(user) != "user works at "+wt {
		t.Fatalf("worktree token not substituted: %q", user)
	}
	claude, _ := os.ReadFile(filepath.Join(wt, ".claude", "CLAUDE.md"))
	if !strings.Contains(string(claude), "# Soul") || !strings.Contains(string(claude), "@USER.md") {
		t.Fatalf("CLAUDE.md missing soul or import: %q", claude)
	}
	agents, _ := os.ReadFile(filepath.Join(wt, "AGENTS.md"))
	if !strings.Contains(string(agents), "# User") || !strings.Contains(string(agents), "user works at "+wt) {
		t.Fatalf("AGENTS.md missing inlined user: %q", agents)
	}
}

func TestMaterializeWithoutUserProfileUnchanged(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "SOUL.md"), []byte("# Soul"), 0o644)
	os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644)
	os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{}`), 0o644)

	wt := t.TempDir()
	a := Agent{Name: "alice", Home: home}
	if err := a.Materialize(wt); err != nil {
		t.Fatal(err)
	}
	claude, _ := os.ReadFile(filepath.Join(wt, ".claude", "CLAUDE.md"))
	agents, _ := os.ReadFile(filepath.Join(wt, "AGENTS.md"))
	if string(claude) != "# Soul" || string(agents) != "# Soul" {
		t.Fatalf("no-profile output must equal soul verbatim: claude=%q agents=%q", claude, agents)
	}
	if _, err := os.Stat(filepath.Join(wt, ".claude", "USER.md")); !os.IsNotExist(err) {
		t.Fatal(".claude/USER.md must not exist without a home USER.md")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/internal/agent/ -run TestMaterialize`
Expected: FAIL — `.claude/USER.md` absent / no import line.

- [ ] **Step 3: Implement — rewrite `Materialize`**

Replace the body of `Materialize` (from the `copies` block onward) so `CLAUDE.md`/`AGENTS.md` are composed explicitly and USER.md is handled. The `mcp.json`→`.mcp.json` and `settings.json`→`.claude/settings.json` copies and the codex `config.toml` render are unchanged. New body from after the `codexDir` MkdirAll:

```go
	// Files copied verbatim (token-substituted): mcp + settings.
	for _, c := range []struct{ src, dst string }{
		{filepath.Join(a.Home, mcpFile), filepath.Join(worktree, ".mcp.json")},
		{filepath.Join(a.Home, settingsFile), filepath.Join(claudeDir, "settings.json")},
	} {
		buf, err := os.ReadFile(c.src)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(c.src), err)
		}
		out := strings.ReplaceAll(string(buf), worktreeToken, worktree)
		if err := os.WriteFile(c.dst, []byte(out), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", c.dst, err)
		}
	}

	// Persona (SOUL.md) → CLAUDE.md + AGENTS.md, optionally augmented with the
	// user profile (USER.md). Absent USER.md leaves both byte-identical to soul.
	soul, err := os.ReadFile(filepath.Join(a.Home, soulFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", soulFile, err)
	}
	soulOut := strings.ReplaceAll(string(soul), worktreeToken, worktree)
	claudeMd, agentsMd := soulOut, soulOut

	userRaw, uErr := os.ReadFile(filepath.Join(a.Home, userFile))
	if uErr != nil && !os.IsNotExist(uErr) {
		return fmt.Errorf("read %s: %w", userFile, uErr)
	}
	if uErr == nil {
		userOut := strings.ReplaceAll(string(userRaw), worktreeToken, worktree)
		if err := os.WriteFile(filepath.Join(claudeDir, "USER.md"), []byte(userOut), 0o644); err != nil {
			return fmt.Errorf("write .claude/USER.md: %w", err)
		}
		claudeMd = soulOut + "\n\n@USER.md\n"
		agentsMd = soulOut + "\n\n# User\n\n" + userOut
	}

	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte(claudeMd), 0o644); err != nil {
		return fmt.Errorf("write .claude/CLAUDE.md: %w", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "AGENTS.md"), []byte(agentsMd), 0o644); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}

	mcp, err := os.ReadFile(filepath.Join(a.Home, mcpFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", mcpFile, err)
	}
	codexConfig, err := renderCodexMCP(mcp, worktree)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), codexConfig, 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
```

Update the `Materialize` doc comment to mention `<worktree>/.claude/USER.md (from <home>/USER.md, when present)` and that it is referenced from CLAUDE.md via `@import` and inlined into AGENTS.md.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/internal/agent/`
Expected: PASS (new + all existing Materialize tests), output pristine.

- [ ] **Step 5: Commit**

```bash
git add core/internal/agent/agent.go core/internal/agent/agent_test.go
git commit -m "feat(agent): materialize USER.md into worktree (CLAUDE import + AGENTS inline) (G6)"
```

---

## Task 4: Host config wiring — AGENT_USER_BUDGET

**Repo:** host worktree, branch `feat/g6-user-budget`.

**Files:**
- Modify: `core/internal/agent/store.go` (add env helper)
- Modify: `core/host/cli.go` (apply it after `agent.NewStore`, ~line 48)
- Test: `core/internal/agent/store_test.go`

**Interfaces:**
- Produces: `func UserBudgetFromEnv(get func(string) string) (int, bool)` — parses `AGENT_USER_BUDGET`; returns `(0, false)` when unset or unparseable.

- [ ] **Step 1: Write the failing test**

Add to `store_test.go`:

```go
func TestUserBudgetFromEnv(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want int
		ok   bool
	}{
		{"unset", "", 0, false},
		{"valid", "1200", 1200, true},
		{"zero", "0", 0, true},
		{"garbage", "abc", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			get := func(string) string { return c.val }
			got, ok := UserBudgetFromEnv(get)
			if got != c.want || ok != c.ok {
				t.Fatalf("got (%d,%v) want (%d,%v)", got, ok, c.want, c.ok)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/internal/agent/ -run TestUserBudgetFromEnv`
Expected: FAIL — `UserBudgetFromEnv undefined`.

- [ ] **Step 3: Implement**

In `store.go`, add `strconv` to the imports and add:

```go
// UserBudgetFromEnv parses AGENT_USER_BUDGET via get (os.Getenv in production).
// It returns (0, false) when the variable is unset or not an integer, so the
// caller keeps the default budget; a valid value (including 0, which disables
// the check) returns (n, true).
func UserBudgetFromEnv(get func(string) string) (int, bool) {
	v := get("AGENT_USER_BUDGET")
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}
```

In `core/host/cli.go`, right after `agents := agent.NewStore(filepath.Join(partDir, "agents"))` (line ~48):

```go
	if n, ok := agent.UserBudgetFromEnv(os.Getenv); ok {
		agents.SetUserBudget(n)
	}
```

Ensure `cli.go` imports `os` (add if missing — `agent` is already imported).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/internal/agent/ && go build ./...`
Expected: PASS + build success.

- [ ] **Step 5: Commit**

```bash
git add core/internal/agent/store.go core/host/cli.go core/internal/agent/store_test.go
git commit -m "feat(host): wire AGENT_USER_BUDGET into agent store (G6)"
```

---

## Release wiring (out of plan scope — main agent, after review)

**No contracts release for G6** — `EnforceBudget` already ships in v0.2.9. G6 is
host-only, so there is no cross-module tag/bump dance:

1. Verify the full host suite green: `GOWORK=off go build ./... && GOWORK=off go test ./...`.
2. Finish the branch via `superpowers:finishing-a-development-branch` (PR against master).

## Notes

- Deferred G1 Minor findings (obsidian negative-budget wording, budget_test substring) remain open; migrating obsidian to `EnforceBudget` is a separate future change.
