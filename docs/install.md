# Install and run

This page takes you from an empty directory to a running Herrscher daemon. Herrscher is a **single binary**: the agnostic domain (`core`), the composition root and daemon, and the plugin-management CLI all live in one Go module. The swappable edges (gateway, backend, memory, orchestrator) are separate modules compiled in.

## Setup

`setup` opens an interactive screen: you check the modules you want (gateways,
backends, memory, skills), then each one is configured in turn. The Discord token
is checked against Discord, Claude and Codex open their own sign-in
(`claude auth login`, `codex login`) or take an API key, the memory vault is tested
for writing, and the route screen sets the policy, the gateway credentials that
`gateway-only` needs, the default model and the language. A review lists what will
change before anything is written to `.env`; on a source checkout it also rewrites
`plugins.go`, fetches the modules and rebuilds, restoring `plugins.go`, `go.mod`
and `go.sum` if the build fails. The new build replaces the binary you ran, so the
next `herrscher` already has the chosen modules; the previous binary stays beside
it with a `.old` suffix until the next rebuild. On Windows a previous binary that
still runs cannot be deleted, so the rebuild moves it aside under a `.old-parked-`
name and removes it on a later rebuild, once it has stopped.

Settings live in the `.env` of the directory you run from, or in the file
`HERRSCHER_ENV_FILE` names. Run `setup` and `herrscher` from the same directory,
or export `HERRSCHER_ENV_FILE`, so the host finds what `setup` wrote.

Once the settings are written, `setup` sends each chosen backend CLI one short
prompt and waits for its answer. A backend that does not answer is shown with its
output and the remedy: install the CLI when it is missing, or sign in again with
`herrscher setup claude` (or `codex`, `cursor`). The settings stay written and
`setup` exits with status 1, so a script sees the failure.

On a machine with no settings file and no host state yet, a bare `herrscher` in a
terminal opens `setup` instead of serving. Without a terminal it prints the
usage after a line saying to run `herrscher setup`.

Run again, `setup` opens a dashboard with the state of every section, and
`herrscher setup discord` (or `claude`, `codex`, `cursor`, `obsidian`, `route`,
`modules`) jumps straight to one. `herrscher init --yes` with its stack flags stays
the non-interactive path for scripts, and `herrscher models list` shows what the
active policy offers. `init` ends with the same prompt to each chosen backend and
exits with status 1 when one does not answer, scripts included. A script that
prepares a machine before its backend CLI is installed or signed in passes
`--no-trial`.

The gateway credentials are both halves or neither. A base URL without a token
would redirect the CLI while it keeps authenticating as this machine's own
account, which is the one failure mode worth refusing outright.

**The terminal path needs no Discord token.** The in-tree terminal gateway is a
first-class gateway, so you can create, drive and resume sessions from your shell
alone. A token is only required if you enable the Discord gateway. For a
boot-started service, see [Service](service.md).

Installed as a service, the daemon keeps the `PATH` of the shell that installed
it: a launcher supplies almost nothing, and an agent cannot use a tool it cannot
find. It drops the entries that cannot outlive that shell, because a service
resolving commands through a directory anyone can recreate would trust whoever
got there first.

## Prerequisites

| Requirement | Why |
|-------------|-----|
| **Go 1.27** | Builds the single binary and, when a source checkout is present, rebuilds it with the compiled-in plugins. |
| **A Discord bot token** | Only if you enable the Discord gateway, which needs `DISCORD_BOT_TOKEN`. The terminal path needs none. |

If you later swap the gateway for another platform, the token requirement follows the plugin, not the host. The host resolves whatever the active gateway declares.

## Clone the repository

A fresh clone builds on its own. The committed `go.mod` points at **published, tagged modules** from `github.com/Herrscherd`, so the module proxy resolves the plugins with nothing checked out beside the repo: there are no `replace` directives.

```bash
git clone https://github.com/Herrscherd/herrscher.git
```

For cross-repo development against local checkouts, use a `go.work` workspace rather than editing `go.mod`; see Layout & wiring.

## Build the single binary

```bash
cd herrscher
go build -o herrscher .          # the only binary; plugins are compiled in
```

The plugins are baked into this one binary via blank imports in `plugins.go` (the xcaddy pattern). To change which plugins are compiled in, use the management verbs, see [Managing plugins](plugins.md).

## Install system-wide (Arch / pacman)

The repo ships a `PKGBUILD`, so on Arch-based systems you can build and install under pacman management:

```bash
makepkg -si                      # builds and installs /usr/bin/herrscher
```

