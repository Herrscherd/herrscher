# Self tools

A session can read and configure the herrscher that runs it. Each local session
gets an MCP server named `herrscher` in its worktree, and through it the agent
reaches every verb of the daemon: sessions, agents, schedules, approvals,
memory, skills, hosts, models, the configuration file, the settings file, the
modules, the setup state, the forge connection, issues and review threads.

## What the agent sees

`herrscher mcp` is the server. It speaks MCP over stdio and talks to the
command socket of its session (`HERRSCHER_COMMAND_SOCKET`). It refuses to start
without that variable: it never falls back to the operator's socket, where a
caller is `owner`. Its tools are generated from the command registry, so a verb
added to the daemon, or contributed by a plugin, becomes reachable without a
line of its own. The list is read again at each `tools/list`.

- `herrscher_commands` lists every verb with its help and parameters, as JSON.
  `family` narrows the list (`session`, `schedule`, `config`, ...).
- `herrscher_run` runs one verb: `command` is its path, `args` its named
  parameters, `rest` its positional arguments, `json` asks for machine output.
  An agent's reply rule and channels are set with `agent set-reply`, the name,
  the rule and the channels as `rest`. The `agent` role does not run that verb
  on its own: a `herrscher_run` call for it becomes a question to the operator
  (see "What it may change" below), and the operator answers it on the
  gateways. The agent cannot give itself a voice.
- The everyday reads have a tool of their own: `whoami`, `health`,
  `session_list`, `session_info`, `session_who`, `session_log`,
  `session_history`, `agent_list`, `models_list`, `skill_list`,
  `schedule_list`, `approve_list`, `memory_search`, `config_get`, `env_get`,
  `session_issue_list`, `session_issue_show`, `drive_list`, `drive_ls`,
  `drive_read` and `drive_log`. A test keeps every one of them
  a verb the agent runs without asking.
  `HERRSCHER_SELF_TOOLS_EXPOSE=all`, or `selfTools.expose` in `config.json`,
  gives every verb a tool, at the cost of more schemas in each turn. The daemon
  settles the exposure and hands it to each session, so the server and the
  weight the daemon reports never disagree. An unknown value serves the core
  set and says so on stderr.

A value is sent as `--name=value`, so a value that starts with `--` stays a
value. A positional argument that starts with `-` is refused, and so is a
parameter the verb does not declare: what the agent sends is what the daemon
parses.

Calls run concurrently, eight at most, so a call that waits on the operator
does not hold the others back. `notifications/cancelled` stops the call it
names: the server drops its connection to the daemon, and the daemon withdraws
the question that call was waiting on, so a late `allow` runs nothing.

## What it may change

The daemon decides each call under the role of the session, as it decides the
`herrscher` command line typed in the session's shell.

- **Reads pass.** What the `agent` role runs runs at once, now including
  `health`, `usage`, `skill list`, `schedule list`, `host list`, `plugin list`,
  `connect status`, `setup check`, `config get`, `env get`, `env request` and
  `approve list`.
  `approve list` shows the session its own questions and those of the sessions
  it delegated, nothing else.
- **Writes are asked.** A verb the `agent` role does not run is not refused any
  more: it becomes a question to the operator, on the session's gateways, with
  the same four answers as a tool call (`allow`, `allow --session`,
  `allow --always`, `deny`) and the same deadline. The question reads
  `Herrscher(<verb and its arguments>)`, an argument with a space or a quote
  quoted, so two different calls never read the same. `allow --always` writes
  a rule for that exact call. A wider rule, such as
  `allow Herrscher(schedule add *)`, is written by hand with `approve rule`.
  `approve rule` asked by a session writes a rule that binds that session
  alone (`--session`, its own name by default, or a session it delegated),
  never one that binds the whole daemon.
  The session's approval mode does not change this: even a `bypass` session
  asks.
- **Some verbs are never run for a session.** `role *`, `approve allow`,
  `approve deny` and `drive grant` are refused outright, even with the operator's approval: a
  session does not grant itself rights and does not answer its own questions.
- **The subtree still binds.** A verb that names another session than the
  caller or one it delegated is refused, asked or not.
