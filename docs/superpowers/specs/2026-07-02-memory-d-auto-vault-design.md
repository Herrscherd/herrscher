# Memory D — Auto-vault provisioning

**Date:** 2026-07-02
**Status:** Design (approved for spec; decisions locked 2026-07-02)
**PR order:** 4 of 4 (C → B → A → D)
**Repos touched:** `herrscher-contracts`, `herrscher-obsidian-memory`, `herrscher-orchestrator`, `core` (host)

## Context

herrscher is supposed to manage its own memory with no manual setup, but two gaps
break that today:

1. **The vault does not open as an Obsidian vault.** `New(root)` already
   `MkdirAll`s the directory (so the folder is auto-created), but it never writes
   an `.obsidian/` app config, and the manifest still marks `vault` /
   `OBSIDIAN_VAULT` as **Required** (`register.go`). So a human must both point at
   a path and hand-initialize it before the Obsidian app will open it.

2. **The scope roots never exist at runtime.** The orchestrator derives a
   `MemoryScope` per turn keyed `projects/<name>` + `agents/<name>`
   (`herrscher-orchestrator/register.go`), but:
   - `scaffold.Init` writes the project node at `projets/<name>/index` (French
     prefix, `/index` suffix) — a **different key** — and it is **never called at
     runtime** (zero non-test call sites).
   - **Nothing** ever creates a `KindAgent` node.
   So `RecallScoped`/`RecordShared`/`RecordPrivate` look up roots that don't
   exist; `Links`/`Recall` fail on the missing file and the orchestrator silently
   swallows the error (`orchestrator.go` `Curator.Context`). Auto-capture (B) and
   ranked recall (A) are therefore dead on arrival until the roots are provisioned
   *and* keyed consistently.

The durable-Agent model keeps an agent's home outside worktrees (worktrees are
deleted on `session close`), so memory must live under `~/.herrscher/`, not in a
worktree.

## Goal

herrscher **creates, scaffolds, and provisions** its own Obsidian vault on first
use — no manual `OBSIDIAN_VAULT`, the vault opens cleanly in the Obsidian app, and
the `MemoryScope` roots the orchestrator uses exist from turn one.

## Decisions locked

- **Single shared vault** at `~/.herrscher/memory` — one graph for all agents,
  with shared `KindProject` subtrees and private `KindAgent` subtrees per the
  existing `MemoryScope`. No vault-per-agent (do not fragment the graph). The
  shared path lives under `~/.herrscher`, so it survives worktree teardown.
- **One source of truth for scope keys** — new `contracts.ProjectKey(name)` and
  `contracts.AgentKey(name)` helpers, used by both the orchestrator (scope
  derivation) and the provisioners (root creation). Standard scheme:
  `projects/<name>` and `agents/<name>` (English, flat, no `/index`). This
  permanently eliminates the projects/projets drift.
- **End-to-end this PR** — wire provisioning into the real startup path
  (`agent create` + session create), not just ship dormant plugin helpers.

## Non-goals

- No vault-per-agent physical split (see Decisions).
- No Obsidian plugin/theme install — just a minimal `.obsidian/` so the app opens
  the folder as a vault without prompting.
- No change to `scaffold.Init` at all. It is runtime-dead (zero non-test call
  sites) and its keys are deliberately org-aware (`<org>/<project>/index`), which
  `contracts.ProjectKey` (org-blind `projects/<name>`) would contradict. Runtime
  correctness comes from the orchestrator + bridge provisioner sharing the key
  helpers, not from `Init`; reconciling `Init` would break its org hierarchy for
  no runtime gain.

## Design

### 1. Scope-key helpers (contracts)

```go
// ProjectKey / AgentKey are the single source of truth for scope-root Keys, so
// the orchestrator (which derives a MemoryScope) and the provisioners (which
// create the root nodes) can never drift apart.
func ProjectKey(name string) string { return "projects/" + name }
func AgentKey(name string) string   { return "agents/" + name }
```

The orchestrator's `register.go` replaces its inline `"projects/"+name` /
`"agents/"+name` literals with these calls. `scaffold.Init` is **not** touched
(see Non-goals): its org-aware keys are intentional and it has no runtime call
site, so the drift that matters — the one the live orchestrator and provisioner
share — is eliminated entirely by both using these helpers.

### 2. `EnsureVault` (obsidian-memory)

```go
// EnsureVault opens the vault at root (creating the directory if absent, as New
// already does) and additionally writes a minimal .obsidian/ app config when it
// is missing, so the Obsidian app opens the folder as a vault. Existing configs
// are left untouched. Returns an opened *ObsidianMemory.
func EnsureVault(root string) (*ObsidianMemory, error)
```

- Reuses `New`'s open path (MkdirAll + `os.Root` + flock) verbatim.
- After opening, if `.obsidian/app.json` is absent, write a minimal `app.json`
  (+ empty `appearance.json`) through the sandboxed `os.Root`. Idempotent: never
  overwrites existing `.obsidian/` files.
- `New` stays open-only/strict (no `.obsidian/` writes); `EnsureVault` is the
  create-or-open superset the manifest/host uses.

### 3. Root-node provisioners (obsidian-memory + contracts)

Two tiny idempotent scaffolds, modeled on `scaffold.Init`'s non-overwriting
`ensure` (create only when absent, never clobber):

```go
// EnsureAgent ensures the private KindAgent root node exists at key (idempotent).
func (m *ObsidianMemory) EnsureAgent(ctx context.Context, key, title string) error

// EnsureProject ensures the shared KindProject root node exists at key (idempotent).
func (m *ObsidianMemory) EnsureProject(ctx context.Context, key, title string) error
```

