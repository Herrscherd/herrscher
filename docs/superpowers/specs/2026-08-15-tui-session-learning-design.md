# A TUI session that learns

Date: 2026-08-15
Status: approved, implementing
Scope: chantier 1 of 2 (the project registry in the vault is chantier 2)

## Problem

`openDefaultSession` (`plugins/terminal/terminal.go:110`) creates the window's
conversation with three fields:

```go
contracts.CreateSession{Name: name, TerminalOnly: true, Shared: true}
```

No `Project`, no `Agent`, no `Extractor`. What follows from that:

- **Nothing is ever distilled.** `herrscher-orchestrator/register.go:50` builds a
  `Learner` only when `memory.extractor` is set; otherwise it builds a plain
  `Curator`. A TUI session always gets the Curator, so `Consolidate` never runs.
- **What is remembered hangs off nothing.** With no project scope,
  `Curator.remember` files the note under `sessions/<name>/notes/<slug>`
  (`conscious.go:85-91`) instead of the project root, and `RecordShared` links it
  to the empty key. `RecallScoped` has no root to walk, so the next session never
  sees it — only a full-text `Search` would.
- **There is no private root either**, and there cannot be one today:
  `session create` rejects an agent unless the session has an isolated git
  worktree (`core/internal/manager/session.go:551`), while a TUI session is
  `Shared: true` by construction.

So the moat fills only through sessions someone configured by hand. The tool the
operator actually uses all day contributes nothing.

Two things the code already gives us for free, and which shape the design:

- **The journal is not required.** `Learner.consolidateLocked` reads the
  transcript back out of memory (`l.mem.Recall(ctx, l.session, 0)`) and treats a
  missing journal as `""`. A TUI session can learn with no neublox anywhere.
- **Memory is always mounted.** `buildMemory` resolves the obsidian vault with no
  required setting (`OBSIDIAN_VAULT`, default `~/.herrscher/memory`). Nothing has
  to be provisioned for this to work.

## Decisions

1. **Location proposes, the first prompt decides, then it is pinned.** The
   directory gives a candidate project; while the session has not been pinned,
   the first real prompt is confronted with the projects the vault already knows
   and may re-scope it. After that the scope is fixed for the session's life.
2. **Memory is a tree with both roots.** A TUI session carries a project root
   *and* an agent root — not one or the other.
3. **The agent root defaults to `tui`,** overridden when the operator names a
   specific agent.
4. **Learning is on by default and keeps pace** with the conversation, not
   deferred to close.
5. **Everything above is a setting** on the terminal plugin's manifest.
6. **Thin vertical first.** The project registry as an internal component of the
   vault is chantier 2; it replaces the resolution rule from underneath without
   changing anything above it.

## Design

Three modules. `herrscher-obsidian-memory` is deliberately untouched.

### 1. herrscher-contracts v0.4.0 — two fields

```go
// CreateSession
// MemoryAgent names the private memory root this session writes its learned
// skills under, and nothing else. It is deliberately separate from Agent: that
// field is a provisioning directive (materialize this durable companion into an
// isolated worktree), and a session that only wants a place to put what it
// learned should not have to be provisioned to get one.
MemoryAgent string

// Event
// Project carries the memory project this session resolved to, piggybacked on
// the terminal reply{done} so the daemon can persist it — the same path Resume
// already takes. Empty when the scope did not change this turn.
Project string
```

Nothing else changes in contracts. `Agent` keeps its exact current meaning, and
the worktree rule guarding it is not relaxed.

### 2. herrscher-orchestrator v0.2.0 — one optional method

```go
// SetScope re-roots the curator's memory scope mid-run. The bridge calls it once,
// when the first turn pins a project the launch could only guess at. Serialised
// under the same mutex as Consolidate so a pass in flight never sees a half-
// changed scope.
func (c *Curator) SetScope(s contracts.MemoryScope)
```