- **Each write is announced.** Every verb a session runs that changes
  something (`session send`, `session issue open`, `session pr reply`,
  `memory record`, a verb run after an approval or as steward, ...) is posted
  to the session's gateways as a notice, cut at 240 characters, and so is a
  question the operator refused. Reads stay silent: a verb declared read only
  (`contracts.New(...).Reads()`), and among the daemon's own verbs what an
  `observer` runs, a verb whose last word names a read (`list`, `show`, `get`,
  `get-mode`, `status`, `checks`, `comments`, ...), and `approve rule` with no
  rule, which only lists the session's own rules. A plugin's verb is announced
  unless the plugin declared it read only: its name alone proves nothing. So do
  `approve ask`, which the approval hook calls at each tool call, and
  `env request`, which is its own notice.

### Guards that hold even after an approval

- A session never writes a secret. A variable counts as one unless it is shown
  in clear by `env get`: a name under `HERRSCHER_`, `MEMORY_`, `AGENT_`,
  `JEV_`, `OBSIDIAN_` or `OTEL_`, or ending in `_MODEL`, `_STREAM`,
  `_DIR`, `_KIND`, `_EFFORT`, `_VERBOSITY`, `_CONTEXT_MESSAGES`, `_TIDY_PINGS`
  or `_MESSAGE_DELETES`, and holding none of `TOKEN`, `KEY`, `SECRET`,
  `PASSWORD`, `PASSWD`, `PASS`, `PAT`, `CREDENTIAL`, `AUTH`, `HEADERS`, `DSN`
  or `URL`. `env set` and `env unset` on any other variable are refused before any
  question, so no value reaches a gateway and no secret is erased. The session asks for it with
  `env request --key <name> [--reason …]`, which posts the request on its
  gateways with the command for the operator to type; no value travels.
- A session restarts the daemon once every ten minutes at most, by
  `service restart` or `service update`. The time is kept in `state.json`, so
  the daemon that comes back still knows it; entries older than the window are
  dropped. When the daemon is back, the session that asked gets a turn telling
  it so, to carry on with its task; a restart that failed tells nobody.
- `session set-budget` from a session never sets a cap above the cap of the
  target's parent, and never removes a cap the parent has.

### A trusted agent

`herrscher role grant session:<name> steward` lets one session configure the
daemon without asking. A `steward` runs what an `operator` runs, with these
exceptions on top of the verbs never run for a session, still put to the
operator: `drive write` and `drive remove`, so every write to a drive is
asked; `approve rule` and `approve mode`, which change the policy every
session answers to; `session create`, which opens a session outside the
steward's subtree; and `config set|unset` and `env set|unset`, which decide who
commands the daemon (the owner, the gateway allow lists) and where the API keys
go (`keys.*`). It stays inside its subtree. It is the one role a session
can be granted, and granting it to a session does not turn the humans of the
gateways into observers, as the first human grant does.

## The configuration and settings files

`config get`, `config set` and `config unset` read and write the `config.json`
the daemon was started with (`serve --config`, the default path otherwise), by
dotted key (`telemetry.enabled`, `compactAt`, `selfTools.expose`). A value that
is valid JSON is stored as that JSON (`80`, `true`, `["jev"]`), anything else
as a string. The key set is checked against what the daemon reads, spelling
and case included; the rest of the file is checked the way `Load` reads it, so
a key it ignores, such as `$schema`, does not block an edit. A rewrite cannot
keep full-line comments: when the file had some, the previous version is kept
as `config.json.bak` and the reply says so.

`env get`, `env set` and `env unset` do the same on the daemon's settings file
(`HERRSCHER_ENV_FILE`). Comments and the other lines are kept, a variable set
twice is left once, and `env get` hides the value of every variable it does
not show in clear (the rule above). A value with a space or a shell character
is written between single quotes; one that holds a single quote is written
between double quotes, and refused if it also holds `"`, `$`, a backquote or
a backslash, which no quoting keeps intact for every reader of the file.

Both files are read at start: a change takes effect at the next
`herrscher service restart`. Two edits of the same file never interleave.

## Modules, connections and setup