The packaged binary is named **`herrscher`**. A pacman-installed binary has its plugins already compiled in and typically has no source checkout, so the self-management verbs run in config-only mode (see [Managing plugins](plugins.md)).

## Run it directly (foreground)

The shortest way to see it live is to run `serve` in the foreground with the health endpoint enabled:

```bash
export DISCORD_BOT_TOKEN=...      # required
export DISCORD_USER_ID=...        # required: the user the bot obeys
export HERRSCHER_OWNER_ID=...     # optional: the instance-id fallback

./herrscher serve --health-addr :8787
```

When `serve` runs on an interactive TTY it also brings up the in-tree terminal gateway (a Bubbletea TUI) in the foreground as a first-class peer of Discord. A background service with no TTY runs headless, hub-driven only.

### The health endpoint

`GET /health` returns JSON liveness plus a `metrics` object:

| Metric group | Contents |
|--------------|----------|
| Turn counters | turns started / completed / abandoned |
| Supervision | bridge restarts |
| Remote transport | remote resolve attempts / failures |
| Latency | remote-call latency: count, p50 / p95 |

The 60s status embed carries the turn and restart counts too. See [Configuration](env.md) for the precedence and the `.env` / `config.json` sources of `--health-addr`.
## Install as a boot-started service (recommended)

For an always-on deployment, don't run in the foreground: install a native OS service. `herrscher service install` writes a native unit plus a `0600` secrets template and never bakes the token into the unit file:

```bash
./herrscher service install \
  --cmd "claude --model claude-opus-5-5 --effort low" \
  --health-addr :8787
```

The per-OS details, scaffolded files, and the restart / update / uninstall lifecycle are documented on the dedicated [System service](service.md) page.
## Your first session

### The one-liner

If all you want is one task done, that is the whole quickstart: no token, no `serve`, no slash command:

```bash
./herrscher "read the thread and propose a split"
```

An argument carrying whitespace is a task, not a verb. It opens a session of its own, sends the task there, and opens the TUI on that tab, so the turn runs in front of you and you can answer it. Everything below is the same machinery, taken one piece at a time. Read on when you want a daemon that stays up, several sessions at once, or Discord.

### Path A. Discord

Once the bot is online, from any channel the bot can see:

```text
/set home #your-category
/session create name:hello
```

- `/set home` records where new sessions put their channels (a category or a forum). All slash handling lives in the gateway plugin; the daemon only ever sees neutral argv.
- `/session create` creates a bridged channel, an isolated git worktree, and a backend runner. It appears as a new channel under your home.

Then **start talking in the session channel**. Each message becomes one turn: the hub hands it to the session's bridge, the backend streams tool activity and cost, and the smart Discord gateway renders its own progress message, acknowledgement, and summary.

### Path B. Terminal TUI

If you ran `serve` on an interactive TTY, the terminal gateway is already in the foreground. Create and drive sessions with slash commands, and **no Discord required**:

```text
/session create --name foo
/session list
/session close --name foo
```

Each session bound to the terminal appears as a **tab**. Typing routes your input to the active session and streams replies into its pane. A few essentials:

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Switch to next / previous session tab |
| `Enter` | Send the typed line (lines starting with `/` run as commands) |
| `Ctrl+W` | Close the active session tab (asks for confirmation) |
| `?` (empty input) | Toggle keybinding help |
| `PgUp` / `PgDn` | Scroll the transcript |
| `Ctrl+C` / `Ctrl+D` | Quit the TUI when the composer is empty; the first press interrupts a running turn |
| `Esc` | Interrupt the running turn |

See [The terminal UI](terminal-ui.md) for the full interface.
### What just happened


A session is a channel + an agent + (optionally) an isolated git worktree, supervised by a long-lived bridge. One turn is active at a time per session; the next queued input starts only after the current turn ends.

## Starting work

```bash
./herrscher "read the thread and propose a split"   # a session on that task
./herrscher --print "…"                             # one turn, the reply on stdout
```

