# The terminal UI

Discord, the in-tree terminal UI, or your own gateway plugin all sit behind the
same neutral port. The UI is the one that ships in the binary.


## The shape of the window

A rail across the top opens with the name `herrscher`, then lists the open sessions: the active
one is marked `▸` and underlined, a working one shows a spinner, one waiting for
approval shows `!`, a finished one `✓`, and an unread one `●` with its count of new
entries. The hints `^P agents · ^T onglet` sit at the right, and a rule runs under the
row. When the sessions do not fit, names shorten first, then the rest folds into a
`+N` count, and the active session always stays visible.

At 110 columns and wider a side column runs down the right of the transcript,
separated by a vertical rule. It shows the session (project, branch, worktree,
gateways), the todos with their `n/m` count, the sub-agents still running and those
just finished, background tasks when the backend reports them, the files the session wrote with their `+added −removed` line counts
against git, and the context gauge (24 cells) with the compaction threshold (92 %). `ctrl+x v` hides or
shows it. Under 24 rows the bottom sections fold to their summary line. The
transcript keeps the full width below 110 columns, and the todos and sub-agents
panels stay in the stack above the composer whenever the column is hidden.

The composer sits in a rounded box at the foot, and the status line is glued right
under that box, so the two things you act on are one object rather than two rows
separated by a rule.

`/resume` filters as you type (a fuzzy match on name, project, vendor and gateway)
and shows the last lines of the selected session above the list.

Every list the window opens (the `/` palette, `@` completion, `/resume`,
`/session switch`, `/skills`) shares one grammar: unbordered rows floating just
above the box, the selected one on a filled background, a footer that says what
the keys do and how much of the list is out of sight. The box is drawn with
rounded runes where the locale says UTF-8, plain ASCII otherwise, and it collapses
below 60 columns.

The window speaks French.

## Sessions as tabs

Several sessions render as live tabs. Each answer opens on a `●` gutter and each
message you send on a `┃` bar, so a turn keeps its shape when colour does not
survive, and nothing is framed, since a frame drawn around every turn marks none
of them.

Events are typed by role (reasoning, tool family, notice, error). The agent's
markdown and diffs are rendered, a resumed session's history is replayed as the
turns it was. The status bar leads with the mode as an inverse chip (Manuel, Édits
acceptés, Plan, Auto in red), then names what the session runs on (vendor, model,
reasoning effort), its project and branch, and how much room is left before the
backend compacts the conversation: a gauge that turns amber at 80 % and red at 95 %,
with the figures `41%  82k/200k`. Cost (`$0.00` until the first turn) and queued messages follow, and the fields
drop by priority as the window narrows. `/usage` spells the numbers out.

`/statusline` opens the bar's settings: tick the fields, reorder them with shift+↑↓,
and see the result in a live preview. Beside the list, a pane shows the script
contract: the JSON it receives on stdin and the `[statusline]` keys. `s` switches
between the built-in bar and the script. In `~/.config/herrscher/terminal.toml`, a
`[statusline]` section can replace the bar with a script (`source = "script"`,
`command`, `refresh`): it receives the state as JSON on stdin and its first line
becomes the bar. A script that fails or runs past one second leaves the built-in bar
with a discreet ✗.

A session has one of four modes, cycled with shift+tab or set with `/mode
[manuel|edits|plan|auto]`. The strip under the plan box shows them with the
current one highlighted, a left click on a segment switches to it, and the status
bar opens with the mode. A tab starts from the mode the session has stored
(`session get-mode`). The TUI sends the change to the daemon through
`session set-mode`, so the host enforces it: manual asks before each edit and
command, accept edits lets edits through and still asks for commands, plan is
read only and refuses writes, and auto lets everything through except what a
deny rule refuses. A refused switch is shown and the mode stays as it was. A plan reply
opens a three-choice menu: validate and switch to accept edits, validate in
manual, or keep planning. `/plan <instruction>` switches to plan and sends the
instruction.

A session's name is chosen when you know the least about what it will become, so
`/rename <nom>` changes it: the row keeps its identity and its id, the transcript
and the participants journal follow, any schedule that targets the session by
name follows too, and the bridge restarts under the new name. A session an agent
schedule owns carries a name derived from the schedule's rather than a stored
one, so renaming it says so: the next window opens a fresh session under the old
name.

The `/` palette is derived from the daemon's own registry, via `commands --json`
filtered to the verbs a tab may run, so the menu cannot fall behind what the
daemon dispatches. It scrolls rather than truncating, and its height follows the
window's, so a tall terminal shows a menu rather than a slit. Arrows walk it,
`⇞`/`⇟` page through it and `début`/`fin` jump to its ends, in the palette and in
every other floating list.

An attached window follows the live turn, because every driven session taps the
daemon's event socket. Esc stops a turn as an interruption rather than an error,
keeping whatever the agent had already written.

## Health in the window

`/health` opens the same snapshot as an overlay, and the status line carries a dot
for the worst state of the checks with `santé n/m` beside it, where `n` counts the
checks that are ok and `m` the ones that can fail. The figures refresh every fifteen
seconds with a plain probe, overlay open or closed; a deep probe only runs on `d`. Inside
the overlay `r` probes again, `d` runs one deep probe (it costs round trips) and the
refresh after it is a plain probe again, `enter` shows the cause and remedy of the
degraded and down checks, and `esc` closes it. The overlay is also in the command
palette.

## Context, and what fills it

`/context` opens the window's room: the total used against the session's window,
and a line for each part that fills it (system prompt, host blocks, tools and
MCP servers, memory, skills, files read, conversation), the share of the last
prompt read from the prompt cache, and what is free. The total comes from the
backend, which reports one count of input tokens per turn, not a breakdown. The
parts the host writes itself are sized by the host at the end of each turn:
`memory` (the recall block), `skills` (the menu and the loaded skills) and
`host` (the delegation, capabilities and identity blocks, and any block a newer
host adds). Their figures start with `~`: they are estimates at about four bytes
per token, not counts from the vendor. Every turn reply sizes all of them, so a
part the host left empty on the last turn reads `0`. A reply that carries no
sizes, such as a posted notice or a reply from a daemon that does not measure
them, leaves the last figures in place. The other parts read `n/a` until a
backend reports them, and so does every host part before the first measured
turn. Without any count yet, the total reads `n/a` too, and no percentage is
invented. Below 80 columns the bars go and the overlay becomes a plain list.
`esc` or `z` closes it, `k` re-reads the figures.

