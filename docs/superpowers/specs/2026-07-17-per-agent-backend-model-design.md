# Per-agent backend + model — design

**Date:** 2026-07-17
**Status:** Approved (design), pending implementation plan

## Goal

Let a multi-agent orchestration run mix backends **per durable agent**, so the
scenario "orchestrator agent runs Fable, its builder agents run gpt-5.6" works
end to end. Concretely:

```
agent create orchestrator --backend claude --cmd "claude --model claude-fable-5"
agent create builder      --backend codex  --cmd "codex --model gpt-5.6"
```

When the orchestrator session `⟢ delegate: builder <task>`, the spawned worker
must actually answer with codex/gpt-5.6 — not silently fall back to Claude.

## Why this is (mostly) already built

Two axes describe "which model answers":

- **Vendor** — which backend plugin builds the responder (`claude` / `codex` /
  `cursor`). Stored per agent in `<agent-home>/backend`
  (`core/internal/agent/store.go:112` `readBackend`), inherited into the session
  at `core/internal/manager/session.go:195` (`vendor = a.Backend`), persisted as
  `state.Session.Vendor` (`core/internal/state/state.go:25`).
- **Model within a vendor** — Fable vs Opus, or the specific gpt model. In the
  live path this is **already carried inside `cmd`**: `supervisor.bridgeArgs`
  passes `--cmd sess.Cmd` but never `--model` (`core/internal/supervisor/supervisor.go:35`),
  and `newSeedBackend` sets `cfg.Settings["cmd"]`/`["kind"]` but never
  `["model"]` (`core/host/seed.go:137-142`). So no backend consumes a `model`
  key today; the invocation string is the de-facto model carrier.

The orchestrator spawns workers **from a named durable agent**:
`coordinator.spawn` → `creator.Create(CreateSession{Agent: toAgent, ...})`
(`core/host/coordinator.go:190`). Session-create reads that agent and inherits
its settings. So per-agent config flows automatically through the agent name —
no change to `contracts.CreateSession` or `hub.Create` is needed.

### The one real gap

`Session.Vendor` is honored **only** on the one-shot seed path
(`newSeedBackend`, `seed.go:117`). The persistent live bridge ignores it:

- `supervisor.bridgeArgs` (`supervisor.go:34`) never passes the vendor.
- `bridge.go`'s `newBackend` (`bridge.go:50-63`) hardcodes `claude.NewBackend`
  whenever the remote resolver isn't active.

So every live session answers with Claude regardless of its agent's vendor.

## Design

Chosen mechanism: **cmd-per-agent** (reuse the existing `cmd` plumbing) rather
than a dedicated `model` field. Rationale: the model already lives in `cmd`,
`cmd` is already inheritable and threaded to the live bridge, and this touches
**zero external backend modules** (each vendor CLI receives its own invocation
string). A dedicated `model` field would require every backend plugin to learn a
new setting key — external modules with unknown support.

Two independent pieces:

### Piece 1 — Live bridge honors the session vendor (the unblock)

Make the persistent bridge select its backend by vendor exactly as the seed path
already does.

1. `core/internal/supervisor/supervisor.go` — `bridgeArgs` threads the vendor:
   append `--vendor sess.Vendor` when `sess.Vendor != ""`. (Distinct from the
   existing `--backend`, which is the *kind* stream|oneshot, not the vendor.)
2. `bridge.go` — add a `--vendor` flag to `runBridge`, and replace the
   hardcoded `claude.NewBackend` fallback in `newBackend` with registry
   selection mirroring `newSeedBackend`: resolve remote first, else
   `selectBackend(vendor, contracts.Default.Backends())`, resolve the plugin
   config, apply `cmd`/`kind`/`model` settings, call `plugin.Backend(ctx, cfg)`.
   - Extract the shared selection logic so live and seed cannot drift. Candidate:
     a `host` helper `BuildBackend(ctx, vendor, cmd, kind, model, dir)` that both
     `newSeedBackend` and `bridge.go`'s `newBackend` call. The claude direct
     import in `bridge.go` is then only a fallback for "no vendor + no plugin",
     or dropped entirely if the registry always has claude compiled in.