`plugin list`, `plugin add --module <path[@version]>` and
`plugin rm --module <path>` run `herrscher plugin` in the daemon's environment:
`add` and `rm` rebuild the binary (`--no_build` only edits the module list),
within fifteen minutes. A module path never starts with `-`. A call that waits
for the operator and then for a build still fits in one tool call: the self
tools wait for the approval deadline plus twenty minutes.
`connect status` shows the forge connections and where each token comes from.
`setup check` reports each section of `herrscher setup` as `ok`, `warning`,
`bad` or `off` without changing anything; `--probe` also checks each account
against its service and opens no sign-in. `herrscher setup check` runs the same
report in a terminal.

## Drives

`drive list`, `drive ls`, `drive read` and `drive log` are reads the agent
runs at once, within the access its agent holds on each drive, and each has a
tool of its own. `drive write` needs `write` access and is asked every time,
of a steward too; `drive remove` is asked, and `drive grant` is never run for
a session, as it grants rights. The drives, the
paths and the journal are described in [Drives](drives.md).

## Issues and review threads

`session issue list`, `show`, `open` and `comment` work on the repository the
session works in, through the host's forge connection: the agent never holds a
token. `--state` takes `open` (the default) or `closed`, `--labels` a comma
separated list, `--limit` the most issues to list (100 by default, a line says
when more were left out). `list` leaves out the pull requests the forge also
lists among issues, and `--limit` counts only the issues it lists. `show` on a
number the forge does not list as an issue, a pull request for one, says that
no issue has that number. `session pr reply --thread <id>` answers one review
thread of the session's pull request; `session pr comments --json` gives each
thread's id. An agent runs all of these without asking, as it already opens its
pull request with `session pr open`: what it writes there is public on the
forge, under the account `herrscher connect` connected. A forge plugin too old
to write issues makes `open` and `comment` say so and name `herrscher update`.

`session end` is not an agent verb: it closes the session and deletes its branches,
so the agent role refuses it, as it refuses `session close`. The operator and a
steward session run it.

## Where it is declared

Each time a local session with a worktree starts, a restart and a fork
included:

- `.mcp.json` gets the `herrscher` server, beside the agent's own servers;
- `.claude/settings.local.json` enables it (`enabledMcpjsonServers`), so Claude
  Code loads it without asking, and only once the `herrscher` entry of
  `.mcp.json` is the one herrscher wrote;
- `.codex/config.toml` gets the same server, with `env_vars` forwarding the
  session's socket, name and token, which Codex does not pass on by itself, and
  a `tool_timeout_sec` long enough for an operator's answer. The file is left
  as it is when it already declares this exact server, and the server is
  appended when it declares none, so comments stay; only a different
  `herrscher` table makes the file be rewritten.
- `.cursor/mcp.json` gets the same server for `cursor-agent`, which may ask
  the operator once to approve a project MCP server.

When the repository tracks `.mcp.json` or `.claude/settings.local.json`,
nothing is enabled for Claude Code, so a server the repository ships under the
name `herrscher` never runs unasked; a tracked `.codex/config.toml` or
`.cursor/mcp.json` is left alone too, and so is any of these paths that
crosses a symbolic link. The daemon's log says which file was skipped. A shared session and a
session on another host get no server. `HERRSCHER_SELF_TOOLS=off`, or
`selfTools.enabled: false` in `config.json`, turns the declaration off.

## What it costs

`herrscher health` has a `self-tools` line: whether sessions get the server,
the exposure, the number of tools and the bytes their declarations weigh.
`herrscher health --deep` also starts `herrscher mcp` against the daemon's own
command socket and counts the tools it lists, so the line says the binary
answers and the daemon serves it its verbs. The path through a session's own
socket is what the end to end scenario proves. A session that asks for
`health --deep` gets the measured line without the start, so it cannot make
the daemon spawn processes in a loop. The
same weight is reported in each turn's context sizes, under the tools line of
`/context` in the terminal UI.

## Limits

- An approval question waits as long as a tool call's does.
- A remote session gets no self tools: its worktree is on another machine, and
  its command socket is not.
- The trust model of [Approvals](approvals.md#who-may-change-the-approvals)
  still holds: an approved shell command runs with the operator's rights and
  can reach the operator's socket. The self tools do not make that weaker or
  stronger; they make the honest path the easy one.
