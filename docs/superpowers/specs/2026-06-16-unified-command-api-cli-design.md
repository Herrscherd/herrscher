# Unified Command API + CLI — Design

**Date:** 2026-06-16
**Status:** approved (design), pending spec review

## Goal

One command API for the whole platform. A command is declared **once** — name(space),
params, handler — and exposed through multiple **formats** (argv CLI now, Discord
slash later) without duplicating the declaration. The CLI is rebuilt on top of this
API now; the Discord slash format converges onto the same API later (gated on the
Memory module, out of scope here).

## The mental model

The unified thing is the **handler**, not the transport. `allowList` is one function.
`/allow list` (Discord slash) and `herrscher allow list` (CLI) are two **formats**
that both resolve to that same function. They differ only in how the input is parsed
and where the command is registered — never in what runs.

```
contracts   → the builder + the neutral Command type (the authority of shape)
core        → the registry: native, agnostic; only ever sees the neutral type
plugins     → each declares its commands via the contract:
                • gateway Discord → send/read/channel/allow… (closures capturing dctl)
                • backend / memory / orchestrator → their own
host (glue) → instantiates the registry, collects commands from core + every
              registered plugin, binds the CLI format onto it
```

Two invariants carried over from the platform's golden rule:

1. **`contracts` stays neutral** — it gets the command *declaration* types (a builder
   producing a value), zero logic, zero Discord. A CLI/command builder is platform-
   agnostic, so it belongs here alongside the other ports.
2. **`core` never learns about Discord.** The registry only manipulates the neutral
   `contracts.Command` value; the handler is an opaque closure. A Discord command's
   closure may capture `dctl`, but the registry that holds it imports nothing of the
   kind. This is what dissolves the "native core vs agnostic core" tension: the
   registry is native to core *and* agnostic, because it only sees the neutral type.

**Discord is a plugin** (the gateway), not a special layer. It implements a contract
like any other plugin. The host is **just glue** — no business logic, no commands of
its own; it collects and wires.

## Components

### 1. `contracts` — the declaration (neutral)

A new `contracts/command_cli.go`. This is **the** command concept contracts
proposes — neutral, format-agnostic. (The legacy slash types in `command.go` —
`Command`, `CommandData`, `Responder`, `InboundCommand` — are Discord-domain code
that mislives in contracts; they are left untouched here and evacuated later, so the
new type just needs a Go-level name that doesn't clash with the legacy `Command`
while both files coexist.) Proposed shape:

```go
// Cmd is one declared command: a namespaced name, its params, and the handler.
// The handler is opaque — the registry that holds it stays agnostic of whatever
// the closure captures (dctl, a backend, …).
type Cmd struct {
    Path   []string // namespace path, e.g. {"allow","list"} or {"serve"}
    Params []Param
    Help   string
    Run    func(ctx context.Context, in Input) error
}

type Param struct {
    Name     string
    Help     string
    Required bool
}

// Input is the parsed invocation handed to a handler, format-agnostic: the CLI
// fills it from argv, a future slash format fills it from the interaction.
type Input struct {
    Args  map[string]string // param name → value
    Rest  []string          // positional remainder
}

func (in Input) Get(name string) string { return in.Args[name] }

// Builder is the fluent declaration the user asked for.
func New(path ...string) *Builder
func (b *Builder) Param(name, help string, required bool) *Builder
func (b *Builder) Help(text string) *Builder
func (b *Builder) Do(fn func(context.Context, Input) error) Cmd
```

The namespace = `Path`. `{"session","create"}` maps to a slash subcommand *and* to
`herrscher session create` — one declaration, both formats, no duplication.

### 2. `core/cli` — the registry + dispatch (native, agnostic)

```go
type Registry struct { /* tree keyed by Path */ }

func (r *Registry) Add(c contracts.Cmd) error          // rejects duplicate Path
func (r *Registry) Dispatch(args []string) error       // argv → resolve Path → parse params → Run
func (r *Registry) Help(path ...string) string         // usage text, derived from the tree
```

`Dispatch` walks the namespace tree to find the deepest matching command, parses the
remaining argv into `Input` (flags `--name value` → `Args`, leftovers → `Rest`), and
calls `Run`. Unknown path or missing required param → a usage error. The registry
imports **only** `contracts` — never `dctl`, never a plugin.

### 3. Plugins expose commands via the contract

A plugin's factory already returns a `GatewaySet` (see `contracts/registry.go`). Add a
field so a plugin contributes commands built from its live config:

```go
type GatewaySet struct {
    // …existing ports…
    Commands []Cmd // commands this plugin exposes (closures capture the live gateway)
}
```

The gateway plugin builds `send`/`read`/`channel`/`allow`/… as `Cmd` values whose
`Run` closes over its `dctl` client, and returns them in `GatewaySet.Commands`. The
backend/memory/orchestrator plugins do the same for theirs when they have commands.

### 4. Host (glue) — collect + bind

The host:

