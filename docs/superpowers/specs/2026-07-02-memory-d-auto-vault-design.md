# Memory D — Auto-vault provisioning

**Date:** 2026-07-02
**Status:** Design (approved for spec)
**PR order:** 4 of 4 (C → B → A → D)
**Repos touched:** `herrscher-obsidian-memory`, `core`

## Context

The Obsidian memory plugin requires a pre-existing vault: its manifest declares
`vault` / `OBSIDIAN_VAULT` as **Required** (`register.go`), and `New(root)`
expects the directory to exist. So today a human must create the vault before
herrscher can remember anything — contradicting "herrscher gère sa propre
mémoire tout seul."

Meanwhile the durable-Agent model (see the Neublox design notes) puts an agent's
home at `~/.herrscher/agents/<name>/` with a `memory/` subtree that must survive
session teardown (worktrees are deleted on `session close`, so memory lives in
the agent home, not the worktree).

## Goal

herrscher **creates and scaffolds** its own Obsidian vault on first use — no
manual `OBSIDIAN_VAULT` setup — and the vault opens cleanly in the Obsidian app.

## Non-goals

- **No vault-per-agent.** One vault holds all agents, with shared (Project) and
  private (Agent) subtrees per the existing `MemoryScope` — do not fragment the
  graph. (Multi-vault physical split is explicitly rejected here.)
- No Obsidian plugin/theme installation — just a minimal `.obsidian/` so the app
  opens the folder as a vault without prompting.

## Design

### 1. `EnsureVault` (obsidian-memory)

Add a provisioning entry point that is idempotent and safe:

```go
// EnsureVault creates the vault directory if absent, writes a minimal
// .obsidian/ app config so Obsidian opens it as a vault, and returns an opened
// *ObsidianMemory. Existing vaults are opened untouched.
func EnsureVault(root string) (*ObsidianMemory, error)
```

- If `root` does not exist: `MkdirAll`, then write a minimal `.obsidian/app.json`
  (+ empty `appearance.json`) so the Obsidian app treats it as a vault.
- If `root` exists: behave exactly like `New(root)` today (open, never
  overwrite config).
- Reuses the existing `os.Root` sandboxing and flock — no new locking model.

`New` stays as-is (open-only, strict); `EnsureVault` is the create-or-open
superset that the manifest/host uses.

### 2. Scope-root scaffolding on first use

After the vault exists, ensure the P1 scope roots exist so
`RecordShared`/`RecordPrivate`/`RecallScoped` have anchors:

- The shared `KindProject` node (`projet .../index`) — via existing `Init`.
- The private `KindAgent` node for the current agent — a new tiny
  `EnsureAgent(ctx, agentKey, title)` that `ensure`s a `KindAgent` node under
  the agent home key (idempotent, non-overwriting like `Init`).

This makes `MemoryScope{Project, Agent}` valid immediately, so B can record and
A can recall from turn one.

### 3. Manifest / config change (obsidian-memory)

Relax `register.go`: `vault` becomes **not Required** with a default resolved by
the host to the agent home (`~/.herrscher/agents/<name>/memory`). The plugin
factory calls `EnsureVault` instead of `New`, so a missing directory is created
rather than erroring.

### 4. Core wiring (herrscher)

- On **agent create** (`agent create <name>` in the durable-Agent model), core
  computes the vault path under the agent home and calls `EnsureVault` +
  `EnsureAgent` so the vault and roots exist before any session runs.
- The memory scope passed into a session (provisioning) names that vault's
  Project + Agent roots — this is where D meets the existing `MemoryScope`
  plumbing.
- If the durable-Agent entity isn't built yet in core, D ships the plugin-side
  `EnsureVault`/`EnsureAgent` + manifest relaxation first (usable via
  `OBSIDIAN_VAULT` pointing at a not-yet-existing path), and the core
  `agent create` wiring lands with the Agent entity work.

## Interfaces changed

| Symbol | Repo | Change |
|--------|------|--------|
| `EnsureVault` | obsidian-memory | new create-or-open constructor |
| `EnsureAgent` | obsidian-memory | new idempotent Agent-root scaffold |
| manifest `vault` | obsidian-memory | Required → optional + host default |
| plugin factory | obsidian-memory | calls `EnsureVault` not `New` |
| agent create | core | ensures vault + roots under agent home |

No breaking change to `New` or the `Memory` port; the manifest relaxation only
widens what configs are accepted.

## Testing

- `EnsureVault` on a non-existent path creates the dir + `.obsidian/app.json`,
  returns a working memory; `Record`/`Recall` roundtrips.
- `EnsureVault` on an existing vault leaves its `.obsidian/` untouched.
- `EnsureAgent` twice creates the `KindAgent` node once (idempotent).
- After `EnsureVault`+`Init`+`EnsureAgent`, a `MemoryScope{Project, Agent}`
  round-trips through `RecordShared`/`RecordPrivate`/`RecallScoped`.
- Manifest resolves with no `OBSIDIAN_VAULT` set (host default applied).

## Open question (non-blocking)

Vault location default: `~/.herrscher/agents/<name>/memory` (per-agent home) vs a
single shared `~/.herrscher/memory` with agents as subtrees. Leaning shared
single vault to honour the one-graph rule; confirm when the core Agent entity is
specified.
