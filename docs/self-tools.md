# Self tools

A session can read and configure the herrscher that runs it. Each new local
session gets an MCP server named `herrscher` in its worktree, and through it the
agent reaches every verb of the daemon: sessions, agents, schedules, approvals,
memory, skills, hosts, models, the configuration file, the setup state and the
forge.

## What the agent sees

`herrscher mcp` is the server. It speaks MCP over stdio and talks to the
command socket of its session (`HERRSCHER_COMMAND_SOCKET`), never to the
operator's socket. Its tools are generated from the command registry, so a verb
added to the daemon, or contributed by a plugin, becomes reachable without a
line of its own.

- `herrscher_commands` lists every verb with its help and parameters, as JSON.
  `family` narrows the list (`session`, `schedule`, `config`, ...).
- `herrscher_run` runs one verb: `command` is its path, `args` its named
  parameters, `rest` its positional arguments, `json` asks for machine output.
- The everyday verbs have a tool of their own: `whoami`, `health`,
  `session_list`, `session_info`, `session_who`, `session_log`,
  `session_history`, `agent_list`, `models_list`, `skill_list`,
  `schedule_list`, `approve_list`, `memory_search`, `config_get`,
  `session_issue_list` and `session_issue_show`.
  `HERRSCHER_SELF_TOOLS_EXPOSE=all` gives every verb one, at the cost of more
  schemas in each turn.
- `setup_check` reports each section of `herrscher setup` as `ok`, `warning`,
  `bad` or `off`, without changing anything. `probe` also checks each account
  against its service, and opens no sign-in.

A value is sent as `--name=value`, so a value that starts with `--` stays a
value. A positional argument that starts with `-` is refused, and so is a
parameter the verb does not declare: what the agent sends is what the daemon
parses.

## What it may change

The daemon decides each call under the role of the session, as it decides the
`herrscher` command line typed in the session's shell.

- **Reads pass.** Everything the `agent` role runs runs at once, now including
  `health`, `usage`, `skill list`, `schedule list`, `host list` and
  `config get`.
- **Writes are asked.** A verb the `agent` role does not run is not refused any
  more: it becomes a question to the operator, on the session's gateways, with
  the same four answers as a tool call (`allow`, `allow --session`,
  `allow --always`, `deny`) and the same deadline. The question reads
  `Herrscher(<verb and its arguments>)`, and `allow --always` writes a rule of
  that shape, such as `allow Herrscher(schedule add --name=digest*)`. The
  session's approval mode does not change this: even a `bypass` session asks.
- **Some verbs are never asked.** `role *`, `approve allow` and `approve deny`
  are refused outright: a session does not grant itself rights and does not
  answer its own questions.
- **The subtree still binds.** A verb that names another session than the
  caller or one it delegated is refused, asked or not.

### A trusted agent

`herrscher role grant session:<name> steward` lets one session configure the
daemon without asking. A `steward` runs what an `operator` runs, except `role`,
`approve allow` and `approve deny`, and stays inside its subtree. It is the one
role a session can be granted, and granting it to a session does not turn the
humans of the gateways into observers, as the first human grant does.

## The configuration file

`config get`, `config set` and `config unset` read and write `config.json` by
dotted key (`telemetry.enabled`, `compactAt`, `skills.roots`). A value that is
valid JSON is stored as that JSON (`80`, `true`, `["jev"]`), anything else as a
string. A write is checked the way the daemon reads the file, unknown keys
included, and refused before anything is written. The daemon reads the file at
start, so a change takes effect at the next `herrscher service restart`.
A rewrite cannot keep full-line comments: when the file had some, the previous
version is kept as `config.json.bak` and the reply says so.

## Issues

`session issue list` and `session issue show` read the issues of the repository
the session works in, through the host's forge connection: the agent never
holds a token. `--state` takes `open` (the default) or `closed`, `--labels` a
comma separated list. Both are reads, so an agent runs them without asking.

## Where it is declared

At `session create`, for a session with a worktree on this machine:

- `.mcp.json` gets the `herrscher` server, beside the agent's own servers;
- `.claude/settings.local.json` enables it (`enabledMcpjsonServers`), so Claude
  Code loads it without asking;
- `.codex/config.toml` gets the same server, with `env_vars` forwarding the
  session's socket, name and token, which Codex does not pass on by itself.

A file the repository tracks is left alone, and the daemon's log says which. A
shared session and a session on another host get no server.
`HERRSCHER_SELF_TOOLS=off` turns the declaration off.

## Limits

- The approval question waits as long as a tool call's does. The server answers
  other calls in the meantime, up to eight at once.
- A remote session gets no self tools: its worktree is on another machine, and
  its command socket is not.
- The trust model of [Approvals](approvals.md#who-may-change-the-approvals)
  still holds: an approved shell command runs with the operator's rights and
  can reach the operator's socket. The self tools do not make that weaker or
  stronger; they make the honest path the easy one.
