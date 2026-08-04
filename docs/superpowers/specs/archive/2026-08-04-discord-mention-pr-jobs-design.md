# Discord mention-driven PR jobs — design

**Date:** 2026-08-04
**Status:** Design (approved)
**Repos:** `herrscher-contracts` (one port grows), `herrscher-discord-gateway`
(everything Discord), `herrscher` (thin seam implementation + the editable
playbook skill).

## Goal

Today the Discord edge is **channel-bound**: `DISCORD_CHANNEL_ID` names one
channel, a session is pinned to it, and the core polls that channel through
`ChannelReader.Read`, treating *every* non-bot message as a turn.

We replace that with an **owner-bound, mention-driven** edge:

- Config takes a **Discord user id** (the owner) instead of a channel id.
- The bot listens in every channel it can see, and **reads everyone's messages**
  as context, but only the owner can make it act.
- A turn starts on an **@mention of the bot or a reply to one of its messages**.
  The conversation stays in that channel — no automatic thread; a thread is
  created only when the owner explicitly asks for one.
- The triggering message's **attachments (screenshots) reach the agent**, as they
  already do today.
- An unknown channel gets a **select menu** asking which repo/project to work on;
  the answer is persisted per channel and never asked again.
- The work itself — worktree, fix, review sweep, PR — is **agent-driven**, and
  the procedure lives in an **editable `SKILL.md`**, not in Go.
- When the agent needs a decision, it ends its turn with a pending choice and the
  gateway renders a **select menu**; the click resumes the turn.

**The core stays agnostic.** Not one line of core learns what a mention, an
owner, or a Discord channel is. The entire mechanism lives in the gateway plugin,
behind one neutral port addition.

## Baseline (verified in code, 2026-08-04)

Facts this design is built on, each checked against the source:

- **The websocket reads nothing today.** `ws.go:219` identifies with
  `Intents: 0`, and `ws.go:157` only dispatches `INTERACTION_CREATE`. All inbound
  message traffic is REST polling.
- **The core polls one channel per session.** `core/host/turnloop.go:321-360`:
  `poll` calls `r.Read(ctx, ch, 100, last)` every `pollInterval` (50 ms,
  `turnloop.go:19`) on `d.channel`, and enqueues every non-bot message as an
  `input` event.
- **REST reads are not gated by the message-content intent.** The current system
  reads message content over REST while identifying with `Intents: 0` — that is
  the shipped product, so it is proof, not assumption. The privileged
  `MESSAGE_CONTENT` intent only affects gateway *dispatch* payloads.
- **Attachments already work.** `turnloop.go:140` `resolveAttachments` →
  `bridge.ResolveAttachments` downloads CDN attachments against the session's
  `attachHosts` SSRF allowlist and hands the backend local image paths.
- **The core already routes events per conversation.** `turnloop.go:623-630`
  `fanOut` prefers `contracts.RoutedEventSink.EmitTo(conv, e)` with
  `conv.ID = d.renderChannel(g)`. The Discord gateway does *not* implement it: it
  implements the flat `EventSink`, and its `sink` renders every event to
  `rc.DefaultChannel()` (`sink.go:65`). **The gateway is the mono-channel part,
  not the core.**
- **`contracts.Inbound` is an orphan.** Declared at `gateway.go:16-23` with
  `Conversation`, `Author`, `Text`, `Attachments`, `MessageID` — and consumed
  nowhere. It was designed for exactly this push seam and never wired.
- **`host.Pick` has no caller.** `turnloop.go:273` `Pick(session, value)` exists;
  a repo-wide search finds only tests. Likewise `contracts.MenuRouter` /
  `Platform.RouteMenu` (`adapters.go:187`) and `ParseChoiceCustomID`
  (`choice.go:12`) are implemented but never invoked. The click→session return
  path is **half-built**: the outbound menu exists, the inbound route does not.
- **`SessionControl` is the neutral push seam** (`session_control.go:11-42`):
  `Dispatch`, `Create(CreateSession)`, `Close`, `Sessions`, `Scrollback`,
  `Resume`, `Interrupt`. `CreateSession` already carries `Project`, `Clone`,
  `Agent`, `Backend`, `Gateways`. It has **no way to inject a message** into a
  live session.
- **`session create` always makes a new channel.** `manager/session.go:528-545`
  switches on `home.Type` and calls `admin.CreateUnder` (category) or
  `admin.ForumPost` (forum) — there is no path that adopts an existing
  conversation, and a daemon with no `/set home` cannot create a session at all.
- **Skills are already editable markdown.** `core/skills/discover.go:11` scans
  `<root>/<name>/SKILL.md`; `engine.go:48` exposes `Refresh()`. No rebuild needed
  to change a procedure.
