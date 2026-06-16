# Herrscher — Unified Binary + Plugin-Registered CLI — Design

**Status:** design locked (2026-06-16). Implementation plan to follow via writing-plans.

**Goal:** Collapse the Herrscher tooling into a single `herrscher` binary whose CLI
surface is *contributed by plugins* — not hardcoded. Kill the legacy dctl monolith
CLI that currently lives inside `herrscherd`. Plugins register their own
subcommands through `contracts`, exactly as they already register gateway/backend
factories.

**Architecture (one line):** the umbrella repo stops being a passive docs/symlink
skeleton and becomes the composition-root that *is* the binary — it requires
contracts + core + the plugins and builds `herrscher`.

---

## 1. Motivation — the "soucis"

- `herrscherd` today ships the **old dctl monolith CLI verbatim** (`send`, `reply`,
  `read`, `watch`, `react`, `thread`, `channel`), instantiating `dctl.New()`
  directly in `main()`. Its help even still prints `dctl — minimal Discord bot CLI`.
  These Discord-specific verbs do not belong baked into the host.
- Plugin selection is hardcoded: `buildGateway` returns the **first** registered
  gateway. With two gateways (discord + slack) or two backends (claude + gpt) you
  are stuck.
- Management (`herrscher-cli`) and execution (`herrscherd`) are two separate
  binaries/repos for what, in solo use, is one tool.

## 2. Module topology (LOCKED)

| Module (repo) | Role |
|---|---|
| `herrscher` (umbrella → binary) | composition-root: `main` + `plugins.go` + CLI dispatcher + management cmds + `core/` packages + wiring. `go build` → binary `herrscher`. Keeps the README/diagrams. |
| `herrscher-contracts` | authority/regulator — every dependency arrow points here. Extended with CLI-command declaration. |
| `herrscher-discord-gateway` | gateway plugin — gains its CLI subcommands (`send`/`read`/`react`/`thread`/`channel`/`watch`). |
| `herrscher-claude-backend` | backend plugin — unchanged (may expose commands later). |
| `dctl` | pure Discord REST library — unchanged. |
| ~~`herrscherd`~~ | absorbed into `herrscher` → **repo deleted**. |
| ~~`herrscher-cli`~~ | absorbed into `herrscher` → **repo deleted**. |
| ~~`herrscher-core`~~ | folded into `herrscher` as `core/` packages → **repo deleted**. |

**Why core folds in (not a separate module):** a separate Go module is justified by
substitution or multi-consumer reuse. `core` has neither today — its sole consumer
is the composition-root; plugins never import it (arrows go to `contracts`). Its
agnosticism is guaranteed by a **`purity_test`** forbidding concrete imports
(`dctl`, any plugin) in `core/...`, *not* by a module split. Extraction back into a
module is trivial later because the purity guard keeps it clean.

**Why contracts never folds in:** it is the authority every plugin imports. If it
became a subpackage of the binary, plugins would `import .../herrscher/contracts` —
inverting the dependency graph (plugin → host). Hard no.

**Install story:**

```sh
go install github.com/Herrscherd/herrscher@latest   # pulls all deps, builds `herrscher`
```

## 3. Single-binary `herrscher` structure

```
herrscher/
├── main.go            command dispatch (built-ins + plugin subcommand tree)
├── plugins.go         managed blank-import manifest (herrscher:plugins markers)
├── cli/               dispatcher: tree build, namespacing, promotion, conflicts
├── manage/            plugin add/remove, update, install (was herrscher-cli)
├── serve.go bridge.go service.go   daemon wiring (was herrscherd, dctl verbs removed)
├── core/              folded herrscher-core: core/host, core/bridge, core/config
│   └── purity_test.go forbids concrete imports in core/...
└── docs/              umbrella README + diagrams + this spec
```

**Host built-in commands (reserved names):** `serve`, `bridge`, `service`,
`plugin`, `update`, `install`, `plugins` (diagnostics), `help`. No business verbs.

## 4. contracts extension — CLI command declaration

`contracts` gains a command-tree type and a per-plugin CLI factory, mirroring the
existing `GatewayFactory`/`BackendFactory` shape.

