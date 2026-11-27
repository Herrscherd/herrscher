# Cross-backend skills

A **skill** is an Anthropic-format `SKILL.md`: a `---` fenced frontmatter block carrying
`name` and `description`, then a markdown body of instructions. Herrscher makes any such
skill usable on **every** backend, including codex, cursor and antigravity, which have no native skill
loader, by replicating Claude's progressive disclosure at the bridge seam.

The engine lives in the dependency-free `core/skills` package (`Discover` + `Engine`);
the bridge wires it in `core/bridge/skills.go`.

## Where skills are discovered

The search path is ordered, and **de-duplication is by `name` with earlier roots winning**:
so a repo skill overrides a global one of the same name.

| Order | Root | Why |
|-------|------|-----|
| 1 | `<workspace>/.claude/skills/<name>/SKILL.md` | The session's repo: the hub runs with `cwd = workspace`, and a one-shot run reads the session's worktree. |
| 2 | `<workspace>/.herrscher/.claude/skills/<name>/SKILL.md` | The skills this agent wrote for itself, projected from memory at session start. |
| 3 | `~/.claude/skills/<name>/SKILL.md` | User-global skills. |
| 4 | `skills.roots` from `config.json` | Extra roots, appended after the three defaults. |

The order is the policy. A repository's own skill beats one the agent taught itself, so a
self-authored procedure can never shadow one the project committed; a learned skill beats
a machine-wide playbook, because this agent's experience on this project is more specific
than an instruction to the machine. Root 2 exists only when self-authored skills are on,
and it belongs to the projection: it is rewritten and pruned at session start, so nothing
hand-written should live there. The projection runs in both places a session starts: the
hub of a long-running session and the one-shot run of `--print` or a seed, which projects
into the session's worktree before its backend starts and builds its host-side engine on
that worktree, not on the working directory of its process. An earlier release projected
into `<workspace>/.herrscher/skills`; that directory is removed at the next projection
that reads the vault, and kept when the vault cannot be read. Nothing is projected,
removed or handed to the vendor when `.herrscher`, `.herrscher/.claude` or one of the two
skill roots is a symbolic link, since a repository could commit one that points outside
the worktree. A skill whose directory is a symbolic link is skipped, and each `SKILL.md`
is written to a temporary file and renamed into place, which replaces a linked `SKILL.md`
without writing its target.

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
injects nothing, so no double loading.

Backends that do not implement the capability, such as codex, cursor and antigravity, get the host-side
engine.

The CLI does not read root 2 on its own, so the host hands the backend the directory that
holds it, `<workspace>/.herrscher`, as the `skill-root` setting. The claude backend passes
it as `--add-dir` in its stream strategy, and the CLI loads the skills under
`<workspace>/.herrscher/.claude/skills` as native skills. Without it, a skill the agent
learned reached a claude session through no path: the recall leaves skill nodes out, since
they are meant to come from the projection, and the engine that would list them is off for
a native backend. Codex, cursor and antigravity ignore the setting.

The engine is also nil when the feature is switched off in config. A nil engine means the
memory context passes through `withSkills` unchanged.

## The `/skills` panel in the TUI

In the terminal TUI, **`/skills`** opens a read-only modal panel listing the discovered
skills: name and description per row, `↑↓` to move the selection, `Esc` to close. It
shows at most 12 rows (a third of the window height, at least 5) and then a `… +N more` line.

The panel is **session-agnostic** (the TUI is one gateway over many sessions), so it lists
from `~/.claude/skills` plus any configured extra roots and deliberately omits per-session
workspace roots. Rows are sorted by name.

## Config

In `~/.config/herrscher/config.json`, all optional:

```jsonc
"skills": {
  "enabled": true,                 // omit or true = on; false disables injection
  "roots": ["/opt/team-skills"],   // extra roots, appended after the three defaults
  "budget": {                      // omit = no budget (every skill is listed, every body in full)
    "menuTokens": 600,             // cap on the menu, in estimated tokens
    "expansionTokens": 4000        // cap on the active skill bodies, in estimated tokens
  }
}
```

A missing `skills` object, or a missing `enabled`, means the feature is **on**. A missing
`budget` object, or a zero field in it, means that block is **unbounded**: the menu and the
expansions are then exactly what they were before budgets existed.

The budget is read from the daemon's `config.json`, so it applies to every session the
daemon runs; the engine is built per session from that file.

## Budgets

Both caps are estimated with the same four-bytes-per-token estimate the context window
uses (`contextwindow.EstimateTokens`). They are estimates, not vendor counts.

**The menu.** When the full menu is over `menuTokens`, the engine chooses which skills to
list, and no skill is ever dropped silently:

- A skill that is **active** (its body is expanded this session) is always listed, even
  when that alone is over the cap.
- The other skills are ranked by a deterministic lexical score against the current
  message: each word of the message that occurs in the skill's name counts twice, and each
  word that occurs in its description counts once. Words shorter than three letters are
  ignored. Ties break by name, so the same message and the same skills always give the
  same menu.
- Skills are added in that order while the menu stays within the cap.
- The menu ends with a localized line saying how many more skills exist and that the user
  can list them all with `/skills` in the terminal.

With no message, or a message that shares no word with any skill, the ranking falls back
to name order.

**The expansions.** The active bodies share one cap, taken in name order. A body that
fits is injected whole. A body that does not fit is cut at the last whole line that fits
the remaining budget (mid-line only when one line alone is longer than that), and a localized line follows it naming the skill and the absolute
path of its `SKILL.md`, which the model can `Read` in full. A skill that comes after the
budget is spent is still listed, with an empty body and the same line, so nothing is
silently hidden. The tags and these notices are outside the cap; the bodies are inside it.

The cut is a fallback, not a filter: the engine never removes a skill from the active set
because of the budget, and the full text is always reachable by reading the file.

## Related

- Backend (Claude): the backend that opts out via `SkillNative`.
- The contracts ports: where the capability is declared.
- [Configuration](env.md): the `config.json` precedence chain.
- Terminal (in-tree): the gateway that hosts the `/skills` panel.