- **`forge` can list repos but not open PRs.** `core/internal/forge/forge.go` has
  `List`, `Clone`; no PR creation. `forge.New()` is built once in
  `core/host/cli.go:48` and handed to the manager. There is no `repo list` CLI
  command.
- **The gateway owns all presentation already.** `turnloop.go:604-609` states it
  explicitly: the host emits abstract semantic events, the gateway draws.

## Architecture

```
Discord                     gateway plugin                    core
─────────────────────────────────────────────────────────────────────────
MESSAGE_CREATE  ──▶  ws.go (intents: GUILD_MESSAGES
                            | DIRECT_MESSAGES)
                       │
                       ▼
                     mention.go — owner? mention-or-reply?
                       │ (everything else dropped here)
                       ▼
                     router.go — channel → session?
                       │                    │ no
                       │ yes                ▼
                       │            select menu (repos)
                       │                    │ click
                       │                    ▼
                       │            ctrl.Create(CreateSession)
                       │            persist channel→session
                       ▼                    │
                     context.go ◀───────────┘
                     (REST: last N messages, all authors)
                       │
                       ▼
                     ctrl.Submit(session, Inbound)  ────▶  sessionDriver.queue
                                                              │
                     sink map (conv → *sink)  ◀────────────── fanOut / EmitTo
                       │
                       ▼
                     progress, ⏳ ack, reply, choice menu
                     in the originating channel

INTERACTION_CREATE ─▶ slash.go
                       ├─ command / autocomplete  → ctrl.Dispatch (unchanged)
                       └─ component (custom_id)   → ctrl.Pick(session, value)
```

### The seam: three methods and one field

This is the **only** contract change, and every added type is platform-neutral.

```go
// Submit injects one inbound message into the named session's turn queue, as if
// the session had read it from its own channel. The host resolves Attachments
// against the session's own SSRF allowlist, so a gateway never downloads.
// Reports false when no live session by that name is driving.
Submit(name string, in Inbound) bool

// Pick answers the named session's pending choice with a select-menu value.
// Reports false when no live session by that name is driving.
Pick(name, value string) bool

// Repos lists the projects a session can be created on: the workspace
// sub-directories already present, plus the remote repositories the configured
// forge can clone. A gateway uses it to offer a choice; contracts never learns
// how either list is obtained.
Repos(ctx context.Context) ([]RepoRef, error)
```

with

```go
// RepoRef is one selectable work target. Local reports whether Name is a
// workspace sub-directory already on disk (create with Project) rather than a
// remote to clone (create with Clone).
type RepoRef struct {
	Name        string
	Description string
	Local       bool
}
```

and one new field on `CreateSession`:

```go
// ChannelID adopts an existing conversation instead of creating one: the new
// session binds to it and posts there. Empty keeps the default behaviour —
// create a channel under the configured home. A gateway that is already talking
// to the user in a conversation uses this so the session lands where the
// conversation already is.
ChannelID string
```

`Submit` is what `Inbound` was declared for. `Pick` finally gives `host.Pick` its
caller. `Repos` is the one genuinely new capability, and it is domain, not
platform — a repository is not a Discord concept. `ChannelID` is an opaque
string the core already stores (`state.Session.ChannelID`,
`contracts.SessionInfo.ChannelID`); adopting one is strictly less work than
creating one.

**Why not reuse `Dispatch`?** `Dispatch(ctx, []string{"session","seed",...})`
exists and injects a turn, but `session seed` carries a `task string` only — it
cannot carry attachments, so screenshots would be dropped. It also returns the
reply synchronously instead of letting the live render path run. `Submit` is the
conversational counterpart.

### Core implementation (thin, and Discord-free)

- `sessionDriver.Submit(in contracts.Inbound)`: `journal(in.Author)`, then
  `resolveAttachments`, then `queue <- Event{T: "input", Who: in.Author, Text:
  in.Text, Attachments: atts}` — the exact three steps `poll` already performs
  (`turnloop.go:345-348`), factored out so both paths share one body.
- Package-level `host.Submit(session string, in contracts.Inbound) bool`,
  mirroring the existing `Pick`/`Seed`/`Interrupt` registry lookups
  (`turnloop.go:271-308`).
- The hub's `SessionControl` implementation forwards `Submit`/`Pick` to those,
  and `Repos` to `forge.Client.List` plus a workspace scan.
- `manager.createSession` gains one branch before the `home.Type` switch: a
  non-empty `spec.ChannelID` adopts that conversation (`Type: "text"`) and skips
  channel creation entirely — so the mention flow works on a daemon that never
  ran `/set home`. The start banner still posts into the adopted channel, so the
  owner sees which repo and worktree the session landed on.