1. Builds the core `Registry`.
2. Adds the **host/domain** commands (the ones that are genuinely agnostic:
   `serve`, `bridge`, `service …`, `session …`, plus `plugin`/`update`/`install`
   from `manage`). These are declared with the same builder.
3. For every instantiated plugin, `registry.Add` each `GatewaySet.Commands` entry.
4. `registry.Dispatch(os.Args[1:])`.

`main.go` shrinks from a hand-written `switch` to: build registry → collect → dispatch.

## How a command flows (CLI, today)

```
herrscher allow list --user 123
  → registry.Dispatch(["allow","list","--user","123"])
  → resolve Path {"allow","list"}  (registered by the gateway plugin)
  → parse Input{Args:{user:"123"}}
  → Cmd.Run(ctx, in)  → allowList closure (captures dctl)
```

## How the same command flows later (slash, out of scope)

```
/allow list user:123
  → gateway plugin maps the interaction to Input{Args:{user:"123"}}
  → same Cmd.Run  (the gateway resolves it against the same declared command set)
```

No second declaration of `allow list`. The slash format is a parser+presenter the
gateway plugin owns; it reuses the one `Cmd`.

**Future direction (post-Memory, not now):** the slash *registration* lifecycle
(add/remove/update of Discord application commands) moves down into `dctl` itself —
the low-level Discord client owns that mechanic. The gateway plugin then just **binds
names → functions** (slash command name → the declared `Cmd`), without owning the
registration plumbing. This is also when the legacy slash types leave `contracts`.

## Migration of the existing verbs

| Verb(s) | Today | After |
|---|---|---|
| `serve`, `bridge`, `service` | switch in `main.go` | host-declared `Cmd`s |
| `plugin`, `update`, `install` | `manage.*Cmd(args)` | host-declared `Cmd`s wrapping `manage` |
| `send`, `reply`, `read`, `watch`, `react`, `thread`, `channel` | switch in `main.go`, call `dctl` directly | **gateway plugin** declares them in `GatewaySet.Commands` |
| `session …`, `set …`, `allow` | Discord slash handlers (manager) | gateway plugin `Cmd`s (closures → core session methods) |

The channel/session/allow verbs leaving `main.go` for the gateway plugin is the step
that makes the host pure glue and the core blind to Discord.

## Scope

Decision (2026-06-16): **the legacy slash abstraction is deleted now, not deferred.**
Carrying two command systems is worse than a temporary regression. Accepted
consequence: **Discord slash commands stop working** until the later dctl/gateway
phase rebinds them; during that window the daemon is driven **only by the CLI**. A
temporary non-building `contracts`/`core` while consumers migrate is also accepted.

**In scope now:**

1. Add the neutral `Cmd` builder/type to `contracts`.
2. Add the `core/cli` registry + dispatch (native, agnostic).
3. **Delete** the legacy slash types from `contracts`: `Command`, `CommandData`,
   `CommandResponse`, `Responder`, `InboundCommand`, `CommandKind`/`Kind*`, and
   `CommandRegistrar` (slash registration surface).
4. **Rewrite** every `core/internal/manager` handler (`session`/`set`/`service`/
   `workspace`/`allow`) from the `func(ctx, contracts.Command) contracts.CommandResponse`
   shape onto a `Cmd` whose `Run` is `func(ctx, Input) error` — output to stdout, the
   slash-only mechanics dropped (`Private`, `Responder` ack-then-edit, autocomplete
   `Suggest`, menu `ChoicePick`).
5. **Strip** the slash dispatch loop from `core/host/serve.go` (the
   `InboundCommand`/`Responder`/`ChoicePick`/`Suggest` plumbing).
6. Host (glue) builds the registry from the core-native commands + `manage`
   (`plugin`/`update`/`install`), binds the CLI format, dispatches `os.Args[1:]`.

**Kept working:** the per-session **bridge** loop (read → backend → reply → persist).
Its only slash-ish coupling is the optional permission-menu via `MenuRouter`/`Choice`
— a *channel* capability, not a command type. Those stay for now; the bridge degrades
to plain text where they're absent (already its behaviour).

**Out of scope (gated on Memory / later dctl phase):** moving slash *registration*
into `dctl`, the gateway binding slash names → the declared `Cmd`s, and re-enabling
Discord slash on top of the unified API. The channel verbs (`send`/`read`/`channel`…)
and the session/allow commands re-surface as gateway-owned bindings then; for now they
exist only as core-native `Cmd`s reachable from the CLI.

## Risks / open questions

- **Go-level name** for the new type while the legacy `Command` still sits in
  `contracts/command.go` (they coexist until the slash evacuation). Proposed: `Cmd`.
- **Param model fidelity**: argv flags are flat; slash options nest (groups/
  subcommands). The `Path` slice models the nesting for both; confirm it's enough
  for the real slash trees (`/session create name:x shared:true`).
- **Where host/domain commands live**: `serve`/`session` are agnostic, so they can be
  declared in `core` (or `host`) — confirm during planning which package owns them.