An argument that carries whitespace is a task, not a verb. It opens a session of
its own (isolated worktree, terminal-only, named after the task's opening words),
sends the task there, and opens the UI on that tab so the turn runs in front of
you and you can answer it. The session is persistent either way, and closing the
window does not end it: `session list` shows it, `session seed --name` continues
it, `session close` ends it.

`--print` is the non-interactive form: one turn, the reply on stdout and the
session name on stderr, so `herrscher --print "…" > out.md` holds the answer
alone. A run with no terminal takes that form on its own, since there is nothing
there to draw a window on. When the vendor ends the turn in error, the command
exits with a failure and the error goes to stderr. The text the vendor streamed
before the failure goes to stdout, so `> out.md` keeps whatever was produced, and
nothing else on stdout. Text blocks are joined as the backend sends them, its
blank line between blocks included. When the backend retries an attempt, the text
of the abandoned attempt is dropped, so stdout holds only the attempt that failed.
The claude backend retries only before the vendor has taken the turn, which an
assistant or user line takes, and so does a system line of subtype init or
api_retry: once it is taken, a stream that ends is a failed turn, with the text
so far on stdout and no second attempt.
On Unix, when a session closes or a one-shot turn returns, the claude backend kills the
whole process group of the CLI, so a process the CLI started outlives it only if it left
that group itself (setsid). On Windows the backend kills only the CLI process.

A single unrecognised word stays a verb, so a typo is still a typo. Use `-p` to
force a one-word task.

## A bare `herrscher`

A bare `herrscher` starts something rather than resuming it. Every launch opens
on a conversation of its own, named after the directory you launched in
(`herrscher-2` if that name is taken, a random `s-xxxx` if the directory cannot
be one). What came before is still there: a live tab if it is still running,
`/resume` otherwise. Nothing piles up either, since a session nobody spoke in is
archived when the window closes. An attached window opens that conversation on
the first message you type, in the repository you launched from when it sits
right under the daemon's workspace, and shared with the daemon's other clients.

One daemon per state file. A second one over the same state answers every message
twice and bills it twice. So a bare `herrscher` on a TTY **serves when nothing
else is** and **attaches** when the service already is, driving the same UI over
the sockets the running daemon exposes. You see the agent that is actually live.
Quitting the window has two outcomes. After an attach, the daemon you found keeps
running. After a serve, quitting stops that daemon, and the turns still running
in it are cancelled. If the hub stops while the window is open, for instance
because its command or events socket is already served by another daemon, the
window closes and says why. Off a TTY there is nothing to
attach to, so the second daemon is refused and the process holding the state is
named. Use `--state` if you really want a separate world, with its own
`--instance` too: a daemon refuses to take over a command or events socket that
another daemon still answers on, and says so instead of cutting that one off.

## Where the sockets live

Every socket and lock herrscher creates for a user lives in one private directory,
chosen the same way on every run: `/run/user/<uid>/herrscher` when the systemd user
session has provided a real `/run/user/<uid>` directory owned by the user with mode
0700, and `/tmp/herrscher-<uid>` otherwise. The directory must be a real directory
(not a symbolic link), owned by the user, with no group or other bits, and it is
checked on every use. A socket keeps its readable name there when the full path fits
the 103 byte unix limit, and takes a hashed name (`hs-<key>.sock`) when it does not.

A socket file is created by binding it under a temporary name in the same directory and
then renaming it onto its final name, so a file left by a crash is replaced in one step.
Closing a listener never removes its file: a stopped daemon leaves its socket on disk,
dead, and a client decides whether a daemon is there by dialing it, where a refused
connection means that no daemon is running. When a daemon stops for good, it first refuses
to start any further session, then ends the running ones, closes its listeners and
removes the socket files of its own instance (command, events and session sockets),
all while it still holds the instance lock. Files of other instances are never
touched. On Windows the sockets are named pipes, which have no file and need no cleanup.

When the chosen directory exists but cannot be used, the daemon refuses to start and
says why. A symbolic link, a directory created by another account (only that account
or root can remove it, and a systemd user session avoids `/tmp` altogether) and a directory open
to its group or others (`chmod 700` on it fixes that) each get their own message.
herrscher never changes a directory it does not own.

The directory is chosen once, at the entry point of the program, and passed down as a
value. Library code takes a `host.RuntimeLayout` argument (`CommandSocketPathIn`,
`EventsSocketPathIn`, `ForwardToDaemon`, and so on), so a caller can point the same code
at another directory. There is no command line flag for that choice: the hidden
`--runtime-dir` flag is gone, and the `serve` process that the tests start reads its
directory from the environment instead.

Neublox, or any other client that follows the events of an instance, must call
`host.EventsSocketPath(instanceID)` to find that socket rather than rebuild the name.
The function is documented in the `core/host` package godoc.

Upgrade note: sockets moved from `/tmp` to the per-user directory described above.
Clients built before this version must be updated to find the sockets through
`host.EventsSocketPath` and the command socket helpers, and remote sessions must be
restarted once so they forward the new paths. An older `/tmp/herrscher-<uid>` left with
group or other access is refused with a message that tells you to run `chmod 700` on it.
A directory owned by another user is refused too: ask that user or root to remove it.

