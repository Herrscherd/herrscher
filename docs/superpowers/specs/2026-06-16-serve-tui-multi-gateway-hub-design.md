# `serve` TUI + multi-gateway hub — design

**Date:** 2026-06-16
**Status:** approved (brainstorm); implementation plan pending

## Problem

`herrscherd serve` today is a headless daemon that supervises one bridge
subprocess per session and prints `bot online.` to stderr. Two pain points:

1. `dctl` is a **REST-only** Discord client (no gateway/websocket), so the bot
   never shows green presence. `bot online.` is misleading, and there is no live
   local view of what sessions are doing.
2. Discord is the *only* way in. There is no way to drive a session from the
   terminal, and a session cannot exist without a Discord channel.

## Goal

`herrscherd serve` opens a **TUI** (Claude-Code-like: session tabs on the left, a
structured conversation transcript + input box on the right), with a `--headless`
mode for service runs. The terminal becomes a **first-class gateway**, a peer to
Discord: a session can be driven from the TUI, from Discord, or both at once, and
**everything is configurable** (which gateways are active, which gateways each
session binds to, the defaults).

Non-goal (now): adding a real Discord gateway WebSocket / presence. dctl stays
REST-only; the TUI is the live local frontend.

## Core decisions

- **The terminal/TUI is a gateway** — a `contracts` plugin implementing the
  `Gateway` port, peer to the Discord gateway. Not a monitoring console.
- **Core/daemon = multi-gateway hub.** It instantiates *all* registered gateways
  (today `buildGateway` picks `contracts.Default.Gateways()[0]`), holds the
  session→gateways table, merges inbound from every gateway, and fans out each
  reply to every gateway **bound to that session**. Input from any source is
  serialized into the one backend.
- **Sessions bind to a set of gateways.** `terminal-only` (no Discord channel),
  `discord-only` (today), or both. Terminal-only is in scope from the MVP →
  session ↔ Discord channel is decoupled.
- **Bridge subprocess = pure backend turn runner** in the worktree
  (`cwd=worktree` for the backend). Gateway I/O moves up to the daemon hub; the
  bridge is fed input and emits turn events over the per-session socket.
- **Transcript = structured turn events** (`you ▸` / `claude ▸` / status), not
  raw bridge stdout.
- **TUI input** is injected into the backend as an operator turn (extending the
  existing menu-pick injection), and the reply fans out to all bound gateways.
- **TUI ↔ daemon: both.** `serve` draws the TUI in-process (foreground); a later
  `attach` client connects to a daemon-level socket (so you can attach to a
  systemd daemon). Phased.
- The core stays agnostic: the hub knows only the `Gateway` port, never Discord
  or the terminal in concrete (`TestCorePurity` extended to cover the hub).

## Components

| Component | Role | Lives in |
|---|---|---|
| **Hub** (new, `core/host`) | Instantiate all registered gateways; session→gateways table; merge inbound; fan-out replies. Replaces `buildGateway`. | daemon |
| **Terminal gateway** (new plugin) | Implements `contracts.Gateway` + an input reader. `Post`/`Reply` → events to the TUI; input box → inbound messages. Self-registers via `init()`. | in-process (daemon) |
| **Discord gateway** (existing) | Unchanged in substance (REST poll/post, dctl internal to the plugin). | daemon |
| **TUI** (new, `core/tui`) | Render: session tab rail, structured transcript, input box, keybindings. Consumes terminal-gateway events. | daemon (serve) or `attach` client |
| **Bridge** (refactor) | Pure backend turn runner in the worktree. Receives input, runs the backend, emits turn events. Gateway I/O removed. | subprocess |
| **Session bus** (extends `internal/control`) | Per-session socket, structured bidirectional protocol: daemon→bridge (injected input/pick), bridge→daemon (turn events). | unix socket |

## Session model & config

- `state.Session` gains **`Gateways []string`** (e.g. `["discord","terminal"]`,
  or `["terminal"]` for terminal-only). `Channel` becomes optional (empty = no
  Discord binding).