`/compact`, or `c` inside `/context`, asks the session to compact its conversation
through the host (`herrscher session compact --name <name> [--instruction <text>]`).
The compaction goes through the session's stack, queued after a turn in progress.
A session whose backend does not declare `Capabilities.Compactor` is refused before
anything is sent. The transcript then records the window before and after, as the
backend reports it, and the gauge follows the new figure. A compaction the backend
does not report on reads `n/a` after.

Automatic compaction runs when the last measured usage reaches the session's
threshold, a percentage of its context window. The default is 92, set with
`compactAt` in `config.json` (0 disables it, values above 100 or below 0 are
refused at load). Each session can override it with
`herrscher session set-compact --name <name> --percent <percent>` (an empty percent
restores the default, 0 disables it for that session). The window size comes from
the session's model and vendor, in this order: `HERRSCHER_CONTEXT_LIMIT` when it is a positive number,
then the `contextLimits` table of `config.json` (a vendor name to a window in tokens,
for example `{"contextLimits": {"codex": 400000}}`), then the window the resolved model declares (its `contextWindow`, when it is positive),
then the built-in size of 200000 for `claude`, `cursor`, `codex` and `antigravity` (256000 for `grok`), for a model that
declares none. `HERRSCHER_CONTEXT_LIMIT` and `contextLimits` are settings you make on
purpose, so they take precedence over the window the catalogue declares for the model.
Vendor names are case-insensitive. `config.json`
refuses a zero or negative size, an empty vendor name and one vendor listed twice in
two spellings. `herrscher serve` warns once at start about a vendor that no backend of
the daemon serves; its entry is then unused. The built-in `codex` figure is a cautious
assumption, the smallest window verified in the Codex backend catalogue (`o4-mini`),
so automatic compaction runs early rather than after the provider refuses the request;
raise it in `contextLimits` when the model in use takes more. The daemon is the only
source of the window it compacts against: each session carries the window the daemon
resolved for its model and vendor (`SessionInfo.ContextWindow`), and the TUI gauge, sidebar,
status bar and statusline read that figure. The TUI resolves the same table itself
only as a fallback, when the daemon gives no size (a daemon older than this field),
from the `config.json` of its own process and its own environment. The two can differ
when `herrscher serve -config <file>` reads another file, or when the TUI runs under an
environment that the daemon does not share; the daemon's figure is the one that counts.
An invalid `config.json` makes `herrscher serve` refuse to start, while the TUI shows a
flash naming the file and falls back to the built-in sizes, or to the
`HERRSCHER_CONTEXT_LIMIT` override when it is set. After a measured compaction still above the threshold, automatic compaction does not retrigger: it rearms once a
measured turn lands under the threshold. A compaction the backend does not measure
never retriggers. `compactStack` lists the stages in order, where `backend` is the
session's own compaction and every other name is the kind of a plugin of the
`compactor` category, built once at start through its `Compactor` factory.

A plugin stage can ask to run before the backend's threshold. A `Compactor` that
also implements `StageTrigger` returns from `TriggerPercent()` the share of the
window, in percent, from which it runs. Zero, or a `Compactor` without the
interface, means the host's `compactAt`, and so does a value above it: that value
does not delay the stage. The host checks the value at start with
`ValidateStageTriggerPercent`, and a value outside 0 to 100 makes `herrscher serve`
refuse to start, naming the stage and the value. The `backend` stage has no
threshold of its own and runs at `compactAt` only. When `compactAt` is 0, automatic
compaction is off and no stage turns it back on: the stages run only on `/compact`.

When the usage reaches the lowest effective threshold, the host runs automatically
only the stages whose effective threshold is reached, in the order of the stack. A
stage's effective threshold is its own trigger when that is above zero and below
`compactAt`, and `compactAt` otherwise. Once `compactAt` is reached, the whole stack
runs, as before. If the early stages leave the usage at or above `compactAt`, the
rest of the stack runs in the same turn, after them, without the stages that just ran.
`/compact` always runs the whole stack.

A stage that runs early runs once per crossing of its threshold, the anti-loop rule:
it does not run again while the measured usage stays at or above its threshold,
since each run costs requests. It rearms when a measured usage lands under its
threshold, which a compaction of the host or a new session does. The state is kept
in memory for each session and is not persisted: a daemon restart rearms every
stage, so an early stage runs again at the first measured turn over its threshold.

For example, with `"compactStack": ["jev", "backend"]` and `compactAt` at 80, a `jev`
stage whose `TriggerPercent()` returns 50 prunes the tool calls once the window
reaches half full, and the backend summarises at 80. When the pruning brings the
usage under 50, the stage rearms.

The `jev` stage, from [herrscher-jev-compactor], prunes the tool calls the
assistant no longer needs before the backend summarises what is left. List it
ahead of the backend:

```json
{ "compactStack": ["jev", "backend"] }
```

It needs `TYPESAFE_API_KEY` in the environment of `herrscher serve`; the optional
`JEV_*` settings of that plugin tune it. A required setting that is missing makes
`herrscher serve` refuse to start with a message naming the variable, there is no
fallback that would drop the stage quietly. A daemon whose `compactStack` does not
name `jev` never builds it and starts without the key. The stage edits the history
only for a session whose backend declares `Capabilities.HistoryEditor`: the host
then hands plugin stages a history editor that reads and prunes the conversation
through the bridge, and the `backend` stage never receives one. The `claude`
backend declares it from herrscher-claude-backend v0.12.0 and, from v0.14.0, the
version this host builds with, keeps the tail a `truncate_tail` edit asks for. On
another backend the stage gets no editor and leaves the history as
it is. The bridge sends the history in frames of at most 128 KiB, and the host
refuses a history past 64 MiB. A read or a prune that the bridge does not answer
within two minutes fails the stage. The host sends the edits of a prune in
batches of 1000, each one applied on its own: when a later batch fails, the
batches before it stay applied. When Jev itself fails, the stage leaves a note
and the `backend` stage still runs. A backend that declares
`Capabilities.HistoryEditor` while its running instance does not implement the
port fails the stage, and the host writes one warning line to stderr the first
time it happens.