Discovered by the bridge through a type assertion, exactly like
`Start(context.Context)` is today (`core/bridge/bridge.go:52`) — so
`contracts.Orchestrator` stays unchanged and an orchestrator that does not
implement it simply keeps the scope it was built with.

### 3. herrscher — the actual work

**a. State and creation.**

- `state.Session` gains `MemoryAgent string` and `ProjectPinned bool`.
  `ProjectPinned` is true when a human named the project and false when the host
  guessed it from the location; only the latter may be re-scoped.
- `session create` gains the params `memory_agent` and `project_pinned`.
  `hub.Create` maps `spec.MemoryAgent` through, and sets `project_pinned` when
  the caller supplied a non-empty `spec.Project`.
- `state.SetProjectPinned(name, project string) error`, mirroring
  `SetResumeToken`'s locking and persistence (`state.go:286`).

**b. Resolving a candidate at launch** — `core/host/scope.go`, new file.

```go
// ProjectFromDir names the project a session started in dir belongs to: the git
// repository the work is in, slugified. It answers "" when dir is in no
// repository, or when the name it yields could not be a project.
func ProjectFromDir(dir string) string

// MatchProject picks, among the projects the vault already knows, the one a
// prompt is about — "" when the prompt is about none of them. It is pure and
// takes the known names from the caller, so the rule is testable without a vault
// and chantier 2 replaces it wholesale with a registry lookup.
func MatchProject(prompt string, known []string) string
```

`ProjectFromDir` walks to `git rev-parse --git-common-dir` rather than to the
worktree, so three worktrees of one repository answer with one project, and
slugifies through the same normalisation `contracts.ProjectKey` uses so a scope
can never split in two by case. A name that fails `projectRe` yields `""`, and
the session is created unscoped exactly as today.

The two are deliberately separate: the location knows nothing about what the
vault holds, and the prompt match knows nothing about the filesystem.

**c. Pinning at the first turn** — the bridge.

The bridge already holds the two things the decision needs: memory, and the
prompt. It resolves once, on the first input frame of a session whose project is
not pinned:

1. List known projects with `Search(Query{Kinds: []NodeKind{KindProject}})`.
   Memory unreachable, or the search failing, degrades to "no known projects" —
   the launch candidate stands.
2. Confront the first prompt with them through `MatchProject`. A prompt that names a known project
   takes it; one that names nothing recognisable leaves the launch candidate
   alone. **This match is textual in chantier 1** — a known project's name
   appearing in the prompt — and it is the one piece this design expects to get
   wrong in the field. That is the point of shipping it: chantier 2 replaces the
   rule with a registry lookup, and the seam is `MatchProject`.
3. If the project changed: `provisionScope` for the new root, `SetScope` on the
   orchestrator, and fold the name into the turn's `reply{done}` event.
4. `turnloop.go` persists it the way it persists `Resume` (`turnloop.go:628`),
   marking the session pinned. Either way the session is pinned from here on.

Injected through `bridge.Options` as a `ScopeResolver` so the turn driver stays
testable without a vault.

**d. What `openDefaultSession` sends.**

The terminal plugin's manifest grows a `Config` bag, and `newGatewaySet` keeps
the resolved `PluginConfig` on the `Terminal` struct instead of discarding it:

| Setting | Env | Default | Meaning |
|---|---|---|---|
| `learn` | `TERMINAL_LEARN` | `true` | false disables everything below; the session is created exactly as today |
| `extractor` | `TERMINAL_EXTRACTOR` | `llm` | which registered extractor distils the transcript |
| `consolidate-every` | `TERMINAL_CONSOLIDATE_EVERY` | `10` | turns between passes; 0 = manual/idle only |
| `memory-agent` | `TERMINAL_MEMORY_AGENT` | `tui` | the private memory root |
| `project` | `TERMINAL_PROJECT` | *(empty)* | forces the project and pins it, skipping resolution entirely |
| `project-pin` | `TERMINAL_PROJECT_PIN` | `first-turn` | `first-turn` \| `launch`, which resolves from the location and never re-scopes |

