# Cross-backend skills

A **skill** is an Anthropic-format `SKILL.md`: a `---` fenced frontmatter block carrying
`name` and `description`, then a markdown body of instructions. Herrscher makes any such
skill usable on **every** backend, including codex and cursor, which have no native skill
loader, by replicating Claude's progressive disclosure at the bridge seam.

The engine lives in the dependency-free `core/skills` package (`Discover` + `Engine`);
the bridge wires it in `core/bridge/skills.go`.

## Where skills are discovered

The search path is ordered, and **de-duplication is by `name` with earlier roots winning**:
so a repo skill overrides a global one of the same name.

| Order | Root | Why |
|-------|------|-----|
| 1 | `<workspace>/.claude/skills/<name>/SKILL.md` | The session's repo: the bridge runs with `cwd = workspace`. |
| 2 | `<workspace>/.herrscher/skills/<name>/SKILL.md` | The skills this agent wrote for itself, projected from memory at session start. |
| 3 | `~/.claude/skills/<name>/SKILL.md` | User-global skills. |
| 4 | `skills.roots` from `config.json` | Extra roots, appended after the three defaults. |

The order is the policy. A repository's own skill beats one the agent taught itself, so a
self-authored procedure can never shadow one the project committed; a learned skill beats a
machine-wide playbook, because this agent's experience on this project is more specific than
an instruction to the machine. Root 2 exists only when
self-authored skills are on, and it belongs to the projection:
it is rewritten and pruned at session start, so nothing hand-written should live there.

`Discover` only scans **immediate** `<root>/<name>/SKILL.md` entries. A missing root, an
unreadable entry, or a `SKILL.md` that does not open with a `---` fence or has no `name`
is skipped, never fatal.

## Who writes into `~/.claude/skills`

Root 2 is not only the operator's. On every `serve` the daemon asks each compiled-in plugin for its playbooks through `Plugin.Skills` and installs them there. The binary carries no playbook of its own. A plugin's playbook therefore exists on a machine
only if that plugin is in the build, and only if its factory, having looked at
the machine, agrees to hand it over. Every plugin that declined is named on
stderr with its reason, so a missing playbook is never silent. Two plugins
claiming one skill name is reported too, and the first claim keeps it.

Your copies stay yours. A skill you edited is reported as **diverged** and left
alone: a playbook is a file precisely so it can be edited where the agent runs.
One still byte-identical to what an earlier release wrote is ours, and gets
replaced, so a rewritten playbook actually reaches the agent instead of freezing
at first install. The fingerprints live beside the skills in
`.herrscher-shipped.json`, a file and not a directory, so `Discover` never
mistakes it for a skill.

A playbook no build ships any more is retired on the same run. An unedited copy is removed and named on stderr. An edited copy is kept and named as diverged, with the path to delete by hand. A symbolic link at that path is never adopted, never removed and never followed: it is reported as a symbolic link and left as it is, with the path to delete to remove the link. Nothing is retired while a plugin failed to load this run, since its playbooks are still shipped, only unread.

See the plugin model for the contract side of this.

## Progressive disclosure, in two turns

Each turn the hub (`core/bridge/hub.go:runOneTurn`) appends the skill context to the
memory context before calling the backend:

1. **The menu, every turn.** `Engine.Menu()` renders a `<skills data-only="true">` block:
   one activation instruction, then one `- name: description` line per discovered skill.
   It is cheap by construction: descriptions only, no bodies. With no skills discovered
   the menu is empty and nothing is injected.
2. **The marker.** When the model decides it needs a skill it emits
   `<use-skill>NAME</use-skill>` in its reply. `Engine.Detect` activates every *known*
   name it finds (unknown names are ignored), and `Engine.Strip` removes the markers from
   the reply **before it is delivered**. The marker is an internal model→engine signal
   and never reaches the human-visible message.
3. **The expansion, next turn.** `Engine.Expansions()` emits the full body of every active
   skill, each fenced as `<skill name="…" dir="…">` with its **absolute directory**, so the
   model can `Read` bundled resource files by path. A body that fails to load is skipped.

The two-turn round trip mirrors Claude's native tool-call → tool-result.

> The marker match is tolerant: case-insensitive, and whitespace around the tags and the
> name is allowed.

## Hot reload

The roots are re-scanned at the start of every turn (`Engine.Refresh`), so a `SKILL.md`
you add or edit mid-session is picked up **without restarting it**. Skills the model has
already activated stay active; a skill that disappears from the roots drops out of the
active set.

## Backends that load skills natively

A backend that discovers and loads skills itself opts out via the `SkillNative` capability
in contracts:

```go
type SkillNative interface {
	NativeSkills() bool
}
```

The **claude** backend returns `true` (the `claude` CLI reads `.claude/skills` and
`~/.claude/skills` on its own), so `newSkillEngine` returns a **nil** engine and the hub
injects nothing, so no double loading. Backends that do not implement the capability, such
as codex and cursor, get the host-side engine.

The engine is also nil when the feature is switched off in config. A nil engine means the
memory context passes through `withSkills` unchanged.

## The `/skills` panel in the TUI

In the terminal TUI, **`/skills`** opens a read-only modal panel listing the discovered
skills: name and description per row, `↑↓` to move the selection, `Esc` to close. It
shows at most 10 rows and then a `… +N more` line.

The panel is **session-agnostic** (the TUI is one gateway over many sessions), so it lists
from `~/.claude/skills` plus any configured extra roots and deliberately omits per-session
workspace roots. Rows are sorted by name.

## Config

In `~/.config/herrscher/config.json`, all optional:

```jsonc
"skills": {
  "enabled": true,                 // omit or true = on; false disables injection
  "roots": ["/opt/team-skills"]    // extra roots, appended after the three defaults
}
```

A missing `skills` object, or a missing `enabled`, means the feature is **on**.

## Related

- Backend (Claude): the backend that opts out via `SkillNative`.
- The contracts ports: where the capability is declared.
- [Configuration](env.md): the `config.json` precedence chain.
- Terminal (in-tree): the gateway that hosts the `/skills` panel.