The stage reports its stats to the usage ledger: [herrscher-jev-compactor] fills
them from v0.3.0, so a `jev` compaction appears in `herrscher usage` and in
`herrscher health`. From v0.4.0, the version this host builds with, the stage
implements `StageTrigger`: when `compactAt` is set and its `trigger_percent` is
below it, the prune runs alone from that share of the window, earlier than
`compactAt`, at which the whole stack runs. Its stats also carry the size of the
state the stage sent to Jev and the reduction that made it fit; the host passes them through but shows them in neither
`herrscher usage` nor `herrscher health`.

When a backend declares `Capabilities.Compactor` but its running instance does not
implement the port, the host writes one warning line to stderr the first time that
instance answers without the port, and each compaction of that session is then
refused with the same error. The host does not repair the instance. A `codex`
session in one-shot mode is such an instance: `CODEX_STREAM=false`, or
`HERRSCHER_CODEX_ONESHOT_WINDOWS=1` on Windows, runs the one-shot responder, which
has no port.

## Notifications

When a turn finishes, and when an approval waits for you, a toast appears at the
top right of the window and fades after a few seconds. It is a bordered box: the
title with the age of the event, the session with its vendor and gateways (and the
duration of the turn), the approval question when one waits, and the way out, which
is `ctrl+p` to the session. The toast only signals: it never answers an approval or
opens anything.

The window also tells the terminal, but only when the window lacks focus. The
terminal gets an OSC 9 notification (`ESC ] 9 ; text BEL`) where it is known to
handle one (iTerm2, WezTerm, ghostty), and a plain BEL everywhere else. A
terminal that reports focus lets the window know when it is in the background; a
terminal that does not report focus is signalled on every event, since nothing
says whether the window is looked at. The BEL fallback is a heuristic on the
terminal's name, not a check that the terminal rang.

`n` (with an empty composer) or `/notify` cycles which events notify: turn end
and approval, turn end only, or approval only.

## Rewinding

Before each message sent, the host takes a checkpoint of the session's worktree (a
git snapshot under `refs/herrscher/checkpoints/`, without touching the index or the
files). On an empty composer and outside a turn, `Esc Esc` opens the "Rewind"
screen: the list of messages with the files each one touched, side by side when
wide (under 80 columns, the files move below the message).

From this screen, `Enter` opens the choices: "restore the code and the conversation",
"restore the conversation only" and "restore the code only", each with a
confirmation (`y`). The conversation returns to the message by relaunching the
backend on a copy of its transcript, cut just before that message, under a new
session identifier: the original stays intact. Only the Claude backend provides this
position today; elsewhere both conversation options are disabled with the reason
shown, and nothing changes. Restoring the code alone does not touch the
conversation. `u` undoes the last code restore (one level); a conversation restore
cannot be undone yet. `f`, or "fork from here", runs `/fork`.

`/fork [--name <name>] --to <n>` creates the session `<name>-fork` with its own
worktree at the checkpoint, resumes the conversation up to that same message, and
opens a new tab; the original stays intact. A fork refused by the backend (one with
no per-message position) creates nothing.

A single `Esc` always interrupts a turn in progress, as above. Files ignored by git
are not covered by checkpoints.

## What the agent is doing right now

A todo list and a running sub-agent are state, not transcript: written into the
flow they would scroll away from the answer they describe, and rewritten on every
update they would say the same thing a dozen times. So the backend lifts them out
of the stream (`TodoWrite` and `Task` never reach the transcript) and they render
as two panels under it: the list windowed around the task in hand with its
`done/total` count, and one line per live sub-agent with its kind and its age. A
sub-agent leaves the panel when it hands back, and the end of a turn clears both,
keeping an unfinished list. The sub-agents of a turn also hang under the `Task`
that started them as a tree (`├`, `└`, `│`), each with a spinner while it runs, a
`✓` when it hands back, a `✗` when it fails, and `délai dépassé, relancé` when it
timed out and was run again.

→ `plugins/terminal/tui/panels.go`

Picking up a chat session's tab and typing there answers you *there*. A turn says
where it was said, so it is rendered in the window that asked and not posted a
second time in the channel the conversation came from, where nobody asked
anything.

## Themes and look

Four themes ship in the binary: Sombre (the default), Clair, Terminal, which
takes the colours of your own terminal palette, and Contraste élevé. `/theme`,
or `ctrl+x t` from anywhere in the window, opens the theme screen, which takes
the whole window. Moving through the list previews each theme on the spot, and
Entrée keeps the one you chose and the screen stays open, with the line
"enregistré à l’instant" beside the `terminal.toml` block. The current theme
carries a ✓. The same screen
holds the look under RENDU: `g` cycles the glyph set (unicode, nerd, ascii),
`b` the borders (rounded, square, none), `d` the density (normal, compact), `w`
bold and `i` italic. `a` turns the automatic theme on or off, which follows the
terminal's background, and `e` or `ctrl+e` opens `themes/perso.toml` in
`$EDITOR`. Échap closes the screen and puts back the theme it last kept.

The style follows Obel Mono: the accent is #FF3C05 on Sombre and a darker
#C42D00 on Clair, which keeps 5.64:1 contrast on white. Progress bars are 16
`━` cells, filled in the accent, and the spinner turns through braille frames.