- `poll` is **not deleted**: sessions created the old way (`/session create`,
  the terminal gateway) keep working unchanged. Push and poll coexist because the
  gateway suppresses one of them per channel (below).

Purity is preserved by construction: `TestCorePurity` /
`TestCoreNamesNoConcretePlatform` still see no Discord identifier in core.

### Gateway implementation (everything else)

**1. Config.** `register.go`: drop `{Key: "channel", Env: "DISCORD_CHANNEL_ID"}`,
add `{Key: "owner", Env: "DISCORD_USER_ID", Required: true}`. `dctl.New(token,
"")` — the client keeps its default-channel parameter but the plugin no longer
sets it, so `DefaultChannel()` returns empty and nothing can silently fall back
to a global channel.

**2. Intents and `MESSAGE_CREATE`.** `ws.go` identifies with
`GUILD_MESSAGES (1<<9) | DIRECT_MESSAGES (1<<12)` — both non-privileged — and
dispatches `MESSAGE_CREATE` to a second handler alongside `INTERACTION_CREATE`.
`newWS` grows one field rather than switching on a string at the call site.

**3. The trigger filter** (`mention.go`, new). A message passes when **all** hold:
author id equals the configured owner, author is not a bot, and either the bot's
application id appears in `mentions`, or `message_reference` points at a message
whose author is the bot. Everything else is dropped inside the plugin, before any
allocation crosses the boundary.

This filter and Discord's own behaviour agree by construction: without the
privileged `MESSAGE_CONTENT` intent, Discord populates `content`, `attachments`
and `embeds` only for messages that mention the app (plus DMs and the app's own
messages) — precisely the set the filter keeps. **No privileged intent is
required, and none is requested.**

**4. Channel → session routing** (`router.go` + `discord-router.json`, new). A
JSON store beside `discord-allow.json` under `DCTL_STATE_DIR`, mode 0600, written
through the same `writeAtomic` helper (`allow.go:58`) so a crash mid-write cannot
truncate it.

- **Known channel** → `ctrl.Submit(session, inbound)`.
- **Unknown channel** → buffer the message in memory, call `ctrl.Repos`, post a
  select menu whose `custom_id` encodes "bind this channel", and return. On the
  click: `ctrl.Create(CreateSession{Name: sessionNameFor(channel), ChannelID:
  channel, Project | Clone: choice, Gateways: ["discord"]})` — `ChannelID` is
  what makes the session adopt the conversation instead of opening a new one —
  then persist the binding and submit the buffered message. The buffer holds
  **one** message per channel: a second ping before the answer replaces it, and
  the menu prompt says so.
- **Stale binding** (session closed out of band) → `Submit` returns false; the
  gateway drops the binding and falls back to the unknown-channel path.

Session names derive from the channel id (`ch-<id>`), so they are stable across
restarts and cannot collide with operator-named sessions.

**5. Per-conversation rendering.** `Gateway` gains `EmitTo(conv, e)` and so
satisfies `contracts.RoutedEventSink`; `fanOut` prefers it over `Emit`
(`turnloop.go:624`), so this switch alone makes rendering follow the session's
own channel. The single `*sink` becomes a `map[string]*sink` keyed by
conversation id, created on demand under a mutex; each sink keeps its existing
per-turn state (`pv`, `lastUser`, `acked`) and its `renderClient` is bound to
that channel instead of reading `DefaultChannel()`. `Emit` stays as the
degradation path for a gateway built without routing.

The push path calls `sink.noteUser(id)` for the triggering message, which is what
`Platform.Read` did (`adapters.go:165`) — so the ⏳ ack still lands on the right
message.

**6. The click→session route.** `slash.go:onInteraction` gains a branch before
the registry dispatch: if `ix.Data.CustomID` parses via `ParseChoiceCustomID`,
route it — a repo-binding id to the router, a session id to `ctrl.Pick(session,
value)` — then `Components().Ack` to collapse the menu. This wires the two dead
seams (`ParseChoiceCustomID`, `host.Pick`) into one live path.

**7. Other people's messages as context.** At submit time the gateway fetches the
last N messages of the channel over REST (`Messages().Read`, already used by
`Platform.Read`) and prefixes them to `Inbound.Text` as a plain transcript block
(`<author>: <text>`), newest last, bot messages included so the agent sees its own
prior turns. N is a plugin setting (`context_messages`, default 30). Assembling
context in the gateway is what keeps the core agnostic: the core receives one
opaque `Text`.

