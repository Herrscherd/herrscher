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
- No rework of `scaffold.Init`'s wider spine (Organization/Domain/Repo/Server) —
  D only reconciles its **Project-node key** to `contracts.ProjectKey`. The rest
  of `Init` is out of scope and stays as-is.

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
`"agents/"+name` literals with these calls. `scaffold.Init` uses `ProjectKey` for
the project node it writes (dropping the `projets/.../index` form for that node).

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

### 3. Root-node provisioners (obsidian-memory)

Two tiny idempotent scaffolds, modeled on `scaffold.Init`'s non-overwriting
`ensure` (create only when absent, never clobber):

```go
// EnsureAgent ensures the private KindAgent root node exists at key (idempotent).
func (m *ObsidianMemory) EnsureAgent(ctx context.Context, key, title string) error

// EnsureProject ensures the shared KindProject root node exists at key (idempotent).
func (m *ObsidianMemory) EnsureProject(ctx context.Context, key, title string) error
```

Callers pass `contracts.AgentKey(name)` / `contracts.ProjectKey(name)`, so the
nodes land exactly where the orchestrator's scope will look. This makes
`MemoryScope{Project, Agent}` valid immediately — B can record and A can recall
from turn one.

### 4. Manifest / config change (obsidian-memory)

Relax `register.go`: `vault` becomes **not Required**, defaulting to the shared
`~/.herrscher/memory` (host-resolved). The plugin factory calls `EnsureVault`
instead of `New`, so a missing directory/config is provisioned rather than
erroring, and the vault opens as an Obsidian vault.

### 5. Core wiring (herrscher)

- **Memory construction** (`bridge.go` `buildMemory` → `resolver.Memory`):
  default the vault path to `~/.herrscher/memory` when `OBSIDIAN_VAULT` is unset.
  Since the plugin factory now calls `EnsureVault`, constructing memory
  provisions the shared vault idempotently.
- **`agent create <name>`** (`core/internal/agent/store.go` `Create`, alongside
  the existing SOUL.md/mcp.json seeding): open the shared vault and call
  `EnsureAgent(contracts.AgentKey(name), <name>)` so the agent root exists before
  any session.
- **session create** (`core/internal/manager/session.go`, where `project`/`agent`
  are derived and threaded to the bridge): call
  `EnsureProject(contracts.ProjectKey(project), project)` so the shared project
  root exists for that session's scope.
- The orchestrator (read/write side) is unchanged beyond adopting the key helpers;
  its `Curator.Context` / `Learner` now find the roots they always assumed.

## Interfaces changed

| Symbol | Repo | Change |
|--------|------|--------|
| `ProjectKey`, `AgentKey` | contracts | new scope-key helpers (single source of truth) |
| scope derivation | orchestrator | uses `ProjectKey`/`AgentKey` instead of inline literals |
| `EnsureVault` | obsidian-memory | new create-or-open constructor (+ `.obsidian/` config) |
| `EnsureAgent` | obsidian-memory | new idempotent Agent-root scaffold |
| `EnsureProject` | obsidian-memory | new idempotent Project-root scaffold |
| `scaffold.Init` project key | obsidian-memory | uses `ProjectKey` (drops `projets/.../index` for that node) |
| manifest `vault` | obsidian-memory | Required → optional + host default `~/.herrscher/memory` |
| plugin factory | obsidian-memory | calls `EnsureVault` not `New` |
| `agent create` | core | ensures shared vault + Agent root under `~/.herrscher/memory` |
| session create | core | ensures Project root for the session scope |

No breaking change to `New` or the `Memory` port; the manifest relaxation only
widens what configs are accepted.

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
- Manifest resolves with no `OBSIDIAN_VAULT` set (host default applied).
- Core: `agent create` leaves an `agents/<name>` node in the shared vault; a
  session create leaves a `projects/<name>` node.