The screen also has a police row. The TUI cannot change the font of the
terminal, so the row only recommends Obel Mono and reports whether it is
installed (`fc-list` on Linux, `~/Library/Fonts` on macOS), absent, or unknown.
`f` opens the INSTALLER OBEL MONO block: the install command, which `c` copies,
and the configuration for the terminal it detects, which `y` copies. The TUI
never writes that configuration and never runs the install script. Obel Mono
Masked is the family for kitty and Alacritty. Alacritty's `[font]` section has
no OpenType feature option, so it can only select the family. kitty's
`font_features` is indexed by the font's PostScript name, which this variable
font cannot guarantee, so kitty also gets the family. WezTerm (`harfbuzz_features`)
and foot (`fontfeatures`) select Obel Mono with `ss01`. The Obel Mono page
says of that feature: "Keys show as dots on screen; the file and your clipboard
keep every character." So `ss01` masks API keys on screen only, and the
configuration copied by `y` keeps them intact. Échap closes the block first.

The preview sits beside the list from 100 columns, and under it below that. When
the window is too short for it, the preview is left out so the controls stay on
screen. `NO_COLOR` strips the colours and keeps the rest.

The choice is stored in `terminal.toml`, beside `config.json`:

```toml
[terminal]
theme = "sombre"
auto_theme = false
glyphs = "unicode"
borders = "rounded"
density = "normal"
bold = true
italic = true

[statusline]
source = "builtin"
```

Every writer rewrites only its own keys: the theme picker and `/statusline` share
the file, and the other's settings and your comments survive either one.

The personal theme is `~/.config/herrscher/themes/perso.toml`. The list always
shows it as Mon thème, even before the file exists, and `e` or `ctrl+e` creates
it from a template when it is missing. Keeping Mon thème while its file is absent
is refused, with a message naming the file. Any other TOML file in the `themes`
directory also shows as a theme, named after its id. A theme inherits from a
built-in with `base` and overrides only the colours and render options it names.
The directory is polled, so an edit shows without a restart, and a file that does
not parse is reported on the screen rather than dropped silently.

```toml
name = "Mon thème"
base = "sombre"

[colors]
accent = "#ff8c42"
err = "ansi:1"

[render]
borders = "square"
```

→ `plugins/terminal/tui/theme.go`, `plugins/terminal/tui/theme_catalog.go`

## Approvals

When the agent asks to run something the host does not allow on its own, the
gateway's menu opens an approval box in the session tab, above the composer. It
names the tool and the command (a long command is cut in the middle with `…`),
the project and the worktree, and offers the choices the host sent: `o` allows
once, `s` allows for the session, `t` always allows (the box shows the exact rule
the host will write), and `r` refuses with a reason. Arrows and Enter pick a row
too. Letters only answer while the composer is empty, so typing is never taken
for a choice.

The reason field takes whatever you type; Enter sends the refusal with it, and
an empty reason is a plain refusal. Each answer goes back to the host as the
menu value, so the host decides; the TUI only forwards it.

The box counts down to a refusal. Its deadline is the one the host announces
(an `approval` event), or five minutes when the host gives none; `x` jumps the
countdown to 0:05, and it turns red under a minute. At zero the approval is
refused without a reason, as if nobody answered.

The box is drawn from 60 to 240 columns: below 80 each choice takes two rows.
The tab carries a `pendingApproval` flag for the rest of the window to mark it.

On the command socket, each session's approval hook answers with the token the
daemon issued to that session at launch, sent in the `HERRSCHER_SESSION_TOKEN`
variable. The token ends with the session's run. Closing or archiving the
session revokes it, and so does a session that stops inside the teardown window,
so a session recreated under the same name gets a new one. A one-shot seed gets
its own token instead, which answers only while that seed runs. A seed on an
archived session is refused, and no token is issued to a seed whose session is
archived or gone. A session archived or closed while its seed runs ends that
seed's token. A hook that has no approval socket and holds no token, or cannot
reach a daemon, refuses the tool call and says why: it never allows a call it
could not check. A seed run inside the daemon sets the approval socket for its
backend, so its hook needs no token at all.