These satisfy a new **optional** capability interface in contracts, so the host
can provision through the `contracts.Memory` port without importing the obsidian
package concretely (a remote memory proxy may legitimately not implement it):

```go
// Provisioner is an optional Memory capability: ensuring the scope-root nodes a
// MemoryScope points at exist before any Record/Recall runs against them.
// Node-creating implementations satisfy it; callers type-assert.
type Provisioner interface {
	EnsureProject(ctx context.Context, key, title string) error
	EnsureAgent(ctx context.Context, key, title string) error
}
```

Callers pass `contracts.AgentKey(name)` / `contracts.ProjectKey(name)`, so the
nodes land exactly where the orchestrator's scope will look. This makes
`MemoryScope{Project, Agent}` valid immediately — B can record and A can recall
from turn one.

### 4. Manifest / config change (obsidian-memory)

Relax `register.go`: `vault` becomes **not Required**. When `OBSIDIAN_VAULT` is
unset the plugin factory defaults the root to the shared `~/.herrscher/memory`
(expanded from `$HOME` in the factory, since a static manifest `Default` cannot
expand `~`). The factory calls `EnsureVault` instead of `New`, so a missing
directory/config is provisioned rather than erroring, and the vault opens as an
Obsidian vault. Keeping the default in the factory means the host needs **no**
path change — `buildMemory` still passes `os.Getenv` unchanged.

### 5. Core wiring (herrscher)

Provisioning is wired at **bridge startup**, not in the daemon. The daemon never
builds a `Memory`; the per-session bridge subprocess does (`bridge.go:66`,
`buildMemory` → `resolver.Memory`), and it already receives the session's
`project`/`agent` as flags (threaded on to `buildOrchestrator`). So one seam in
the bridge covers both roots, keeps `core/internal/*` free of any memory
construction, and guarantees the roots exist before the orchestrator's first
`RecallScoped`/`RecordShared`.

- **Memory construction is unchanged.** Because the obsidian factory now calls
  `EnsureVault` and defaults the path, `buildMemory` provisions the shared vault
  idempotently with no edit to `bridge.go`'s `buildMemory`.
- **Scope-root provisioning** (`bridge.go` `Run`, immediately after
  `mem := buildMemory(...)`): if `mem` is non-nil and implements
  `contracts.Provisioner`, call `EnsureProject(contracts.ProjectKey(project),
  project)` when `project != ""` and `EnsureAgent(contracts.AgentKey(agent),
  agent)` when `agent != ""`. Type-assertion keeps `bridge.go` plugin-agnostic
  (a remote memory proxy that lacks the capability is simply skipped); errors are
  logged best-effort and never block the bridge, matching `buildMemory`'s
  optional-memory contract.
- The orchestrator (read/write side) is unchanged beyond adopting the key helpers;
  its `Curator.Context` / `Learner` now find the roots they always assumed — and
  because the bridge provisions with the *same* `project`/`agent` flags the
  orchestrator derives its scope from, the keys cannot drift.

## Interfaces changed

| Symbol | Repo | Change |
|--------|------|--------|
| `ProjectKey`, `AgentKey` | contracts | new scope-key helpers (single source of truth) |
| `Provisioner` | contracts | new optional Memory capability (`EnsureProject`/`EnsureAgent`) |
| scope derivation | orchestrator | uses `ProjectKey`/`AgentKey` instead of inline literals |
| `EnsureVault` | obsidian-memory | new create-or-open constructor (+ `.obsidian/` config) |
| `EnsureAgent` | obsidian-memory | new idempotent Agent-root scaffold (implements `Provisioner`) |
| `EnsureProject` | obsidian-memory | new idempotent Project-root scaffold (implements `Provisioner`) |
| manifest `vault` | obsidian-memory | Required → optional; factory defaults `~/.herrscher/memory` + calls `EnsureVault` |
| bridge scope provisioning | core (host) | after `buildMemory`, type-asserts `Provisioner` and ensures the session's Project + Agent roots |

No breaking change to `New` or the `Memory` port; `Provisioner` is a separate
optional interface (type-asserted, never added to `Memory`), and the manifest
relaxation only widens what configs are accepted.

## Version bumps (release order, user-gated)

contracts (adds key helpers) → obsidian-memory (EnsureVault/EnsureAgent/
EnsureProject + manifest, requires new contracts) → orchestrator (adopts key
helpers, requires new contracts) → host (bump all, wire agent create + session
create). Each is additive/patch; concrete versions confirmed at release time.

## Testing

- `EnsureVault` on a non-existent path creates the dir + `.obsidian/app.json`,
  returns a working memory; `Record`/`Recall` roundtrips.
- `EnsureVault` on an existing vault leaves its `.obsidian/` untouched.
- `EnsureAgent`/`EnsureProject` twice each create their node once (idempotent),
  never clobbering an existing node's body.
- `ProjectKey`/`AgentKey` are stable and match what `EnsureProject`/`EnsureAgent`
  write and what the orchestrator derives (a single round-trip test asserting the
  orchestrator's scope roots resolve to nodes that the provisioners created).
- After `EnsureVault` + `EnsureProject` + `EnsureAgent`, a
  `MemoryScope{Project, Agent}` round-trips through
  `RecordShared`/`RecordPrivate`/`RecallScoped` with no missing-root error.
- Manifest resolves with no `OBSIDIAN_VAULT` set (factory default applied); the
  factory returns a working, `.obsidian/`-initialised vault at `~/.herrscher/memory`.
- Bridge: a `*ObsidianMemory` satisfies `contracts.Provisioner` (compile-time
  assertion + a round-trip test that `EnsureProject`/`EnsureAgent` through the
  interface leave `projects/<name>`/`agents/<name>` nodes the orchestrator's scope
  then resolves).
