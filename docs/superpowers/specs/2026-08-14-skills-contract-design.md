# Skills as a first-class plugin contract

Date: 2026-08-14
Status: approved, implementing

## Problem

A plugin can already carry playbooks: `contracts.Plugin` has a static
`Skills fs.FS`, and the host installs it from `installPluginSkills`. But skills
are not a contract the way a gateway or a backend is:

- there is no `CategorySkills`, so a skills-only plugin has no category to
  declare and no accessor to be found through;
- `Plugin`'s doc says "exactly one factory is non-nil, consistent with
  Manifest.Category", so a plugin that contributes only skills is illegal as
  written;
- the tree is fixed at compile time, so a plugin cannot look at the machine and
  decline: a playbook for a tool that is not installed sits in every agent's
  context forever.

Counted-like-the-others is otherwise nearly free: `core/host/serve.go:303`
announces every registered plugin with its category, the TUI plugin screen lists
modules from `plugins.go`, and `herrscher plugin add|remove` is generic.

## Design

### 1. The contract — herrscher-contracts v0.3.0

```go
const CategorySkills Category = "skills"

// SkillsFactory produces the plugin's playbook tree.
type SkillsFactory func(ctx context.Context, cfg PluginConfig) (fs.FS, error)
```

`Plugin.Skills` changes type from `fs.FS` to `SkillsFactory`. A factory rather
than an embedded tree so a plugin can refuse: a playbook whose tool is absent is
noise in every agent's context, and only the plugin knows whether the tool is
there.

The "exactly one factory" rule is restated: it governs the four *port* factories
(Gateway, Backend, Memory, Orchestrator), which must agree with
`Manifest.Category`. `Skills` is orthogonal — any plugin may set it, and a
plugin whose category is `skills` sets it and no port factory.

`Registry.Skills()` is added, `byCategory`, symmetric with `Gateways()`.

Breaking change, one caller: `herrscher-discord-gateway` wraps its embedded tree
in a one-line factory. Behaviour identical.

### 2. Host wiring

`installPluginSkills` takes a context and a getenv, walks `Plugins()`, and for
each non-nil `Skills`:

- resolves config **strictly when `Category == CategorySkills`**, leniently
  otherwise. This is the subtle part. `Resolve` fails on a Discord gateway with
  no `DISCORD_BOT_TOKEN`, and the static field existed precisely so a gateway
  that never instantiates still ships its playbook. Lenient resolution passes
  the partial `PluginConfig` that `Resolve` already returns alongside its error,
  which keeps that property. A *skills-category* plugin missing a required
  setting is reported and skipped, in the same words the gateway hub uses:
  `herrscher: skills not configured, skipping: superset: missing required
  config: home (set SUPERSET_HOME)`.
- calls the factory; an error or a nil tree is one line on stderr and never
  fatal, as today.
- installs through the existing `core/skills.Install`, so the shipped-manifest
  and divergence handling are unchanged.

Nothing is added for announcement or listing: both already go through
`Plugins()` and the module list.

### 3. herrscher-superset-skills

Module `github.com/Herrscherd/herrscher-superset-skills`, public, MIT, same
skeleton as the other plugin repos (`register.go`, `skills/<name>/SKILL.md`,
the org CI workflow).

Manifest: kind `superset`, category `skills`, status experimental, one setting
`home` bound to `SUPERSET_HOME` with default `~/.superset`, not required. The
factory checks that a real Superset install is there; if not it returns an
error, the host says so, and nothing is installed.

Content is original: no copy of the eight skills Superset installs and updates
itself under `~/.claude/skills/superset/`. Two writers on one tree would
duplicate every skill in every agent's context. Names are prefixed `superset-`
so a collision with that bundle is impossible.

- `superset-session` — a job arrives from chat and belongs in a Superset
  worktree: how a herrscher session and a Superset worktree line up, which owns
  the branch, where the PR comes from.
- `superset-handoff` — moving work between a Superset agent and a herrscher
  session in either direction, and where the answer lands.

Both are written against Superset's actual skills, read first. A playbook with
no real mechanism behind it does not ship.

### 4. Default hydration

A `require` in herrscher's `go.mod` and a blank import between the
`// herrscher:plugins` markers. Release order: contracts v0.3.0 → discord-gateway
bump → superset-skills v0.1.0 → herrscher bump and release.

### 5. Tests

**contracts** — the registry isolates `CategorySkills`; a skills-only plugin is
legal; a gateway may still carry skills.

**host** — the factory is called; a factory error is reported and does not stop
the other plugins; a skills-category plugin missing a required setting is
skipped with the "missing required config" wording; **a gateway with no token
still ships its playbook**, which is the regression this change could introduce.

**superset-skills** — the tree is returned when the Superset home exists and an
error when it does not; every SKILL.md has parseable frontmatter with a name and
a description.

## Rejected alternatives

**Two fields** (`Skills fs.FS` kept, `SkillsFn SkillsFactory` added): doubles the
surface so that one plugin can keep a one-line spelling.

**Strict category** (skills only on a `CategorySkills` plugin, so the Discord
playbook moves to its own repo): purest, but it splits every gateway into two
repos and two releases, and it makes installing a playbook without its tool
possible again.

**Vendoring Superset's own skills**: two writers on
`~/.claude/skills/superset/`, and every skill duplicated in every agent's
context.