Behavior after Piece 1: a session whose agent has `--backend codex` actually
runs codex live. Vendor mixing works even without Piece 2 (model stays the
vendor default).

### Piece 2 — Agent stores its default cmd

Give a durable agent a default invocation string, inherited like `backend`.

1. `core/internal/agent/agent.go` — add `cmdFile = "cmd"` constant and a `Cmd`
   field on `Agent`.
2. `core/internal/agent/store.go` — add `readCmd(home)` (mirror `readBackend`),
   populate `Cmd` in `Get`/`List`, add `Cmd` to `CreateSpec`, write the `cmd`
   file in `Create` when set.
3. `core/internal/manager/agent.go` — `agentCreateRun` reads a `cmd` param and
   passes it into `CreateSpec`.
4. `core/internal/manager/session.go` — inherit the agent cmd. Precedence
   (explicit wins): `cmd:` param  >  agent's `Cmd`  >  `h.defaultCmd`. Wire it
   in the `agentName != ""` block near line 195, right where `vendor` is
   inherited, e.g. `if !cmdExplicit && a.Cmd != "" { cmd = a.Cmd }`.
5. CLI flag declaration — register the `cmd` flag on `agent create` wherever
   `agent create`'s flags are declared (same place `--backend` is declared).

Behavior after Piece 2: `agent create builder --backend codex --cmd "codex
--model gpt-5.6"` fully pins that agent's vendor+model; every session/worker
created from it inherits both.

## Data flow (end to end, after both pieces)

```
agent create orchestrator --backend claude --cmd "claude --model claude-fable-5"
agent create builder      --backend codex  --cmd "codex --model gpt-5.6"

session create lead --agent orchestrator
  → inherits vendor=claude, cmd="claude --model claude-fable-5"
  → live bridge selects claude backend, runs Fable

(in lead) ⟢ delegate: builder <task>
  → coordinator.spawn → Create{Agent: builder}
  → session-create inherits vendor=codex, cmd="codex --model gpt-5.6"
  → supervisor passes --vendor codex --cmd "codex --model gpt-5.6"
  → live bridge selects codex backend, runs gpt-5.6
```

## Constraints & non-goals

- **cmd/vendor coherence** is the operator's responsibility: an agent's `cmd`
  must invoke a CLI matching its `backend`. They're set together on the same
  `agent create`, so this is natural; no cross-validation is added.
- **No dedicated `model` field**, no `contracts.CreateSession.Vendor`/`Cmd`
  additions (inheritance rides the agent name), no changes to any external
  backend module.
- Existing `--model` flag on `bridge.go` stays for backward compat but remains
  unused by the supervisor; model is expressed through `cmd`.

## Testing

- **agent store**: `Create` with `Cmd` writes `<home>/cmd`; `Get`/`List` read it
  back; absent file yields empty (mirror existing `backend` tests).
- **session inheritance**: `session create --agent X` with no `cmd:` inherits the
  agent's `Cmd`; an explicit `cmd:` overrides it; neither set falls to
  `defaultCmd` (extend the existing agent-inheritance test that already asserts
  vendor inheritance, `core/internal/manager/agent_test.go:90`).
- **supervisor**: `bridgeArgs` includes `--vendor` when `sess.Vendor` is set and
  omits it when empty (mirror the existing `--backend`/`--project` arg tests).
- **backend selection**: the shared `BuildBackend` helper picks the plugin whose
  `Manifest.Kind` matches the vendor and errors on an unknown vendor (reuse
  `selectBackend`); a fake two-plugin registry asserts codex-vs-claude routing.
- **worker inheritance (integration-ish)**: a delegated worker created from an
  agent with vendor=codex ends up with `Session.Vendor == "codex"` and the
  agent's cmd (extend `agent_test.go:90` worker-inherits-codex).
