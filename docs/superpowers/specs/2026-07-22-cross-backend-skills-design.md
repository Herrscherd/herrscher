# Cross-backend skills for Herrscher — design

Date: 2026-07-22
Status: approved (design), pending implementation plan

## Goal

Make any `SKILL.md` skill (the Anthropic skill format: a folder with a
frontmatter `name` + `description` and a markdown body) usable from **every**
Herrscher backend — not just `claude`, which already loads skills natively, but
also `codex` and `cursor`, which have no native skill mechanism. Write a skill
once; use it everywhere.

## Non-goals (YAGNI)

- No execution of skill-bundled scripts and no sandbox. The model reads the
  skill body and its resource files with its own file tools.
- No TUI management panel (browse/toggle). Deferred to a later iteration.
- No hot-reload. Skills are discovered once at session start.
- No new skill authoring format. We consume the existing `SKILL.md` format.

## Background: the seam

`contracts.Prompt{Content, Context, Attachments}` is assembled per turn in
`core/bridge/hub.go:runOneTurn`. There:

- `Context` is filled from `orch.Context(turnCtx)` (memory recall) and handed to
  the backend, which folds it into the turn (`claude` does this in
  `withContext`).
- The assistant's final text `out` is available right after `resp.Respond`.

Because the daemon spawns **one bridge process per session**, `runOneTurn` has a
natural per-session home for skill state. This single location covers both
injection (augment `Context`) and expansion detection (scan `out`).

## Architecture

### 1. Discovery — `core/skills` (new, pure package)

- `Discover(roots []string) []Skill` scans each root for `*/SKILL.md`.
- Roots, in order: session workspace (`<dir>/.claude/skills`), global
  (`~/.claude/skills`), plus any extra roots from Herrscher config. Later roots
  do not shadow earlier ones; all discovered skills are offered, de-duplicated by
  `name` (first root wins on a name clash, so a repo skill overrides a global one
  of the same name).
- Each `SKILL.md` frontmatter is parsed into
  `Skill{Name, Description, Dir, bodyPath}`. The markdown body is loaded lazily
  (only when the skill is expanded).
- A malformed or `name`-less `SKILL.md` is skipped (logged), never fatal.

### 2. Progressive-disclosure engine — `core/skills.Engine`

One `Engine` per session (built in the hub). It holds the discovered skills and
the set of currently-active skill names.

- `Menu() string` — a compact block listing every discovered skill as
  `name: description`, prefixed with the activation instruction: to use a skill,
  emit `<use-skill>NAME</use-skill>`. Empty string when no skills were found.
- `Detect(out string)` — scans an assistant turn for
  `<use-skill>NAME</use-skill>` markers (tolerant of surrounding whitespace) and
  marks each named, known skill active. Unknown names are ignored.
- `Expansions() string` — for every active skill, its full `SKILL.md` body plus
  the absolute path of its directory (so the model can `Read` bundled resource
  files). Re-emitted every turn while the skill stays active.

Injection per turn: `Context = memCtx + Menu() + Expansions()`. The menu is
cheap (names + one-line descriptions); bodies enter context only after the model
asks for them, and only for skills it activated. This mirrors Claude's native
progressive disclosure: turn N the model requests a skill, turn N+1 its body is
present (a two-turn round trip, exactly like a tool call → result).

### 3. Backend gating — `contracts.SkillNative`

A new optional interface, symmetric with the existing `ResumeAware`:

```go
// SkillNative marks a backend that discovers and loads skills itself (e.g. the
// claude CLI). The host skips its own skill injection and detection for such a
// backend so skills are not double-loaded.
type SkillNative interface{ NativeSkills() bool }
```

- `herrscher-claude-backend` implements it, returning `true`. The hub then does
  **no** menu injection and **no** detection for claude sessions — the CLI
  already loads `.claude/skills` + `~/.claude/skills`.
- `codex` / `cursor` do not implement it (or return `false`); the hub runs the
  full engine for them.

### 4. Wiring — `core/bridge/hub.go`

- Build the `Engine` once when the hub starts a session (from the session
  workspace dir + config roots), unless the backend is `SkillNative`.
- In `runOneTurn`: if the engine is active, set
  `Context = join(memCtx, engine.Menu(), engine.Expansions())`; after
  `resp.Respond` returns, call `engine.Detect(out)`.
- When the engine is nil (native or no skills found), `runOneTurn` is unchanged.

### 5. Config

Herrscher config gains an optional `skills.roots []string` (extra roots appended
after the workspace + global defaults) and a `skills.enabled bool` (default
true) to switch the whole feature off. Defaults require no config change.

## Data flow (codex/cursor session)

1. Session starts → hub discovers skills from workspace + global roots → builds
   `Engine`.
2. Turn: `Context = memCtx + Menu()`. Model sees the skill menu.
3. Model decides to use `pdf-fill` → emits `<use-skill>pdf-fill</use-skill>` in
   its reply.
4. Hub `Detect` marks `pdf-fill` active.
5. Next turn: `Context = memCtx + Menu() + Expansions()`, where `Expansions()`
   carries the `pdf-fill` `SKILL.md` body + its absolute dir. Model follows the
   skill, `Read`-ing bundled files by path as needed.

## Error handling

- Unreadable root or malformed `SKILL.md` → skip that entry, log to stderr,
  continue. A skills problem never breaks a turn.
- Marker for an unknown skill → ignored.
- No skills discovered → `Engine` behaves as a no-op (empty `Menu`/`Expansions`),
  identical to today's behavior.

## Testing

- `core/skills` unit tests (temp dirs with real `SKILL.md` fixtures):
  discovery + name de-dup across roots, frontmatter parse (including malformed →
  skipped), `Menu()` rendering, `Detect()` marker parsing (whitespace variants,
  unknown names), `Expansions()` for active skills (body + absolute dir).
- Hub integration tests with a fake backend:
  - engine active → injected `Context` contains the menu;
  - after a turn emitting a marker → next turn's `Context` contains the body;
  - backend with `NativeSkills() == true` → no injection, no detection.

## Files touched

- `core/skills/` (new): `Skill`, `Discover`, `Engine`, frontmatter parser.
- `core/bridge/hub.go`: build + gate the engine, augment `Context`, call
  `Detect`.
- `core/config`: `skills.roots`, `skills.enabled`.
- `herrscher-contracts`: add `SkillNative`.
- `herrscher-claude-backend`: implement `NativeSkills() bool`.
