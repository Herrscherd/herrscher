# Discord slash commands (live, all handling in the gateway plugin)

Decisions (locked with the user):
- **Transport**: Discord Gateway **WebSocket** in the plugin (dctl stays REST-only;
  the WS client lives in `herrscher-discord-gateway`, listens for `INTERACTION_CREATE`).
- **Commands**: the old dctl catalog, minus everything `workspace`, plus `set home`:
  `/allow add|remove|list`, `/session create|close|list|who`,
  `/session allow add|remove|list`, `/set home`, `/service restart|update`.
  All old parameters kept (except workspace/project-from-workspace).
- **Seam**: neutral `contracts.SessionControl` (AddSession/RemoveSession/ListSessions).
  The hub implements it; the gateway receives it and calls it from its slash handlers.
  **No Discord-specific code in core.**

## Key facts found
- `manager.Handler` already emits neutral `contracts.Cmd` and does the full
  create/close (worktree, channel, forge, persist, `sup.Start`/`sup.Stop`).
- `RunHub` (`core/host/serve.go:143`) loads sessions from `state.json` **only at
  startup** — no watcher. The per-session go-live step is
  `control.Accept` + `go RunSession(...)` + `sup.Start(sess)` (serve.go:162-171).
- The operator CLI `herrscher session create` is the legacy path; it can't bring a
  session live in an already-running daemon (starts the bridge in the wrong process).

## Phases

### Phase 1 — contracts seam (herrscher-contracts)
- Add `SessionControl` interface + neutral `SessionSpec` / `SessionInfo`.
- Add opt-in `SessionControlReceiver { BindSessionControl(SessionControl) }` so a
  gateway can receive the controller without changing the factory signature.
- Tag a release.

### Phase 2 — hub implements SessionControl (herrscher core)
- Refactor `RunHub` so the per-session "go live" wiring is a reusable method on a
  hub struct (holding st, sup, gws, partDir, ctx).
- Implement `AddSession`/`RemoveSession`/`ListSessions` reusing `manager.Handler`
  for resource work, then the go-live/go-dead wiring on the running hub.
  Avoid double `sup.Start` (handler vs hub own exactly one).
- After building gateways, call `BindSessionControl` on any gateway implementing
  `SessionControlReceiver`. Stays discord-agnostic.

### Phase 3 — Discord Gateway WebSocket client (herrscher-discord-gateway)
- Minimal gateway WS: connect, IDENTIFY (intents: none beyond default needed for
  interactions), heartbeat, RESUME; decode `INTERACTION_CREATE` into
  `dctl.Interaction`; reconnect with backoff. TTY-independent, runs in a goroutine
  started from the factory/receiver.

### Phase 4 — slash registry + handlers + allowlist (herrscher-discord-gateway)
- Build a `dctl.Registry` with the locked catalog (builders from dctl).
- Handlers translate interaction → `SessionControl` calls (session create/close/list)
  and persist `home`/allowlist in the plugin's own store.
- Allowlist enforcement before dispatch; ephemeral error replies.
- `Defer` long ops; `EditResponse` with the result.

### Phase 5 — wire-up
- On startup: `Registry.Sync` to register commands for the guild.
- `BindSessionControl` receiver kicks off the WS listener.
- Build/test/CI green in all three repos; bump versions; PRs.