```go
// Command is a declarative CLI node. Leaves carry Run; branches carry Sub.
type Command struct {
    Name    string   // verb segment, e.g. "send" or "channel"
    Summary string   // one-line help
    Usage   string   // optional usage line
    Promote bool     // request top-level promotion (opt-in, see §5)
    Run     func(ctx context.Context, args []string) error // leaf handler
    Sub     []Command                                       // nested subcommands
}

// CLIFactory builds a plugin's command tree from its runtime config. Called once
// at startup to assemble the dispatch table. Building the client is zero-cost
// (e.g. dctl.New stores a token; no network until a command runs).
type CLIFactory func(ctx context.Context, cfg PluginConfig) ([]Command, error)

type Plugin struct {
    Manifest Manifest
    Gateway  GatewayFactory
    Backend  BackendFactory
    CLI      CLIFactory   // NEW — optional
}
```

The discord plugin's `CLIFactory` builds its `dctl.Client` from `cfg` once and each
`Command.Run` closes over it. The Discord verbs move out of the host entirely and
become this tree (e.g. `send`, `read`, `react`, `thread`, and `channel` with
`list/create/post/delete/ensure` children).

## 5. Dispatcher — namespacing, promotion, conflict policy (LOCKED)

1. **Namespaced is canonical and always available.** `herrscher discord send …`
   always works — the unambiguous fallback, whatever happens.
2. **Promotion is opt-in.** A plugin may mark a command `Promote: true` to surface
   it at top level (`herrscher send`). Default is not promoted.
3. **Host built-ins are reserved.** `serve`, `bridge`, `service`, `plugin`,
   `update`, `install`, `plugins`, `help` always win; a promotion that would shadow
   one is refused (the verb stays reachable namespaced).
4. **Two plugins promoting the same verb = refuse to guess.** Neither is promoted
   (no "first in plugins.go wins" — that order is editable, hence fragile). Both
   stay reachable namespaced. The conflict is recorded.
5. **Conflicts are loud.** Surfaced in three places:
   - stderr warning at startup: `⚠ verb "send" promoted by discord AND xfoo — kept
     namespaced, use "herrscher discord send"`;
   - `herrscher --help` shows only resolved promotions; conflicted verbs appear
     under their namespace;
   - `herrscher plugins` lists each plugin, its commands, and flags promotions
     disabled by conflict.

Resolution logic lives in the **host dispatcher**, not in `contracts` — contracts
only *declares* the command structure and the promotion intent.

## 6. Migration

- Remove the legacy dctl CLI from the host: delete `runSend/runReply/runRead/
  runWatch/runReact/runThread/runChannel` and the `dctl —` usage text.
- Re-home those verbs in `herrscher-discord-gateway` as a `CLIFactory` tree.
- Move `serve`/`bridge`/`service` wiring into `herrscher` (dctl-direct calls gone;
  they already go through the plugin registry's `GatewaySet`).
- Move `plugin`/`update`/`install`/`plugins` from `herrscher-cli` into
  `herrscher/manage`. `install` now builds and installs the `herrscher` binary.
- Fold `herrscher-core` packages under `herrscher/core/...`; add `purity_test`.
- Replace the umbrella's `@herrscher/` symlink scheme with real `require`+`replace`
  in the composition-root `go.mod`.
- **Deleting the `herrscherd`, `herrscher-cli`, `herrscher-core` repos is
  destructive and public — execute ONLY with explicit user go-ahead at that step.**

## 7. Recorded invariants & deferred work (NOT in this scope)

- **Engine invariant.** A future orchestration engine consumes only the universal
  plugin ports. Engine-specific needs go through optional `Capabilities` + `Degrade`,
  **never** a new mandatory plugin method. A plugin implementing the base ports
  works with every engine. (We deliberately do **not** add a `contracts.Engine`
  seam now — speculative until a second engine exists.)
- **Orchestrator = mediator over the port graph**, not a second core. It is a port
  `core` *consults* ("for this context, which backend(s)/gateway(s) and what
  fallback?"). Default behaviour = today's trivial first/all + static degrade.
  Owns: selection among N plugins of a category, composition (fan-out/chains),
  resilience (failover/retry/circuit-break), and cross-cutting policy.
- **Orchestrator REQUIRES Memory** (hard dependency): its coordination is
  memory-backed. When the Orchestrator category is activated, the registry must
  verify a Memory plugin is present at startup, else refuse to start.
- Phase 1 transport (NATS/gRPC) unchanged by this design.

## 8. Out of scope

No Orchestrator/Memory/Engine implementation here. This spec covers only: the
single `herrscher` binary, the contracts CLI-command declaration, the dispatcher
with its conflict policy, the migration of Discord verbs into the plugin, and the
repo consolidation.
