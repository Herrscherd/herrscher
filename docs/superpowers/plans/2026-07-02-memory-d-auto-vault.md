# Memory D — Auto-vault Provisioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** herrscher creates, scaffolds, and provisions its own Obsidian vault on first use — no manual `OBSIDIAN_VAULT`, the vault opens cleanly in the Obsidian app, and the `MemoryScope` roots the orchestrator uses exist from turn one.

**Architecture:** Four repos, edited in dependency order under the shared `/home/shan/dev/go.work` (local siblings auto-resolve, so every change is testable across repos before any release). `contracts` gains scope-key helpers (`ProjectKey`/`AgentKey`) as the single source of truth plus an optional `Provisioner` capability. `obsidian-memory` gains `EnsureVault` (create-or-open + `.obsidian/`), `EnsureAgent`/`EnsureProject` (idempotent root scaffolds implementing `Provisioner`), and a relaxed manifest that defaults the vault to `~/.herrscher/memory`. `orchestrator` adopts the key helpers. The host wires provisioning at **bridge startup** (the daemon never builds memory — the per-session bridge subprocess does, and it already receives `project`/`agent`), type-asserting `Provisioner` so `bridge.go` stays plugin-agnostic.

**Tech Stack:** Go, `os.Root`-sandboxed file I/O, self-registering plugins via `contracts.Register`, GitHub releases via `gh`/`git tag`.

**Spec:** `docs/superpowers/specs/2026-07-02-memory-d-auto-vault-design.md`

**Standing constraints (do not violate):**
- **Every release/tag/push is USER-GATED.** Do NOT `git push`, `git tag`, or create/publish releases without explicit user confirmation of the exact version. Tasks 1–7 are local, non-pushing edits + commits; Task 8 is the gated release sequence.
- The public host imports only PUBLIC Herrscherd modules (host CI has no auth). No new deps are introduced here, so this stays satisfied.
- No breaking change to `New` or the `Memory` port. `Provisioner` is a *separate* optional interface, type-asserted — never added to `Memory`.

---

## File Structure

**herrscher-contracts** (`/home/shan/dev/herrscher-contracts`)
- Modify `memory_scope.go` — add `ProjectKey`/`AgentKey` helpers (co-located with the scope policy they key).
- Modify `memory.go` — add the `Provisioner` optional interface (next to the `Memory` port).
- Create `scope_key_test.go` — assert the helper strings.

**herrscher-obsidian-memory** (`/home/shan/dev/herrscher-obsidian-memory`)
- Create `ensure_vault.go` — `EnsureVault` + `ensureObsidianDir` + `EnsureAgent`/`EnsureProject` + the `Provisioner` compile assertion (all new provisioning surface in one focused file).
- Create `ensure_vault_test.go` — vault/`.obsidian`/root-node tests.
- Modify `register.go` — relax manifest, factory defaults path + calls `EnsureVault`.

**herrscher-orchestrator** (`/home/shan/dev/herrscher-orchestrator`)
- Modify `register.go` — use `contracts.ProjectKey`/`contracts.AgentKey`.

**herrscher (host)** (`/home/shan/dev/herrscher`)
- Modify `bridge.go` — add `provisionScope`, call it after `buildMemory`.
- Create `bridge_provision_test.go` — assert provisioning through a fake `Provisioner`.

---

## Task 1: contracts — scope-key helpers + `Provisioner` interface

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/memory_scope.go`
- Modify: `/home/shan/dev/herrscher-contracts/memory.go`
- Test: `/home/shan/dev/herrscher-contracts/scope_key_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-contracts/scope_key_test.go`:

```go
package contracts

import "testing"

func TestScopeKeyHelpers(t *testing.T) {
	if got := ProjectKey("game"); got != "projects/game" {
		t.Fatalf("ProjectKey: got %q, want projects/game", got)
	}
	if got := AgentKey("scripter"); got != "agents/scripter" {
		t.Fatalf("AgentKey: got %q, want agents/scripter", got)
	}
}

// compile-time check that the interface shape is what callers depend on.
type stubProvisioner struct{}

func (stubProvisioner) EnsureProject(_ context.Context, _, _ string) error { return nil }
func (stubProvisioner) EnsureAgent(_ context.Context, _, _ string) error   { return nil }