Two paths run without a daemon. A `session create` run locally starts a session
whose children hold no token the daemon issued, so every tool call they make is
refused by the hook, while text answers still work (`--print` runs this way). A
`session seed` run locally has no token either, so its tool calls are refused
too. `approve ask` refuses without a daemon. A seed on a remote backend with a session
runs: its approvals travel back to the daemon over the approval stream (see
[Trust model of remote sessions](remote.md#trust-model-of-remote-sessions)).

The bridge process (`herrscher bridge --hub-socket`) runs its backend outside the
daemon, and that backend needs approvals too, so it asks them over the hub
connection it already holds. Every turn it runs and every compaction it makes sets an approval
asker in the context of that call, so the backend receives one on each `Respond`
and each `Compact`. The asker sends a hub event `approval_ask` whose value is the
JSON of the request (`contracts.ApprovalRequest`), and waits for an
`approval_answer` event whose value is the JSON of the answer
(`contracts.ApprovalAnswer`), matched by the request's ID. The daemon answers
through the approval desk for the session bound to that connection when the bridge
was opened. The session comes from the connection, never from a field of the
request, so a bridge cannot ask on behalf of another session. An answer that
cannot be sent, a hub that is gone, or a turn that ends before the answer arrives
refuses the tool call, and the reason is written in the language the hub announced
(`approval.unavailable` in `core/bridge/locales`). The bridge also refuses when no
answer comes within its own bound, `host.BridgeApprovalWait()`, which the binary
passes in `Options.ApprovalWait` (`approval.no_answer`): the hook wait
(`agent.HookWait`, 900 s, after which the CLI cuts the hook and runs the tool call)
minus 60 s, so 840 s. The daemon's approval desk answers that question before the
bridge gives up: it bounds the question at 30 s less, 810 s (`bridgeDeskBound()`).
At 810 s the desk refuses, leaves the pending approvals, and an answer that comes
later records nothing, not even a session rule. On the approval socket and on the
daemon token path, and for a one-shot seed run, the hook has its own bound,
`hookAnswerWait()`, the hook wait minus 30 s, so 870 s: a hook that gets no answer
in time denies the call with `hook.approval_timeout`. The desk bounds those
questions at 30 s less, 840 s (`hookDeskBound()`), with the same removal and the
same rule for a late answer. The order is therefore desk (810 s on the bridge path,
840 s on the hook path), then bridge (840 s), then hook (870 s), then CLI (900 s):
every refusal a consumer receives is the desk's own, and the CLI never cuts a hook
that is still waiting. The cap applies to every session whatever its backend: a
codex session has no hook, but its wait is capped at the same bounds. An approval
wait configured longer than the bound of the call that asks it is truncated to that
bound: the question is cut at the bound, and the daemon's warnings name the
configured wait and the bound it is cut at (`approve.wait_truncated`). A wait equal
to its bound is not truncated and says nothing. A request whose ID is
already waiting is refused at once (`approval.duplicate`), without a second request to
the operator. The daemon answers every request that carries an ID: one it cannot decode
is refused under that ID (`approval.request_unreadable`), and a session without an
approval desk is refused with `approval.no_desk`. A request with no readable ID cannot
be answered, so the bridge's wait for it ends at its own bound.

An answer that picks `session` or `always` and is accepted before the question's
deadline is kept even when the call it targeted has ended in the meantime: the turn
finished, or the bridge closed, before the desk recorded it. The operator chose the
rule in time, so the rule is recorded and is not rolled back when its call ends.

A one-shot seed run by the daemon fails at once with `the seed runtime has no
approval desk` when it has no desk, instead of running tools that nobody could
approve. The error is translated like the other host faults. A `session handoff`
run by the daemon writes its brief through the same daemon runtime, so its brief
backend gets the session's approval asker, and it fails the same way without a
desk.

## Using the terminal you are in

Capabilities are probed once at startup, and every feature that has a richer and
a plainer form reads that one answer.

Images render through the kitty graphics protocol where it exists, sixel where it
does not, and Unicode half-blocks everywhere else, so a picture is shown on any
terminal that can draw text. PNG, JPEG, GIF and WebP all decode, and an oversized
one is downscaled rather than dropped. Links the transcript already contained
become real OSC 8 hyperlinks where the terminal has them, and styled text where
it does not.

Degradation is silent at the point of use: nothing announces that a protocol is
missing, it simply draws the next best thing. `/capabilities` says what is live,
what is not, and what each unavailable feature falls back to.

## Nothing opens on its own

A link is followed because the operator asked, never because a message arrived or
a turn completed. A notification does not open a link either: it only signals.

`ctrl+x l` walks the links in the transcript. While one is selected the status line
shows its *resolved target*, since a markdown label can name one destination and
point at another, and `ctrl+x o` opens it: a URL through the desktop handler, a
`file.go:42` reference through `$EDITOR` at that line. That is display, not a
confirmation prompt. The keypress is the consent. With no link selected, `ctrl+x o`
folds or unfolds the latest reasoning block or tool output instead.

## Sessions board and panes

`ctrl+p` (or `/agents`) opens the sessions board: every session, terminal and
other gateways alike, grouped by state (Au travail, Attend une réponse, À relire,
Fini). Each session takes two lines: its state glyph, name, vendor, gateway and
project, then its age, cost, context gauge and last activity. Its running sub-agents
hang under it, one line each with name, vendor and task (three at most, then a
count); a session without a tab in this terminal shows none. Letters filter the
rows, `↑↓` choose, `Enter` opens or switches to the session, and `Esc` clears the
filter before it closes the board. Below 100 columns the board is the list alone.

From 100 columns a Résumé panel sits beside the list. It shows the vendors and
gateways in use, the memory and skills (and how many wait for approval), the
number of live schedules with the next window, and the approval policy (rules and
how many refuse). Each value is read from the host when the board opens and
refreshed while it stays open; a value the host does not answer for reads `n/a`.
A session whose last answer finished while nobody was looking is listed under À
relire until it is opened or stays on screen.

Inside the board, `ctrl+p` switches to the panes: up to four live sessions side
by side, each in a box whose title carries its state glyph, name and vendor, whose
body ends with the last lines of its transcript, and whose foot shows the context
gauge, cost and gateway. The grid is two by two from 120 columns and 30 rows, two
panes side by side from 80 columns, and a single pane moved with `←` `→` below
80. `Tab` changes the focused pane, `ctrl+w` zooms it, and `Enter` sends the
composer text to it. A click on a pane focuses it. When a pane shows an approval,
`o` (once), `s` (session) or `r` (refuse) answers it without leaving the pane.
`ctrl+p` returns to the chat.

## Reading a long session

`ctrl+f` searches the transcript as you type, marks every match, cycles them with
`ctrl+n` and `ctrl+p`, and puts the scroll position back where it was when you
close it.

`ctrl+x k` and `ctrl+x j` jump by turn. `ctrl+x s` opens the session picker, `ctrl+x f`
collapses the history above the current exchange to one line per turn, `alt+c`
folds long code blocks to a summary line, and `ctrl+x c` copies the last one to the
clipboard as raw text.

Each tool call is one line: its family verb in bold, its argument in grey, a
summary, its duration, and on the right `✓`, `✗`, or a spinner while it runs. Its
results hang under it behind `└`, and a click or `alt+e` folds them. Reasoning is a
`▸` block headed `Réflexion · 14s · 1.2k tokens`, in italics, and unfolds to `▾`.
While a turn runs, the transcript ends on a busy line: `En cours…`, the elapsed
time, the tokens sent, the cost, and `Échap interrompre` on the right.

`ctrl+r` searches the prompts you sent, newest first, with the same fuzzy
matching as the command palette: type a fragment, `⏎` recalls the match into the
composer, `esc` closes it.

## Writing while the agent works

The composer stays open while a turn runs. `⏎` queues the message to go out when
the turn ends. `⌥⏎` queues it as a steer: it goes out right after the next tool
call, so the agent can change course without waiting for the whole turn. `esc`
interrupts the turn and keeps the queue, and `⌃⏎` sends the queue now (it
interrupts a running turn first). Queued rows sit above the composer with `✕
retirer` on the right. Remove one by clicking its `✕`, or press `↑` on an empty
composer to focus the queue, then `suppr` on the row; `esc` leaves the focus.

`/queue` lists the queue as a transcript notice.

`ctrl+s` parks the current draft on a stack and clears the composer; `ctrl+s` on
an empty composer brings the last parked draft back. The line above the composer
names the drafts waiting.

`ctrl+x ctrl+e` opens the draft in `$EDITOR` (then `$VISUAL`, then `vi`). The
composer is framed in the accent colour while the editor is open, and the saved
text comes back into it when the editor exits. A terminal that reports `ctrl+⏎`
through the enhanced keyboard protocol sends the queue directly; the chord
`ctrl+x ⏎` does the same everywhere else.

`!command` runs a shell command in the session's directory through the host's
approval policy, the same one that gates the agent's own Bash calls. The output
is shown in the transcript and enters the session as context on the next message,
so the agent sees what you ran. The composer frame turns amber while the line
starts with `!`.

`@` completes a file from the worktree and `/` completes a command. Both are
fuzzy: the characters you type are highlighted in the match, and command rows show
the PR that delivers them on the right.

Tool lines carry a budget of two rows apiece. Unbounded, a compound shell command
becomes a paragraph in a single colour, and the session path it opens with is
identical on every call. So a continuation hangs under the target it belongs to,
an absolute path deep enough to cost a row is said in the two segments that
identify it, and what is left over is counted rather than dropped: `+3 ↵`.
`alt+e` lifts the budget and spells every command out as it was run.

Edits show an inline diff with a line number column for the old file and one for
the new, so an insertion shifts the numbers below it and each row says where it sits
in both. Each hunk opens with an `@@` separator, and additions and deletions carry
their own background, falling back to the basic ANSI pair on a 16-colour terminal,
where a hex green would be approximated away. Under 70 columns the diff keeps a
single number column. A hunk is worth four lines before it folds to
`▶ n lignes de plus (alt+e)`, since a fifty-line diff quoted mid-answer buries the
sentence that follows it; `alt+e`, the same key that spells a command out, unfolds
it. Tables truncate their widest column instead of wrapping into
confetti.

→ [Keys](#keys)

## Reviewing what a turn changed

`ctrl+x d` opens the diff panel over the window. It lists the files the session
touched, with their status (M, A, D), their counts and a reviewed mark, and shows
the selected file as a two-column numbered diff. Scope is the current turn by
default, or the branch against its base when you press `b`. Untracked files count,
and the snapshot runs against a temporary index, so your real index and worktree
are left as they were.

Inside the panel, `space` marks a file reviewed, `i` sends a note to the agent about
the selected file (the message starts with its path), `c` opens the commit message
generated from the changes so you can edit it, `enter` opens the file in `$EDITOR`,
and `p` commits, pushes the session branch to its origin and opens the pull
request through `herrscher session pr open`, the same command as on the command
line. The number is then recorded in the session, so the sidebar, the `/usage`
overlay and the `/resume` picker show it as `PR #42 open`. The panel shows the
number it got back, next to the commit SHA. Before it commits anything, the panel
checks GitHub on this host, and it refuses with nothing committed or pushed, each
cause with its own message: no GitHub connection, which names `herrscher connect
github` as the remedy; the rate limit reached, which gives the time of the reset when
GitHub reported one; GitHub unreachable from this host. A saved token GitHub has
refused, and a repository the connection cannot read, have their own messages too.
A channel without a session cannot open a pull request, and the panel refuses that
before it commits. When GitHub does not answer in time, the notice names the commit
and says to run `session pr status` to see whether the pull request exists, since it
may have been opened. `/review` opens the same panel in
review mode. Git calls have a timeout and run in the session's folder. The
GitHub check, the commit, the push and the pull request share one budget of 4 minutes
15 seconds, the GitHub check included. If herrscher quits while they run, the wait
ends too.

When the pull request of a session is merged or closed, the session shows a notice
once, which names `herrscher session end --name <session>`. Nothing closes on its
own: the command is run by hand.

## Getting text back out

The window runs on the alternate screen with the mouse captured. That is what
lets the wheel scroll a transcript the terminal keeps no scrollback for, and it
is also why click-drag selection is unavailable exactly where the interesting
text is. So there are two ways out.

`alt+y` puts the last answer on the clipboard, and `/copy [reply|turn|all]` takes
the answer, the whole exchange that produced it, or the tab's transcript. What
was said only, never this window's tool lines, reasoning, costs or notices, which
are noise in a paste.

`ctrl+x m` (or `/mouse`) hands the mouse back to the terminal so its own selection
works as always. The status bar says `mouse → terminal` while it is released,
`pgup` and `pgdn` still scroll, and `ctrl+x m` takes it back.

## Numbers that mean what they say

The context gauge measures the live prompt, read from the counters that arrive
mid-turn. The reply that ends a turn carries the vendor's *billing* totals for
it, which is what the cost line and the budget need, and what the window would
otherwise have shown as an impossible `1252.2k/200k · 100%`. A window opened on a
session that was already running starts from the last measurement the host
recorded, rather than from nothing until the next turn.

The percentage beside the gauge counts down rather than up: it is what remains
before a compaction, which is the question one actually asks of a context gauge. When
automatic compaction is off for a session the gauge reads `pas de compaction auto`
instead.

## Keys

Every key of the terminal is one table: a context, an action id and the chords
that run it. The defaults are Claude Code's own (same context names, action ids
and keys). Where herrscher had another function on a key Claude uses, Claude's
key wins and the herrscher function moved under `ctrl+x` with a mnemonic letter.
`/help` shows the keys of the main window as they are now, and `/keybindings`
opens your overrides file.

### Main window

Contexts are read in order: Chat first, then Global.

| Key | Action | What it does |
|---|---|---|
| `⏎` | `chat:submit` | send; while a turn runs, queue the message |
| `⌥⏎` | `chat:queueRedirect` | while a turn runs, steer it with the message; when idle, new line. `"alt+enter": null` removes both |
| `⌃J`, `⇧⏎`, `\⏎`, and `⌥⏎` at rest | `chat:newline` | new line |
| `échap` | `chat:cancel` | interrupt the running turn |
| `⇧⇥` | `chat:cycleMode` | cycle the approval mode (Manuel, Édits acceptés, Plan, Auto) |
| `⇥` | `tabs:next` | next session tab |
| `↑` `↓` | `chat:previousHistory`, `chat:nextHistory` | history; `↑` on an empty composer focuses the queue |
| `⌃C` | `app:interrupt` | interrupt a running turn; otherwise quit (the guard below) |
| `⌃D` | `app:exit` | with an empty composer, same guard as `⌃C`; with text in the composer, nothing |
| `⌃R` | `history:search` | search the prompt history |
| `⌃V` | `chat:imagePaste` | paste an image |
| `⌃U` | `chat:removePending` | remove the pending message |
| `⌃S` | `chat:stash` | set the draft aside |
| `⌃L` | `chat:clearInput` | clear the composer |
| `⌃_`, `⌃-`, `⌃⇧-`, `⌃⇧_` | `chat:undo` | undo the last change of the composer (200 steps) |
| `⌃F` | `chat:search` | search the transcript |
| `⌃W` | `app:closeTab` | close the tab |
| `⌃P` | `chat:openBoard` | sessions board |
| `⌃G`, `⌃X ⌃E` | `chat:externalEditor` | edit the draft in `$EDITOR` (then `$VISUAL`) |
| `⌃X ⏎` | `chat:queueSubmit` | send the queue now |
| `⌃X ⌃S`, `alt+F12` | `chat:sendNow` | send now |
| `⌃X ⌃K` | `chat:killAgents` | interrupt every busy session of this window |
| `⌃X C` | `chat:copyCode` | copy the last code block |
| `alt+Y` | `chat:copyReply` | copy the last answer |
| `alt+C` | `chat:toggleCodeFold` | fold long code blocks |
| `alt+E` | `chat:toggleToolExpand` | spell out commands and unfold diffs |
| `⌃O` | `app:toggleTranscript` | the same toggle as `alt+E` |
| `⌃X F` | `chat:toggleTurnFold` | fold the turns |
| `⌃X O` | `chat:openOrFold` | open the selected link, or fold the latest block |
| `⌃X L` | `chat:nextLink` | select the next link |
| `⌃X J` `⌃X K` | `chat:nextTurn`, `chat:previousTurn` | next or previous turn |
| `⌃X M` | `app:toggleMouse` | release the mouse so the terminal can select |
| `⌃X D` | `app:openDiff` | diff panel for the turn or the branch |
| `⌃X S` | `app:openSessions` | session switcher |
| `⌃X T` | `app:openTheme` | theme picker |
| `⌃X V` | `app:toggleSidebar` | toggle the sidebar |
| `?` | `chat:helpToggle` | help, when the composer is empty |
| `n` | `chat:notifyCycle` | cycle the notification, when the composer is empty |
| `/` `@` `!` | not in the table | command, file mention, shell command |

`?` and `n` are typed keys: the table reads them only when the composer is empty,
so they can be rebound like the other keys but still type a character otherwise.

### Moved keys

These herrscher functions had a key that Claude now uses for its own action.
They moved. The last column names Claude's action on the old key. When that
action is listed under Not yet, the old key shows a notice that names the new key.

| Function | Before | Now | Claude's action on the old key |
|---|---|---|---|
| diff panel | `⌃D` | `⌃X D` | `app:exit` (quit with an empty composer) |
| session switcher | `⌃T` | `⌃X S` | `app:toggleTodos` |
| mouse release | `⌃G` | `⌃X M` | `chat:externalEditor` (opens `$EDITOR`) |
| next link | `⌃L` | `⌃X L` | `chat:clearInput` (clears the composer) |
| open the link, or fold | `⌃O` | `⌃X O` | `app:toggleTranscript` (now toggles the tool detail, see the main table) |
| copy the last code block | `⌃Y` | `⌃X C` | `chat:defaultToNewerModel` |
| sidebar | `⌃B` | `⌃X V` | `task:background` |
| fold the turns | `alt+T` | `⌃X F` | `chat:thinkingToggle` |
| previous or next turn | `alt+↑` `alt+↓` | `⌃X K` `⌃X J` | `app:diffFileListUp` and `app:diffFileListDown` |

### Not yet

These are names of Claude's reference that herrscher does not run. The first
table holds the fourteen keys that are still bound by default. Pressing one of
them in the chat does nothing but show a short notice that names the action, says
it is not yet available in herrscher, and names the new key when the action is in
Moved keys. The draft in the composer stays as it is.

| Keys | Action | Status |
|---|---|---|
| `⌃X ⌃A`, `⌃X ⇥` | `abovePrompt:toggle`, `abovePrompt:focus` | reserved, does nothing |
| `⌃⇧B` | `app:toggleBrief` | reserved, does nothing |
| `⌃]` | `app:openArtifact` | reserved, does nothing |
| `⌥P` | `chat:modelPicker` | reserved, does nothing |
| `⌥O` | `chat:fastMode` | reserved, does nothing |
| `⌥W` | `chat:workflowKeywordToggle` | reserved, does nothing |
| `⌘K` | `chat:clearScreen` | reserved, does nothing |
| `⌃T` | `app:toggleTodos` | moved to `⌃X S` |
| `⌃Y` | `chat:defaultToNewerModel` | moved to `⌃X C` |
| `⌃B` | `task:background` | moved to `⌃X V` |
| `alt+T` | `chat:thinkingToggle` | moved to `⌃X F` |
| `alt+↑` `alt+↓` | `app:diffFileListUp`, `app:diffFileListDown` | moved to `⌃X K` `⌃X J` |

The table accepts the other names of Claude's reference without a message,
because herrscher does not run them yet. Each is listed by its action name,
grouped by context prefix, and none is bound or executed here:

- `abovePrompt:highlightNext`, `abovePrompt:highlightPrevious`, `abovePrompt:leave`, `abovePrompt:next`, `abovePrompt:press`, `abovePrompt:previous`
- `agents:find`, `agents:nextGroup`, `agents:previousGroup`, `agents:rename`, `agents:switchView`, `agents:togglePin`
- `app:redraw`, `app:toggleDiffNoiseFilter`, `app:toggleDiffPreSession`, `app:toggleReplTab`, `app:toggleTerminal`
- `attachments:exit`, `attachments:next`, `attachments:previous`, `attachments:remove`
- `autocomplete:next`, `autocomplete:previous`
- `confirm:cycleMode`, `confirm:next`, `confirm:nextField`, `confirm:previous`, `confirm:previousField`, `confirm:toggle`
- `diff:back`, `diff:nextSource`, `diff:previousSource`
- `effortSlider:decreaseEffort`, `effortSlider:increaseEffort`, `effortSlider:thisSessionOnly`, `effortSlider:toggleUltracode`
- `footer:clearSelection`, `footer:close`, `footer:dismiss`, `footer:down`, `footer:next`, `footer:openSelected`, `footer:previous`, `footer:up`
- `help:dismiss`
- `historySearch:cycleScope`, `historySearch:execute`
- `modelPicker:decreaseEffort`, `modelPicker:increaseEffort`, `modelPicker:thisSessionOnly`
- `pane:bottom`, `pane:grow`, `pane:pageDown`, `pane:pageUp`, `pane:scrollDown`, `pane:scrollUp`, `pane:shrink`, `pane:top`
- `permission:toggleDebug`
- `plugin:cycleMarketplace`, `plugin:favorite`, `plugin:install`, `plugin:toggle`
- `scroll:bottom`, `scroll:fullPageDown`, `scroll:fullPageUp`, `scroll:halfPageDown`, `scroll:halfPageUp`, `scroll:lineDown`, `scroll:lineUp`, `scroll:top`
- `selection:clear`, `selection:copy`, `selection:extendDown`, `selection:extendLeft`, `selection:extendLineEnd`, `selection:extendLineStart`, `selection:extendRight`, `selection:extendUp`
- `settings:fixUsage`, `settings:periodDay`, `settings:periodWeek`, `settings:retry`, `settings:search`, `settings:sortByTokens`
- `tabs:previous`
- `theme:toggleSyntaxHighlighting`
- `transcript:exit`, `transcript:toggleShowAll`
- `voice:pushToTalk`

Of Claude's 148 reference names, 44 run here, the fourteen in the first table are
bound by default and do nothing yet, and the 90 bullets make up the rest. The
names are kept in `plugins/terminal/tui/keys_claude_reference.go`, checked against
a frozen copy of the reference in `testdata`, and a test loads a Claude file for
each of them and requires no warning.

### Quitting: `⌃C` and `⌃D`

With nothing running, `⌃C` and `⌃D` (with an empty composer) quit at once. When
a turn runs in the active session, the first press interrupts it and a second
press quits. When another session is busy, the first press warns and names how
many sessions will stop, and a second press within 3 seconds quits. Inside a
dialog (a commit message or its editor, an approval reason), `⌃C` closes it
as `⎋` does, keeps the text, and neither quits nor interrupts the turn. In the
diff note `⌃C` does nothing, because closing the note would drop what is typed
there: `⎋` closes it. Quitting a served daemon
stops the sessions it runs, as described above.

### Your keys: `~/.config/herrscher/keybindings.json`

The file has Claude Code's exact format, so a file copied from
`~/.claude/keybindings.json` works as it is:

```json
{
  "$schema": "https://www.schemastore.org/claude-code-keybindings.json",
  "$docs": "https://code.claude.com/docs/en/keybindings",
  "bindings": [
    { "context": "Chat", "bindings": { "ctrl+e": "chat:externalEditor", "ctrl+g": null } }
  ]
}
```

- Your bindings are added after the defaults. A later binding on the same chord
  in the same context replaces the default, and `null` removes a default.
- A chord is one or more keys separated by a space (`ctrl+x ctrl+e`). Modifiers
  are `ctrl`, `alt` (also `meta`, `opt`), `shift` and `cmd` (also `super`, `win`).
- Not remappable: `ctrl+c`, `ctrl+m`, `ctrl+[`, `ctrl+i`, `ctrl+h` (they are the
  same bytes as other keys in a terminal), and `ctrl+d` as an exit key. `ctrl+z`
  and `ctrl+\` are reserved by the terminal.
- An invalid entry is skipped and reported in the banner: an unknown context,
  an unknown modifier, an unknown action, a duplicate in one context, a key that
  cannot be used, or a value that is neither an action nor `null`. The rest of
  the file still applies. A file that is not valid JSON is skipped as a whole,
  and reported.

### Claude's file and `[keys]`

By default herrscher also reads `~/.claude/keybindings.json` before its own
file, so your Claude personalisations apply here too. Claude actions that
herrscher does not run are accepted without a message, while an action name
that neither product knows is reported. The `[keys]` section of
`terminal.toml` controls this:

```toml
[keys]
import_claude = false
chord_timeout = 3
```

`chord_timeout` is the delay in seconds between two keys of a chord (default 3).

### `/keybindings`

`/keybindings` opens the herrscher file in `$EDITOR`. It creates the file with
`$schema`, `$docs` and `bindings: []` when it does not exist, and reloads the
table when the editor closes, flashing the problems if there are any.

### Overlays and footers

The overlays (diff, sessions, search, rewind, theme, status bar, plugins) still
read their own keys, and their footers and the composer placeholder still show
the built-in keys. A changed key shows in `/help` and in the chat bindings, not
yet in those footers.

→ `plugins/terminal/tui/keys.go`, `plugins/terminal/tui/keys_defaults.go`,
`plugins/terminal/tui/keys_file.go`, `plugins/terminal/tui/keys_runtime.go`

[herrscher-jev-compactor]: https://github.com/Herrscherd/herrscher-jev-compactor