- Creation: `session create --gateways discord,terminal` / `--terminal-only` /
  `--no-discord`. Default configurable via `config.json` (`default_gateways`) +
  env; no flag → the configured default.
- Global config: `config.json` lists active gateways and their settings. A
  gateway that is not configured (e.g. no Discord token) is simply absent from
  the hub without failing — terminal-only sessions still run.
- Back-compat: an existing session with no `Gateways` is treated as
  `["discord"]`; an empty `Channel` means no Discord binding.

## Session bus protocol

Extends `internal/control` (today daemon→bridge, one bare value per connection)
into a bidirectional stream of JSON lines over the per-session socket.

bridge → daemon (turn events):
```json
{"t":"human","who":"discord:user#123","text":"refactor the env loader"}
{"t":"chunk","text":"reading envfile.go…"}
{"t":"status","text":"proposing 3 changes"}
{"t":"reply","text":"done · 4 files changed","done":true}
```

daemon → bridge (injected input + control):
```json
{"t":"input","who":"terminal","text":"apply them"}
{"t":"pick","value":"2"}
```

- One line = one message; `bufio` + `encoding/json` (stdlib only).
- The bridge **serializes**: Discord-poll input and socket-injected input enter
  one queue → a single backend, deterministic order.
- The hub reads events and **fans out**: `reply`/`chunk` → every bound gateway
  (Discord `Post`, TUI render). `pick`/`input` go down.
- Back-compat: the legacy bare-value (pick) line is still accepted during
  migration.

## Headless & attach

- `herrscherd serve` → TUI foreground by default (daemon process + in-process
  render).
- `herrscherd serve --headless` → daemon, no UI (systemd/service mode, as today).
  Discord *and* terminal sessions run; no local render.
- `herrscherd attach` → TUI client connecting to a running daemon via a
  **daemon-level socket** (list sessions, subscribe to buses, send input). Phase
  4.
- Quitting the TUI (`q`) does **not** kill the daemon or bridges — it detaches
  the render only.

## TUI layout

- Top bar: instance id, active gateways, plugin count.
- Left rail: sessions as tabs (`●` running / `○` idle), active highlight,
  `＋ new session`.
- Main pane: focused session's structured transcript (`you ▸` / `claude ▸` /
  status), with an input box at the bottom (`❯ type to send…`).
- Status line: keybindings — `j/k` switch, `n` new, `d` close, `enter` send,
  `q` quit (daemon keeps running).

## Testing

Stdlib `testing`, house style (minimal comments):

- Bus protocol: encode/decode round-trip + input serialization order.
- Hub: merge two sources → one queue; fan-out to N mocked sinks; only bound
  gateways receive a session's replies.
- Session model: `Gateways` + back-compat (empty `Channel`, missing `Gateways`).
- Terminal gateway: `Post` → event; input → inbound message (mocked transport).
- Purity: the hub imports no concrete gateway (extend `TestCorePurity`).
- TUI: state-reducer tests (events → view), no pixel rendering.

## Phasing

1. **Hub + session `Gateways`** — core handles N gateways, fan-out, optional
   `Channel`. (Discord-only, no UI — non-regression.)
2. **Structured bus** — bridge emits turn events; refactor bridge into a runner.
3. **Terminal gateway + in-process TUI** — `serve` opens the TUI, terminal-only
   works. ⇐ the requested deliverable.
4. **`attach` + daemon-level socket** — connect to a daemon-service.

## Risks / open points

- Bridge refactor (moving gateway I/O to the hub) touches the supervisor loop;
  keep the subprocess + `cwd=worktree` model, change only what feeds it.
- The terminal gateway lives in the daemon process while bridges are
  subprocesses — the session bus is the link; confirm event volume (chunks) is
  fine over a unix socket (expected: trivial).
- `config.json` schema growth (`default_gateways`, per-gateway settings) must
  stay back-compatible with existing files.