var _ Provisioner = stubProvisioner{}
```

Add the import at the top of the test file (needed by the stub):

```go
import (
	"context"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestScopeKeyHelpers`
Expected: FAIL — `undefined: ProjectKey` / `undefined: Provisioner`.

- [ ] **Step 3: Add the key helpers**

In `/home/shan/dev/herrscher-contracts/memory_scope.go`, after the `RelContains`/`RelAppliesTo` const block (before `RecordShared`), add:

```go
// ProjectKey / AgentKey are the single source of truth for scope-root Keys, so
// the orchestrator (which derives a MemoryScope) and the provisioners (which
// create the root nodes) can never drift apart. Scheme: flat, English, no /index.
func ProjectKey(name string) string { return "projects/" + name }
func AgentKey(name string) string   { return "agents/" + name }
```

- [ ] **Step 4: Add the `Provisioner` interface**

In `/home/shan/dev/herrscher-contracts/memory.go`, after the `CurationHook` interface (end of file), add:

```go
// Provisioner is an optional Memory capability: ensuring the scope-root nodes a
// MemoryScope points at exist before any Record/Recall runs against them. It is
// deliberately NOT part of the Memory port — node-creating implementations (the
// obsidian vault) satisfy it, and callers type-assert, so a remote memory proxy
// that cannot create roots is simply skipped.
type Provisioner interface {
	// EnsureProject ensures the shared KindProject root at key exists (idempotent).
	EnsureProject(ctx context.Context, key, title string) error
	// EnsureAgent ensures the private KindAgent root at key exists (idempotent).
	EnsureAgent(ctx context.Context, key, title string) error
}
```

(`memory.go` already imports `context` — no import change.)

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: PASS (all existing tests + the new one).

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add memory_scope.go memory.go scope_key_test.go
git commit -m "feat(memory): ProjectKey/AgentKey scope-key helpers + optional Provisioner port"
```

---

## Task 2: obsidian — `EnsureVault` (create-or-open + `.obsidian/`)

**Files:**
- Create: `/home/shan/dev/herrscher-obsidian-memory/ensure_vault.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/ensure_vault_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-obsidian-memory/ensure_vault_test.go`:

```go
package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestEnsureVaultCreatesObsidianConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	m, err := EnsureVault(root)
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	defer m.Close()

	for _, name := range []string{".obsidian/app.json", ".obsidian/appearance.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	// vault is usable: a Record/Recall round-trips.
	if err := m.Record(context.Background(), contracts.Node{Key: "n", Kind: contracts.KindDecision, Title: "t"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	sg, err := m.Recall(context.Background(), "n", 0)
	if err != nil || sg.Root.Key != "n" {
		t.Fatalf("Recall: %+v err=%v", sg, err)
	}
}

func TestEnsureVaultNeverOverwritesExistingObsidianConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(root, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := []byte(`{"theme":"obsidian"}`)
	if err := os.WriteFile(filepath.Join(root, ".obsidian", "app.json"), custom, 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := EnsureVault(root)
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	defer m.Close()

	got, err := os.ReadFile(filepath.Join(root, ".obsidian", "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("app.json overwritten: got %q, want %q", got, custom)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestEnsureVault`
Expected: FAIL — `undefined: EnsureVault`.

- [ ] **Step 3: Write the implementation**

Create `/home/shan/dev/herrscher-obsidian-memory/ensure_vault.go`:

```go
package obsidian

import (
	"context"
	"fmt"

	"github.com/Herrscherd/herrscher-contracts"
)

// EnsureVault opens (creating if absent) the vault at root and additionally
// writes a minimal .obsidian/ app config when it is missing, so the Obsidian app
// opens the folder as a vault without prompting. It is the create-or-open
// superset of New: New stays open-only/strict (no .obsidian/ writes); EnsureVault
// is what the manifest/host use. Existing .obsidian/ files are never overwritten.
func EnsureVault(root string) (*ObsidianMemory, error) {
	m, err := New(root)
	if err != nil {
		return nil, err
	}
	if err := m.ensureObsidianDir(); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}

// ensureObsidianDir writes a minimal .obsidian/ config through the sandboxed root
// when absent. Idempotent: a file that already exists is left untouched. The
// files are non-markdown, so Search/Obsidian/git treat them as vault config, not
// memory nodes.
func (m *ObsidianMemory) ensureObsidianDir() error {
	if err := m.root.MkdirAll(".obsidian", 0o755); err != nil {
		return fmt.Errorf("obsidian: create .obsidian dir: %w", err)
	}
	for _, name := range []string{".obsidian/app.json", ".obsidian/appearance.json"} {
		if _, err := m.root.Stat(name); err == nil {
			continue // exists — never overwrite
		}
		if err := m.root.WriteFile(name, []byte("{}\n"), 0o644); err != nil {
			return fmt.Errorf("obsidian: write %s: %w", name, err)
		}
	}
	return nil
}

// EnsureAgent ensures the private KindAgent root node at key exists, creating it
// with title as its heading only when absent (idempotent — never overwrites an
// existing node). Callers pass contracts.AgentKey(name) so the node lands exactly
// where the orchestrator's scope will look.
func (m *ObsidianMemory) EnsureAgent(ctx context.Context, key, title string) error {
	return m.ensure(ctx, contracts.Node{Key: key, Kind: contracts.KindAgent, Title: title})
}

// EnsureProject ensures the shared KindProject root node at key exists, creating
// it only when absent (idempotent). Callers pass contracts.ProjectKey(name).
func (m *ObsidianMemory) EnsureProject(ctx context.Context, key, title string) error {
	return m.ensure(ctx, contracts.Node{Key: key, Kind: contracts.KindProject, Title: title})
}

// Compile-time proof the vault satisfies the optional provisioning capability the
// host type-asserts at bridge startup.
var _ contracts.Provisioner = (*ObsidianMemory)(nil)
```

Note: `EnsureAgent`/`EnsureProject` reuse the existing `ensure` method (`scaffold.go:122`) — flock + `Stat` + `recordUnlocked`, create-only. They are placed here (not in `scaffold.go`) because they are the runtime provisioning surface, distinct from the human-facing `Init` scaffold.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestEnsureVault`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add ensure_vault.go ensure_vault_test.go
git commit -m "feat(vault): EnsureVault create-or-open with .obsidian init"
```

---

## Task 3: obsidian — `EnsureAgent`/`EnsureProject` idempotency tests

**Files:**
- Test: `/home/shan/dev/herrscher-obsidian-memory/ensure_vault_test.go` (append)

(The methods were written in Task 2 alongside `EnsureVault`; this task locks their idempotency + `Provisioner` behavior with dedicated tests.)

- [ ] **Step 1: Write the failing test**

Append to `/home/shan/dev/herrscher-obsidian-memory/ensure_vault_test.go`:

```go
func TestEnsureAgentAndProjectAreIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	m, err := EnsureVault(root)
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	defer m.Close()
	ctx := context.Background()

	// Use the interface, exactly as the host will (type-asserted Provisioner).
	var p contracts.Provisioner = m

	if err := p.EnsureProject(ctx, contracts.ProjectKey("game"), "game"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := p.EnsureAgent(ctx, contracts.AgentKey("scripter"), "scripter"); err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}

	// Mutate the project node, then re-ensure: an existing node is never clobbered.
	proj, err := m.load(contracts.ProjectKey("game"))
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	proj.Body = "hand-edited"
	if err := m.Record(ctx, proj); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := p.EnsureProject(ctx, contracts.ProjectKey("game"), "game"); err != nil {
		t.Fatalf("EnsureProject (2nd): %v", err)
	}
	got, err := m.load(contracts.ProjectKey("game"))
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if got.Body != "hand-edited" {
		t.Fatalf("EnsureProject clobbered an existing node: body=%q", got.Body)
	}
	if got.Kind != contracts.KindProject {
		t.Fatalf("project kind: got %q, want %q", got.Kind, contracts.KindProject)
	}

	// The provisioned roots make a scoped round-trip work with no missing-root error.
	s := contracts.MemoryScope{Project: contracts.ProjectKey("game"), Agent: contracts.AgentKey("scripter")}
	if err := contracts.RecordShared(ctx, m, s, contracts.Node{Key: "facts/eco", Kind: contracts.KindDecision, Title: "eco"}); err != nil {
		t.Fatalf("RecordShared: %v", err)
	}
	if err := contracts.RecordPrivate(ctx, m, s, contracts.Node{Key: "skills/ds", Kind: contracts.KindDecision, Title: "ds"}); err != nil {
		t.Fatalf("RecordPrivate: %v", err)
	}
	if _, err := contracts.RecallScoped(ctx, m, s, 1); err != nil {
		t.Fatalf("RecallScoped: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails, then passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestEnsureAgentAndProjectAreIdempotent`
Expected: PASS (implementation already exists from Task 2). If it FAILS, the failure identifies a real bug in the Task 2 methods — fix it in `ensure_vault.go`, do not weaken the test.

- [ ] **Step 3: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add ensure_vault_test.go
git commit -m "test(vault): EnsureAgent/EnsureProject idempotency + scoped round-trip"
```

---

## Task 4: obsidian — relax manifest, factory defaults path + `EnsureVault`

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/register.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/register_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-obsidian-memory/register_test.go`:

```go
package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

// findPlugin returns the registered obsidian memory plugin.
func findPlugin(t *testing.T) contracts.Plugin {
	t.Helper()
	for _, p := range contracts.Default.Memories() {
		if p.Manifest.Kind == "obsidian" {
			return p
		}
	}
	t.Fatal("obsidian memory plugin not registered")
	return contracts.Plugin{}
}

func TestManifestVaultIsOptional(t *testing.T) {
	p := findPlugin(t)
	for _, s := range p.Manifest.Config {
		if s.Key == "vault" && s.Required {
			t.Fatal("vault setting must be optional (Required=false)")
		}
	}
	// Resolve with no OBSIDIAN_VAULT set must succeed (no missing-required error).
	if _, err := contracts.Resolve(p.Manifest.Config, func(string) string { return "" }); err != nil {
		t.Fatalf("Resolve with empty env: %v", err)
	}
}

func TestFactoryUsesExplicitVaultAndInitsObsidian(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	p := findPlugin(t)
	mem, err := p.Memory(context.Background(), contracts.PluginConfig{Settings: map[string]string{"vault": root}})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer mem.Close()
	if _, err := os.Stat(filepath.Join(root, ".obsidian", "app.json")); err != nil {
		t.Fatalf("factory did not init .obsidian: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestManifestVaultIsOptional|TestFactoryUsesExplicitVaultAndInitsObsidian'`
Expected: FAIL — `TestManifestVaultIsOptional` fails (`vault` is still `Required: true`), and the factory still calls `New` (no `.obsidian`).

- [ ] **Step 3: Rewrite `register.go`**

Replace the entire contents of `/home/shan/dev/herrscher-obsidian-memory/register.go` with:

```go
package obsidian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher-contracts"
)

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "obsidian",
			Category: contracts.CategoryMemory,
			Config: []contracts.Setting{
				{Key: "vault", Env: "OBSIDIAN_VAULT", Help: "path to the memory vault directory (default ~/.herrscher/memory)", Required: false},
			},
		},
		Memory: func(ctx context.Context, cfg contracts.PluginConfig) (contracts.Memory, error) {
			root := cfg.Get("vault")
			if root == "" {
				// Default to the shared vault under ~/.herrscher, which survives
				// worktree teardown. Resolved here (not as a static manifest Default)
				// because a manifest string cannot expand ~/$HOME.
				home, err := os.UserHomeDir()
				if err != nil {
					return nil, fmt.Errorf("obsidian: default vault path: %w", err)
				}
				root = filepath.Join(home, ".herrscher", "memory")
			}
			// EnsureVault (not New): provision a missing directory + .obsidian config
			// so the vault opens as an Obsidian vault with no manual setup.
			return EnsureVault(root)
		},
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./...`
Expected: PASS (all tests, including the full existing suite).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add register.go register_test.go
git commit -m "feat(vault): default OBSIDIAN_VAULT to ~/.herrscher/memory, factory EnsureVault"
```

---

## Task 5: orchestrator — adopt `ProjectKey`/`AgentKey`

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/register.go:20-25`

The existing tests (`learner_test.go`, `orchestrator_test.go`) already assert keys `projects/game` / `agents/scripter`, so they are the regression guard — the helpers must produce byte-identical keys, and this change must keep them green.

- [ ] **Step 1: Confirm the guard passes before the change**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./...`
Expected: PASS (baseline).

- [ ] **Step 2: Edit `register.go`**

In `/home/shan/dev/herrscher-orchestrator/register.go`, change the scope derivation (lines ~20-25):

```go
			if p := cfg.Get("memory.project"); p != "" {
				scope.Project = contracts.ProjectKey(p)
			}
			if a := cfg.Get("memory.agent"); a != "" {
				scope.Agent = contracts.AgentKey(a)
			}
```

(Replaces the inline `"projects/" + p` / `"agents/" + a` literals. Update the adjacent comment's "we key them onto the shared spine here: projects/<name>, agents/<name>" to note the helpers now own the scheme: `// … via contracts.ProjectKey/AgentKey (single source of truth).`)

- [ ] **Step 3: Run tests to verify they still pass**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./...`
Expected: PASS — keys unchanged (`projects/game`, `agents/scripter`), so `learner_test.go` / `orchestrator_test.go` stay green. The workspace resolves `contracts.ProjectKey`/`AgentKey` locally even though `go.mod` still pins the older contracts (the pin is bumped at release, Task 8).

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
git add register.go
git commit -m "refactor(scope): derive scope roots via contracts.ProjectKey/AgentKey"
```

---

## Task 6: host — provision scope roots at bridge startup

**Files:**
- Modify: `/home/shan/dev/herrscher/bridge.go` (the `if mem != nil` block near line 66, and a new `provisionScope` helper)
- Test: `/home/shan/dev/herrscher/bridge_provision_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher/bridge_provision_test.go`:

```go
package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

// recordingMem is a fake Memory that also implements contracts.Provisioner,
// capturing the ensured roots.
type recordingMem struct {
	projects [][2]string // {key, title}
	agents   [][2]string
}

func (m *recordingMem) Recall(context.Context, string, int) (contracts.Subgraph, error) {
	return contracts.Subgraph{}, nil
}
func (m *recordingMem) Record(context.Context, contracts.Node) error         { return nil }
func (m *recordingMem) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return nil, nil
}
func (m *recordingMem) Links(context.Context, string, string, string) error { return nil }
func (m *recordingMem) Close() error                                        { return nil }
func (m *recordingMem) EnsureProject(_ context.Context, key, title string) error {
	m.projects = append(m.projects, [2]string{key, title})
	return nil
}
func (m *recordingMem) EnsureAgent(_ context.Context, key, title string) error {
	m.agents = append(m.agents, [2]string{key, title})
	return nil
}

// plainMem implements only Memory (no Provisioner) — provisionScope must skip it.
type plainMem struct{ recordingMem }

func TestProvisionScopeEnsuresBothRoots(t *testing.T) {
	m := &recordingMem{}
	provisionScope(context.Background(), m, "game", "scripter", slog.Default())

	if len(m.projects) != 1 || m.projects[0] != [2]string{"projects/game", "game"} {
		t.Fatalf("project root not ensured: %+v", m.projects)
	}
	if len(m.agents) != 1 || m.agents[0] != [2]string{"agents/scripter", "scripter"} {
		t.Fatalf("agent root not ensured: %+v", m.agents)
	}
}

func TestProvisionScopeSkipsEmptyNames(t *testing.T) {
	m := &recordingMem{}
	provisionScope(context.Background(), m, "", "", slog.Default())
	if len(m.projects) != 0 || len(m.agents) != 0 {
		t.Fatalf("empty names must ensure nothing: %+v %+v", m.projects, m.agents)
	}
}

func TestProvisionScopeIgnoresNonProvisioner(t *testing.T) {
	// A Memory that is not a Provisioner must be handled without panicking.
	var mem contracts.Memory = &struct{ contracts.Memory }{}
	provisionScope(context.Background(), mem, "game", "scripter", slog.Default())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test . -run TestProvisionScope`
Expected: FAIL — `undefined: provisionScope`.

- [ ] **Step 3: Add `provisionScope` and call it**

In `/home/shan/dev/herrscher/bridge.go`, change the memory block (currently at lines 66-69):

```go
	mem := buildMemory(ctx, log)
	if mem != nil {
		defer mem.Close()
		provisionScope(ctx, mem, *project, *agent, log)
	}
```

Then add the helper immediately after `buildMemory` (after line 99):

```go
// provisionScope ensures this session's memory scope roots exist before the first
// turn, so B can record and A can recall from turn one. It is plugin-agnostic and
// best-effort: memory implementations that can create nodes satisfy
// contracts.Provisioner (the local obsidian vault does; a remote proxy may not
// and is skipped). It keys the roots with the same contracts helpers the
// orchestrator derives its scope from, so the keys cannot drift. Errors are
// logged, never fatal — memory stays optional, matching buildMemory.
func provisionScope(ctx context.Context, mem contracts.Memory, project, agent string, log *slog.Logger) {
	p, ok := mem.(contracts.Provisioner)
	if !ok {
		return
	}
	if project != "" {
		if err := p.EnsureProject(ctx, contracts.ProjectKey(project), project); err != nil {
			log.Debug("ensure project root", "project", project, "err", err)
		}
	}
	if agent != "" {
		if err := p.EnsureAgent(ctx, contracts.AgentKey(agent), agent); err != nil {
			log.Debug("ensure agent root", "agent", agent, "err", err)
		}
	}
}
```

(`bridge.go` already imports `context`, `log/slog`, `os`, and `contracts` — no import change. `*project`/`*agent` are the existing bridge flags threaded into `buildOrchestrator` on the next line.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test . -run TestProvisionScope`
Expected: PASS.

- [ ] **Step 5: Run the full host build + vet + tests**

Run: `cd /home/shan/dev/herrscher && gofmt -l . && go vet ./... && go build ./... && go test ./...`
Expected: no gofmt output, vet clean, build ok, tests PASS. (Workspace resolves the new `contracts.Provisioner`/`ProjectKey` locally; the `go.mod` pin is bumped at release, Task 8.)

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher
git add bridge.go bridge_provision_test.go
git commit -m "feat(bridge): provision memory scope roots at session startup"
```

---

## Task 7: Cross-repo integration check (workspace, pre-release)

**Files:** none (verification only)

- [ ] **Step 1: Build + test every touched repo through the workspace**

Run:
```bash
cd /home/shan/dev/herrscher-contracts && go test ./... && \
cd /home/shan/dev/herrscher-obsidian-memory && go test ./... && \
cd /home/shan/dev/herrscher-orchestrator && go test ./... && \
cd /home/shan/dev/herrscher && go vet ./... && go build ./... && go test ./...
```
Expected: all PASS. This proves the four repos compose correctly against the local (un-released) versions before any tag is cut.

- [ ] **Step 2: Sanity-check the vault opens end-to-end (manual smoke)**

Run:
```bash
cd /home/shan/dev/herrscher && rm -rf /tmp/herrscher-vault-smoke && \
OBSIDIAN_VAULT=/tmp/herrscher-vault-smoke go test ./... -run TestProvisionScope -count=1 && \
ls -la /tmp/herrscher-vault-smoke 2>/dev/null || echo "(no vault written by unit test — expected; real vault is written at bridge runtime)"
```
Expected: tests PASS. (The unit tests use fakes/temp dirs; this step only confirms nothing panics with an env var set. Real vault creation is exercised by the obsidian `EnsureVault` tests in Task 2/4.)

No commit — verification only.

---

## Task 8: Releases (USER-GATED — do NOT run without explicit per-version confirmation)

> **STOP.** Every step below pushes or tags a public repo. Do **not** execute any of it until the user has confirmed the exact version numbers. Present the proposed versions, wait for explicit approval, then proceed one repo at a time in dependency order. If the user declines a push, keep the work local and report it.

**Proposed versions (confirm with user):**
- `herrscher-contracts`: `v0.1.10` → **`v0.1.11`** (adds `ProjectKey`/`AgentKey`/`Provisioner`; additive).
- `herrscher-obsidian-memory`: current → **next patch** (adds `EnsureVault`/`EnsureAgent`/`EnsureProject` + manifest relaxation; requires contracts `v0.1.11`).
- `herrscher-orchestrator`: current → **next patch** (adopts key helpers; requires contracts `v0.1.11`).
- host `herrscher`: bump all three pins + ship the bridge wiring (PR to `master`).

Confirm the obsidian/orchestrator current tags first: `git -C <repo> tag | sort -V | tail -3`.

- [ ] **Step 1: Release contracts (after user confirms version)**

```bash
cd /home/shan/dev/herrscher-contracts
go mod tidy && git diff --exit-code go.mod go.sum
git push origin master
git tag v0.1.11 && git push origin v0.1.11
```

- [ ] **Step 2: Bump + release obsidian (after user confirms version)**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
go get github.com/Herrscherd/herrscher-contracts@v0.1.11
go mod tidy && go test ./... && git diff --exit-code go.mod go.sum || true
git add go.mod go.sum && git commit -m "build: require herrscher-contracts v0.1.11"
git push origin master
git tag <vX.Y.Z> && git push origin <vX.Y.Z>
```

- [ ] **Step 3: Bump + release orchestrator (after user confirms version)**

```bash
cd /home/shan/dev/herrscher-orchestrator
go get github.com/Herrscherd/herrscher-contracts@v0.1.11
go mod tidy && go test ./...
git add go.mod go.sum && git commit -m "build: require herrscher-contracts v0.1.11"
git push origin master
git tag <vX.Y.Z> && git push origin <vX.Y.Z>
```

- [ ] **Step 4: Bump host pins + open PR (after user confirms)**

```bash
cd /home/shan/dev/herrscher
git checkout -b feat/memory-d-auto-vault
go get github.com/Herrscherd/herrscher-contracts@v0.1.11
go get github.com/Herrscherd/herrscher-obsidian-memory@<vX.Y.Z>
go get github.com/Herrscherd/herrscher-orchestrator@<vX.Y.Z>
go mod tidy && git diff --exit-code go.mod go.sum || true
gofmt -l . && go vet ./... && go build ./... && go test -race ./...
git add -A && git commit -m "feat(memory): auto-provision Obsidian vault + scope roots (chantier D)"
git push -u origin feat/memory-d-auto-vault
gh pr create --fill --base master
```

- [ ] **Step 5: Verify host CI green, then merge (user-gated)**

Run: `gh pr checks --watch` then merge once green. Confirm master CI stays green (host CI has no auth — all three deps are public, so this is satisfied; see the private-deps trap in memory `host-deps-must-be-public`).

---

## Self-Review

**Spec coverage:**
- Scope-key helpers (spec §1) → Task 1. ✅
- `EnsureVault` + `.obsidian/` (spec §2) → Task 2, Task 4 (factory). ✅
- `EnsureAgent`/`EnsureProject` + `Provisioner` (spec §3) → Task 1 (interface), Task 2 (impl), Task 3 (idempotency). ✅
- Manifest relaxation + factory default (spec §4) → Task 4. ✅
- Core wiring at bridge startup (spec §5) → Task 6. ✅
- Orchestrator adopts helpers (spec §1) → Task 5. ✅
- `scaffold.Init`: explicitly out of scope (spec Non-goals) → no task, by design. ✅
- Testing bullets (spec Testing) → covered across Tasks 2/3/4/6. ✅
- Release order contracts→obsidian→orchestrator→host, user-gated (spec Version bumps) → Task 8. ✅

**Placeholder scan:** Release version tags in Task 8 are intentionally `<vX.Y.Z>` — these are USER-GATED and confirmed at release time, not plan-time guesses; contracts `v0.1.11` is proposed concretely. No other placeholders.

**Type consistency:** `Provisioner{EnsureProject, EnsureAgent}` signatures `(ctx, key, title string) error` are identical in contracts (Task 1), obsidian (Task 2), the recordingMem fake (Task 6), and `provisionScope` call sites. Keys are always `contracts.ProjectKey(name)`/`contracts.AgentKey(name)` — never inline literals — in orchestrator (Task 5) and bridge (Task 6), so scope-derivation and provisioning cannot drift.