**8. No double delivery.** For any channel the router drives by push,
`Platform.Read` returns an empty slice. The core's poller keeps running harmlessly
for that session and keeps working normally for legacy channels. One inbound path
per channel, decided by the plugin, invisible to the core.

### The editable playbook

`skills/pr-job/SKILL.md` in the host repo, discovered by `core/skills` and
reloadable via `Refresh()`. It holds the finishing procedure — review against CI,
architecture decisions, performance, code quality, security, bug review, strip
useless comments, update the docs to match the code, avoid false positives, then
open the PR.

The gateway contributes **one line** to the seed of a freshly created session:
`follow the skill "pr-job"`. The skill name is a plugin setting (`playbook`,
default `pr-job`). Changing what the bot does end-to-end is editing a markdown
file; changing *that it does it* is one setting. No Go, no rebuild, either way.

This is deliberately not a host state machine: the agent already has the worktree
(every session gets one), and `gh` in its own shell. The host contributes the
isolation and the procedure, not the git commands.

## Error handling

- **Unknown/failed repo list** — `ctrl.Repos` error: post the error in-channel and
  drop the buffered message. The next ping retries.
- **`Submit` on a dead session** — returns false; binding dropped, unknown-channel
  path retried once for the same message, so a restart mid-conversation is
  invisible to the owner.
- **Menu click for a vanished session/channel** — `Pick` returns false; the
  gateway acks the interaction with a short "cette session n'est plus active"
  rather than leaving a dead menu spinning.
- **Corrupt router store** — same policy as `allow.go:26-38`: report on stderr and
  start from an empty store. Empty means "ask again", which is safe; it can never
  mean "act on the wrong repo".
- **Websocket close 4013/4014 (bad intents)** — already fatal and non-reconnecting
  (`ws.go:101-108`); the added intents are non-privileged, so this path should
  stay unreachable. If it does trigger, the existing message already tells the
  operator to check the intents.
- **Missing `owner`** — the plugin fails to build its gateway set with a clear
  error, rather than starting a bot that obeys everyone.

## Testing

Gateway (table tests against fakes, matching the existing style):

- `mention_test.go` — the filter accepts owner+mention and owner+reply-to-bot;
  rejects a non-owner mention, an owner message with no mention, a bot author, and
  a reply to a non-bot message.
- `router_test.go` — unknown channel buffers and posts a menu; the click creates,
  persists and submits; a known channel submits directly; a stale binding is
  dropped and re-asked; a second ping replaces the buffered message; a corrupt
  store degrades to empty.
- `sink_test.go` — two conversations render independently: interleaved events do
  not cross channels, and each keeps its own ack/progress state.
- `slash_test.go` — a component interaction routes to `Pick` and never reaches the
  command registry.
- `ws_test.go` — `MESSAGE_CREATE` reaches the message handler; the identify
  payload carries the two expected intents and no privileged bit.

Core:

- `turnloop_test.go` — `Submit` enqueues an input frame with resolved attachments;
  `Submit` on an unknown session returns false; the shared body is exercised from
  both the poll and the submit path.
- `handler_test.go` — creating with `ChannelID` set adopts it, never calls
  `CreateUnder`/`ForumPost`, and succeeds with no `home` configured; creating
  without it keeps the current behaviour exactly.
- Existing purity tests are the guard for "core stays agnostic" and must keep
  passing untouched.

## Sequencing

Three shippable slices, in order — each leaves the tree green and the existing
channel-bound flow working:

1. **Seam** — `contracts` grows `Submit`/`Pick`/`Repos` + `RepoRef` +
   `CreateSession.ChannelID`; core implements them (including the adopt branch in
   `manager.createSession`); nothing calls them yet. Tag `herrscher-contracts`.
2. **Rendering** — the gateway implements `RoutedEventSink` and its sink becomes
   per-conversation. Independently useful: it un-pins rendering from the global
   default channel.
3. **Mention flow** — intents, `MESSAGE_CREATE`, filter, router, context, click
   route, config swap, playbook skill. This is the slice that changes behaviour.

## Out of scope

- **Buttons and modals.** Select menus only, as decided: `dctl/components.go`
  builds a select and nothing else, and adding buttons would touch dctl, the
  contracts capability set, and the gateway. Revisit if yes/no questions get
  tedious.
- **Host-side PR creation.** `forge` gains nothing; the agent runs `gh`.
- **Live streaming of other people's messages.** Context is pulled on demand at
  turn start. Streaming would need the privileged `MESSAGE_CONTENT` intent and a
  per-channel buffer in the plugin.
- **Automatic threads.** The conversation stays in the channel; a thread is
  created only on explicit request, which is an ordinary agent action, not a
  gateway feature.