Every setting is `Required: false`, so a build with no environment behaves as the
defaults describe. `learn=false` is the single switch that restores today's
behaviour exactly.

## Data flow

```
herrscher (launch)
  └─ openDefaultSession
       CreateSession{Name, TerminalOnly, Shared,
                     Project: "", MemoryAgent: "tui",
                     Extractor: "llm", ConsolidateEvery: 10}
          └─ hub.Create → ResolveProject(cwd, knownProjects) → "herrscher"
               └─ session create → state.Session{Project: "herrscher",
                                                 ProjectPinned: false, …}
                    └─ supervisor → herrscher bridge --project herrscher
                                      --agent tui --extractor llm
                                      --consolidate-every 10
                         └─ buildOrchestrator → Learner{scope: {projects/herrscher,
                                                                agents/tui}}

first prompt ─ "je bosse sur neublox aujourd'hui"
  └─ bridge: not pinned → known projects → "neublox" wins
       ├─ provisionScope(projects/neublox)
       ├─ orch.SetScope({projects/neublox, agents/tui})
       └─ reply{done, project: "neublox"}
            └─ turnloop → state.SetProjectPinned("herrscher", "neublox")

turn 10 ─ Consolidate → extractor over the transcript
            ├─ facts   → projects/neublox
            └─ skills  → agents/tui
```

## Error handling

The governing invariant is the one the orchestrator already states: **learning
never breaks a turn.** Every new failure mode folds into it.

- Memory unreachable at launch → no known projects → the location candidate is
  used unchanged. A launch never fails because the vault is missing.
- `ResolveProject` produces nothing valid → the session is created unscoped,
  which is exactly today's behaviour.
- The first-turn re-scope fails to persist → memory still records under the
  resolved root for this run; the row keeps the launch candidate and is re-pinned
  on the next start. Logged at warn, never fatal.
- The extractor fails (no backend credentials, a refused call) → `Consolidate`
  already records the first error and runs the sweep anyway. Unchanged.
- An orchestrator that does not implement `SetScope` → the type assertion fails,
  the scope stays as built, and the event still carries the project so the row is
  right for the next start.

## Testing

- `ProjectFromDir`, table-driven: a worktree under a repo, a subdirectory of a
  repo, a non-git directory, a repository whose name fails `projectRe`.
- `MatchProject`, table-driven: a prompt naming a known project, one naming none,
  one naming two (the first named wins), a match differing only by case.
- Pin-at-first-turn against a fake memory holding two known projects: a prompt
  naming the other one re-scopes, emits the project on `reply{done}`, and calls
  `SetScope`; a prompt naming nothing leaves the scope alone.
- `turnloop` persists `e.Project` and marks the session pinned, alongside the
  existing `Resume` assertion.
- `openDefaultSession` with default config sends extractor/cadence/memory-agent;
  with `learn=false` it sends the three fields it sends today and nothing more.
- Regression: `session create --agent X --shared` still fails. Decoupling
  `MemoryAgent` must not have relaxed the worktree rule.
- Regression: a session created with an explicit project is pinned at launch and
  is never re-scoped by any prompt.

## Order of work

`herrscher-contracts` v0.4.0 is two struct fields and blocks the other two. Once
it is tagged, `herrscher-orchestrator` v0.2.0 (one method) and the `herrscher`
work are independent and can run side by side; `herrscher` only needs the
orchestrator tag at the moment it bumps `go.mod`.

## Out of scope

- The project registry as a component of the vault, and the path → project
  association it would own. That is chantier 2, and this design is shaped so it
  replaces `ProjectFromDir` and `MatchProject` without touching anything above
  them.
- Relaxing the isolated-worktree rule for `Agent`. `MemoryAgent` exists precisely
  so we do not have to.
- Per-turn re-scoping. The scope is pinned once; a session that changes subject
  is a session the operator can close.
