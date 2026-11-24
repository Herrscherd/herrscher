# Environment variables

Herrscher is configured by precedence, highest first:
**CLI flag → environment → `config.json` → built-in default**. At startup the
host auto-loads a project-root `.env` so every command, and every plugin's
config resolution, sees its vars without an explicit `--env-file`. A real
environment variable always wins over the file.

The key point: the **active gateway plugin declares its own required vars** (the
Discord gateway needs `DISCORD_BOT_TOKEN`); the host resolves them generically.
The variables below are the ones the host and the bundled plugins read.

## Gateway and state (Discord / dctl)

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `DISCORD_BOT_TOKEN` | unset (required for the Discord gateway) | The Discord bot token. Required when the active gateway is Discord; read with echo disabled by the `init` wizard. | Discord gateway plugin |
| `DISCORD_USER_ID` | unset (required for the Discord gateway) | The Discord user the bot obeys. It reads every channel it can see, but only this user's mentions and replies make it act: the bot is bound to a person, not to a channel. | Discord gateway plugin |
| `DISCORD_VERBOSITY` | `silent` | How much of a turn is posted in-channel: `silent` (the answer and nothing else), `quiet` (adds a live list of tool names), `actions` (adds each tool's detail), `full` (adds the assistant's thinking). Raise it only in a channel you would not mind screenshotting. | Discord gateway plugin |
| `DISCORD_CONTEXT_MESSAGES` | `30` | How many prior channel messages are carried into the turn as context, all authors. | Discord gateway plugin |
| `DISCORD_TIDY_PINGS` | off | Delete the message that opened a turn once its answer is posted, so a channel keeps the work rather than the requests. Only the spellings people write (`1`, `true`, `yes`, `on`) count as on: a typo must not switch on something that deletes. | Discord gateway plugin |
| `DISCORD_MESSAGE_DELETES` | off | Contribute the `discord message delete` verb to the daemon. Off, the verb is absent from the registry rather than present and failing: a command that was never registered cannot be talked into running by a message asking for it. On, it still refuses any message the bot did not write. | Discord gateway plugin |
| `DCTL_STATE_DIR` | `~/.config/dctl` | Where the gateway keeps its own `0600` stores (`discord-allow.json`, `discord-router.json`). | Discord gateway plugin |
| `HERRSCHER_OWNER_ID` | none | Owner snowflake; the fallback the per-daemon instance id is derived from when `HERRSCHER_INSTANCE_ID` is unset. | Host |
| `HERRSCHER_STATE_DIR` | `~/.config/herrscher` | The state directory (holds `state.json`); also the root of durable agent homes (`<stateDir>/agents/<name>/`). | Host |
| `HERRSCHER_INSTANCE_ID` | none | Namespace slug for shared resources (isolates multiple instances). | Host |

## Backends

Every agent backend plugin declares the same shape, with only the prefix and a couple
of defaults change. Which one is active comes from `--vendor` then
`HERRSCHER_BACKEND` (see Transport); only the active
backend's vars are read.

| Setting | Claude | Codex | Cursor | Meaning |
|---------|--------|-------|--------|---------|
| `cmd` | `CLAUDE_CMD` (`claude`) | `CODEX_CMD` (`codex`) | `CURSOR_CMD` (`cursor-agent`) | Base command to run the agent. |
| `model` | `CLAUDE_MODEL` | `CODEX_MODEL` | `CURSOR_MODEL` | Model override. Unset ⇒ the CLI's own default. |
| `stream` | `CLAUDE_STREAM` (`true`) | `CODEX_STREAM` (`true`) | `CURSOR_STREAM` (`true`) | Streaming mode: stream-json, persistent app-server, resume-based stream respectively. For Claude and Codex, exactly the string `false` disables it. Cursor also takes `off` and every false spelling of a boolean (`0`, `f`, `false`), and refuses a value that is not a boolean. |
| `dir` | `CLAUDE_DIR` | `CODEX_DIR` | `CURSOR_DIR` | Working directory. |
| `kind` | `CLAUDE_KIND` | `CODEX_KIND` | `CURSOR_KIND` | Backend kind. |
| `effort` | no variable | `CODEX_EFFORT` | no variable | Reasoning effort override (Codex only). |

The `llm` extractor has **no env vars of its own**: it resolves the first
registered backend the same way the host does, so it reads that backend's
settings (`CLAUDE_MODEL`, `CLAUDE_CMD`, …).

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `HERRSCHER_CURATION_MODEL` | none (⇒ the backend's own model) | Overrides *any* setting whose env name ends in `MODEL` when the extractor builds its curation backend, so curation can run on a cheaper/faster model than the conversation. | `llm` extractor |

## Memory

The vault path and the per-node budget are read by the **Obsidian memory plugin**;
`AGENT_USER_BUDGET` by the **host** (durable-agent store). All are optional.

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `OBSIDIAN_VAULT` | `~/.herrscher/memory` (auto-provisioned) | Path of the durable-memory Obsidian vault. Optional: if unset, the plugin creates the vault at the default location. | Memory plugin (Obsidian) |
| `OBSIDIAN_NODE_BUDGET` | `2000` | Per-node Body budget in runes; a record that overflows raises a `BudgetError` the learner consolidates against. `0` disables. | Memory plugin (Obsidian) |
| `AGENT_USER_BUDGET` | `1500` | Rune budget for the durable `USER.md` identity/prefs file. `≤0` disables. | Host (agent store) |

## Learning / consolidation (orchestrator)

Read by the **orchestrator** when learning is enabled (an extractor is named).
Every one is optional and defaults to off or conservative, so an untuned deployment
runs the plain persist-only learner. See [Learning](sessions.md#learning) for
what each pass does.

| Variable | Default | Meaning |
|----------|---------|---------|
| `AGENT_STALE_DAYS` | `30` | Days of no re-observation before a node is marked **stale**; `≤0` disables. |
| `AGENT_ARCHIVE_DAYS` | `90` | Days of no re-observation before a node is **archived** (dropped from recall); `≤0` disables. |
| `MEMORY_MERGE_MIN` | `0` (off) | Min nodes in a domain group before the merge pass folds them into an umbrella. |
| `MEMORY_MERGE_TARGET` | `stale` | Which nodes merge considers: `stale` \| `active` \| `all`. |
| `MEMORY_MERGE_MAX` | `40` | Cap on nodes handed to the merger per domain group. |
| `MEMORY_PROMOTE_MIN_AGE_DAYS` | `0` (off) | Days a private skill must age before the curator promotes it to the shared project scope. |
| `MEMORY_REPORT_ENABLED` | `true` (on) | Write a REPORT audit node at the end of a pass that made ≥1 transition. Boolean (see below); default **on**. |
| `MEMORY_REPORT_PREFIX` | `reports/` | Key prefix each report node is written under (a timestamp is appended). |
| `MEMORY_IDLE_DAYS` | `0` (off) | Days since the last consolidation before the inactivity trigger may fire; `≤0` disables it. |
| `MEMORY_IDLE_HOURS` | `2` | Quiet period (no observed turn) required once `MEMORY_IDLE_DAYS` has elapsed; `0` removes the quiet gate. Only consulted when `MEMORY_IDLE_DAYS > 0`. |
| `MEMORY_RAW_ARCHIVE` | off | Archive one untruncated raw transcript node per turn (full-text tier), surfaced only by `memory search --raw`. Boolean (see below); default **off** (it is append-heavy). |
| `MEMORY_LEARNED_SKILLS` | off | Let the agent write its own skills with `<skill name="…">`; the host projects them into the session worktree as `SKILL.md` and using one keeps it alive. Sharing one with the project's other agents still needs an explicit `herrscher skill approve`. Boolean (see below); default **off**. |

### How the orchestrator's boolean settings parse

`MEMORY_REPORT_ENABLED`, `MEMORY_RAW_ARCHIVE` and `MEMORY_LEARNED_SKILLS` go through
**one shared parser**. They differ only in their default, never in how a value is read:

- **True**: `true`, `1`, `on`, `yes`.
- **False**: `false`, `0`, `off`, `no`.
- Matching is **case-insensitive** and surrounding **whitespace is trimmed**, so
  `TRUE` and ` on ` mean what you meant.
- **Anything else** (unset, empty, a typo, garbage) falls back to that
  setting's default (`MEMORY_REPORT_ENABLED` on, the other two off).

## Skills

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `SUPERSET_HOME` | `~/.superset` | The Superset state directory. Its existence is what the plugin takes as proof that Superset runs on this machine: absent, the plugin hands over no playbooks and the daemon says so on stderr. A leading `~/` is expanded. | herrscher-superset-skills plugin |

A skills plugin builds no port, so this is the whole of its configuration, and
unlike a gateway's token, an unresolved **required** setting on a skills plugin
skips that plugin rather than being ignored: the playbooks are its own, so their
configuration is its own too.

## Compaction (Jev)

These settings are read by the herrscher-jev-compactor
plugin, and only when `compactStack` names `jev`. A daemon whose stack does not name `jev`
never builds the stage and reads none of them.

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `TYPESAFE_API_KEY` | **required** | The TypeSafe API key. **Required as soon as `jev` is in `compactStack`**: without it, `herrscher serve` refuses to start and names the missing variable, with no fallback that would drop the stage quietly. | herrscher-jev-compactor plugin |
| `JEV_MODEL` | `jev-latest` | The Jev model asked. | herrscher-jev-compactor plugin |
| `JEV_ENDPOINT` | `https://api.typesafe.ai/v1/systemone` | The System One URL, https only. | herrscher-jev-compactor plugin |
| `JEV_KEEP_THRESHOLD` | `0.5` | Probability, 0 to 1, at or above which a call or its output stays. | herrscher-jev-compactor plugin |
| `JEV_KEEP_CALL_THRESHOLD` | `JEV_KEEP_THRESHOLD` | Probability, 0 to 1, at or above which a call stays. | herrscher-jev-compactor plugin |
| `JEV_KEEP_RESULT_THRESHOLD` | `JEV_KEEP_THRESHOLD` | Probability, 0 to 1, at or above which the full output stays. | herrscher-jev-compactor plugin |
| `JEV_RULES` | none | Per-tool actions (`keep`, `prune`, `truncate`), without asking Jev. | herrscher-jev-compactor plugin |
| `JEV_PIN_INPUTS` | none | Comma-separated substrings: a call whose input contains one always stays. | herrscher-jev-compactor plugin |
| `JEV_KEEP_ERRORS` | `false` | `true` keeps the output of a failed call. | herrscher-jev-compactor plugin |
| `JEV_INCLUDE_EMPTY_RESULTS` | `false` | `true` also judges calls whose result is empty, including calls still running. | herrscher-jev-compactor plugin |
| `JEV_MIN_RESULT_CHARS` | `0` | Outputs shorter than this many characters always stay. | herrscher-jev-compactor plugin |
| `JEV_MAX_PRUNED` | `0` | Most tool calls one compaction edits, pruned or truncated. `0` for no limit. | herrscher-jev-compactor plugin |
| `JEV_MAX_REMOVED_SHARE` | `1` | Most of the history, above 0 up to 1, one compaction may remove. Measured in `JEV_REDUCTION_UNIT`. | herrscher-jev-compactor plugin |
| `JEV_PRESERVE_RECENT` | `6` | Newest messages whose calls are pinned. | herrscher-jev-compactor plugin |
| `JEV_PRESERVE_RECENT_TOKENS` | `0` | Token budget of the newest messages whose tool calls are never pruned. `0` for none. | herrscher-jev-compactor plugin |
| `JEV_PRESERVE_TURNS` | `0` | Everything after the Nth last user message is never pruned. `0` for none. | herrscher-jev-compactor plugin |
| `JEV_PRESERVE_FIRST` | `true` | `true` keeps the first message of the history, the initial request, never pruned or abridged. | herrscher-jev-compactor plugin |
| `JEV_TRUNCATE_HEAD` | `300` | Characters kept from an output Jev lets go. `0` for none. | herrscher-jev-compactor plugin |
| `JEV_TRUNCATE_TAIL` | `0` | Characters kept from the end of an output Jev lets go. Needs herrscher-claude-backend v0.14.0 or later. | herrscher-jev-compactor plugin |
| `JEV_MIN_REDUCTION` | `0.25` | Share of the history, 0 to 1, that stops the stack. | herrscher-jev-compactor plugin |
| `JEV_REDUCTION_UNIT` | `characters` | Unit of `JEV_MIN_REDUCTION`: `characters`, or estimated `tokens`. | herrscher-jev-compactor plugin |
| `JEV_TRIGGER_PERCENT` | `0` | Share of the context window, in percent, from which the host runs Jev before `compactAt`. `0` runs it at `compactAt`. A value outside 0 to 100 is refused. | herrscher-jev-compactor plugin |
| `JEV_ON_SHORT` | `keep` | Short reduction: `keep` leaves it to the next stage, `done` stops the stack, `revert` leaves the history as it was. | herrscher-jev-compactor plugin |
| `JEV_MAX_STATE_TOKENS` | `25000` | Token budget of the state, below `JEV_MAX_REQUEST_TOKENS`. | herrscher-jev-compactor plugin |
| `JEV_MAX_REQUEST_TOKENS` | `30000` | Token budget of one request. | herrscher-jev-compactor plugin |
| `JEV_RETRIES` | `0` | Times a request is sent again after a `429` or a `5xx`, 10 at most. | herrscher-jev-compactor plugin |
| `JEV_RETRY_BACKOFF` | `1s` | First wait before a retry, doubled at each retry, capped at 30 s. | herrscher-jev-compactor plugin |
| `JEV_MAX_REQUESTS` | `0` | Most requests sent to Jev for one compaction, retries included. `0` for no cap. | herrscher-jev-compactor plugin |
| `JEV_MAX_SENT_TOKENS` | `0` | Most tokens sent to Jev for one compaction, retries included and estimated. `0` for no cap. | herrscher-jev-compactor plugin |
| `JEV_DECISION_LOG` | empty | JSONL file where each decision on a tool call is appended, with a compaction id. Created in mode 0600, moved to `<file>.1` at 10 MiB. | herrscher-jev-compactor plugin |
| `JEV_REPLAY_DIR` | empty | Directory where each compaction writes one JSON snapshot for `cmd/jev-bench`. Created in mode 0700, files in mode 0600. A snapshot holds the full history in clear. | herrscher-jev-compactor plugin |
| `JEV_INCREMENTAL` | `false` | `true` does not ask Jev again about the calls it already kept in an earlier compaction of the same session, while their content and the goal stay the same. | herrscher-jev-compactor plugin |
| `JEV_MAX_PARALLEL` | `4` | Requests sent to Jev at once, at least 1. | herrscher-jev-compactor plugin |
| `JEV_DRY_RUN` | `false` | `true` computes what Jev would prune and changes nothing. | herrscher-jev-compactor plugin |
| `JEV_STATE_INPUT_RUNES` | `1000,200,60` | Three cut levels of tool inputs in the state, longest first. | herrscher-jev-compactor plugin |
| `JEV_STATE_TEXT_HEAD` | `400` | Characters kept at the head of a long text when the state abridges it. `0` for none. | herrscher-jev-compactor plugin |
| `JEV_STATE_TEXT_TAIL` | `150` | Characters kept at the tail of a long text when the state abridges it. `0` for none. | herrscher-jev-compactor plugin |
| `JEV_STATE_CONTEXT` | built-in text | Context text Jev reads, replacing the built-in one when set. | herrscher-jev-compactor plugin |
| `JEV_STATE_STAGES` | all, in order | Reduction stages allowed, among `abridge_texts`, `collapse_messages`, `compact_calls`, `leave_out` and `merge_calls`. | herrscher-jev-compactor plugin |
| `JEV_GOAL_SOURCE` | `auto` | The goal: `auto` (the instruction, then the last prompts), `prompts`, or `fixed`. | herrscher-jev-compactor plugin |
| `JEV_GOAL_TEXT` | none | The fixed goal, required when `JEV_GOAL_SOURCE` is `fixed`. | herrscher-jev-compactor plugin |
| `JEV_GOAL_PROMPTS` | `3` | Number of the last user prompts that make the goal, at least 1. | herrscher-jev-compactor plugin |
| `JEV_GOAL_PROMPT_RUNES` | `500` | Characters kept of each prompt in the goal, at least 1. | herrscher-jev-compactor plugin |
| `JEV_TIMEOUT` | `2m0s` | Timeout of one request, as a Go duration. | herrscher-jev-compactor plugin |

## PixelLab

These settings are read by the herrscher-pixellab-backend plugin, and only when `backend: pixellab` is selected.

| Variable | Default | Meaning |
|----------|---------|---------|
| `PIXELLAB_ACCOUNTS` | none | Inline account list, `<secret> [| version] [| tier]`, separated by `;` or a newline. |
| `PIXELLAB_ACCOUNTS_FILE` | none | File holding the same list. Read when `PIXELLAB_ACCOUNTS` is empty. |
| `PIXELLAB_ASEPRITE_DIR` | `~/.config/aseprite/extensions` | Directory of the Aseprite extensions, which can supply an account. |
| `PIXELLAB_ENDPOINT` | `wss://api.pixellab.ai` | Base WebSocket URL. `ws://` only for a loopback address. |
| `PIXELLAB_DEFAULT_SIZE` | `64` | Side in pixels of an image by default, 256 at most. |
| `PIXELLAB_MAX_IMAGES` | `4` | Largest value accepted for `--n`. |
| `PIXELLAB_TIMEOUT` | `10m` | Time limit of one request, a Go duration. |
| `PIXELLAB_TURN_TIMEOUT` | `30m` | Time limit of a whole turn, a Go duration. |

## Terminal (TUI)

The session a launch opens **learns** by default: what it distils is filed under
the project it is standing in and under its own agent root, so the window you
use all day feeds what the next session knows. `TERMINAL_LEARN=false` is how you
stop it: a session that records nothing, as before.

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `TERMINAL_LEARN` | `true` | Whether the default session mounts a memory scope and an extractor at all. `false` restores a session that records nothing; the five settings below then have no effect. | Terminal gateway plugin |
| `TERMINAL_EXTRACTOR` | `llm` | Registered curation extractor that distils the transcript. A name no plugin registered means nothing is ever recorded, and `herrscher serve` says so on stderr. | Terminal gateway plugin |
| `TERMINAL_CONSOLIDATE_EVERY` | `10` | Turns between consolidation passes. `0` leaves consolidation to the manual verb and the idle trigger; an unparseable value falls back to the default rather than switching consolidation off. | Terminal gateway plugin |
| `TERMINAL_MEMORY_AGENT` | `tui` | Memory root for what the window learns *as itself*, beside the project root. Empty means no agent scope. | Terminal gateway plugin |
| `TERMINAL_PROJECT` | none (⇒ resolved from the directory) | Force the memory project instead of resolving one. Naming it also **pins** it: the session's first prompt will not revise it. The value is folded to a scope segment (`Mon Projet` ⇒ `mon-projet`) rather than refused, so a typed setting cannot be the reason a window opens on nothing. | Terminal gateway plugin |
| `TERMINAL_PROJECT_PIN` | `first-turn` | When the project is settled. `first-turn` takes the directory as a guess the first prompt may revise against the projects the vault already holds; `launch` takes the directory as final. | Terminal gateway plugin |

The directory's answer comes from `git rev-parse --git-common-dir`, so every
worktree of one repository lands in the same project rather than one project per
worktree. Outside a repository there is no project, only the agent root.

A memory root is not a location: `TERMINAL_PROJECT` names where what the session
learns is filed, and never moves where the session runs.

## Forge (GitHub)

These variables are read by the GitHub forge when no token was saved by
`herrscher connect github`. The connection itself is described in
[GitHub connection](github.md).

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `GH_TOKEN` | unset | A GitHub token used for `github.com`, or for the host named by `GH_HOST`. Ignored for any other host. Blank values are ignored. | Forge credentials |
| `GITHUB_TOKEN` | unset | Used when `GH_TOKEN` is unset, with the same host rule. | Forge credentials |
| `GH_HOST` | unset | A GitHub host, besides `github.com`, that `GH_TOKEN` and `GITHUB_TOKEN` apply to, for example `ghe.example.com`. Unset, they apply to `github.com` only. Also listed by `herrscher connect status`. | Forge credentials |

## Host: logging and env file

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `HERRSCHER_LOG` | `info` | Structured (`log/slog`) log level on stderr: `debug\|info\|warn\|error`. The bridge's `-v` flag forces `debug`. | Host + bridge |
| `HERRSCHER_LIMITS_TIMEOUT` | `10s` | Cap of one reading of an account's limits, counted from its start, as a Go duration between `1s` and `5m`. Read when the daemon starts, so a change takes a restart. A malformed or out of range value is reported in the daemon log and the default applies. | Host |
| `HERRSCHER_ENV_FILE` | `./.env` | Path of the `.env` file to load. Resolved to an absolute path and re-exported, so each `bridge` subprocess (whose cwd is a session worktree) loads the *same* file. An implicit `./.env` is best-effort; an explicit `$HERRSCHER_ENV_FILE` is authoritative (a load failure is fatal). | Host |

## Turn telemetry (OpenTelemetry)

The `config.json` key `telemetry.enabled` takes priority over the environment:
`false` turns the export off even when an endpoint is set, and a missing key or `true`
leaves the decision to the variables below. Use the key when the daemon runs as a
service, since a service unit usually carries only `PATH`.

```json
{
  "telemetry": {
    "enabled": false
  }
}
```

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset (export off) | The collector endpoint. Without one, the other telemetry variables enable nothing. | Host |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | unset | The traces endpoint, used exactly as written. Wins over `OTEL_EXPORTER_OTLP_ENDPOINT` when both are set. | Host |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | `http/protobuf` or `grpc`. | Host |
| `OTEL_SERVICE_NAME` | `herrscher` | The service name. | Host |
| `OTEL_SDK_DISABLED` | unset | `true` turns the export off even when an endpoint is set. | Host |

The other `OTEL_EXPORTER_OTLP_*` and `OTEL_BSP_*` variables set headers, timeouts and
batching. The endpoint rules, the span contents and the error types are described in the
[README](../README.md#turn-telemetry-opentelemetry).

## Set by the daemon, not by you

These are written into a `bridge` child's environment by the daemon that spawns it.
Setting them yourself changes nothing an operator wants changed. They are listed so an
unfamiliar name in a process environment can be identified.

| Variable | Meaning | Written by |
|----------|---------|------------|
| `HERRSCHER_CAPABILITIES` | The summary of the verbs the supervising daemon dispatches, one line per verb family. The bridge turns it into the `<capabilities>` block of every turn's prompt, so the agent knows it can reach the daemon as `herrscher <verb>` instead of reaching for a token of its own. Built after the registry, so it includes gateway-contributed verbs. | Host (`serve`) → bridge |
| `HERRSCHER_SESSION` | Which session a process spawned inside it belongs to. The [approval](approvals.md) hook is what needs it: a vendor CLI's own session id is the vendor's, and the daemon knows nothing about it. A hook that does not find this variable allows the tool call and says so on stderr. | Host (`serve`) → bridge |
| `HERRSCHER_COMMAND_SOCKET` | Where `herrscher <verb>` dials the daemon. Only a session on a [host](remote.md) gets it: the socket is forwarded back over ssh under a path named after the session, which the far side cannot derive on its own. Empty locally, where the path follows from the instance id. | Host (`serve`) → remote bridge |

The gateway credentials travel the same way, and for a stricter reason: the daemon
**removes** them from its own environment at startup so no vendor CLI can read
them, then hands them back to the bridge (the same trusted binary) through its
environment rather than its argv, since `/proc/<pid>/cmdline` is world-readable.

ssh carries no environment, so a bridge on a host is started with `--env-stdin` and reads
that whole block on its standard input instead, credentials and capability summary alike,
plus `HERRSCHER_COMMAND_SOCKET`, `HERRSCHER_INSTANCE_ID` and a `TMPDIR` of `/tmp`. The
reason is the one above: argv would be world-readable over there too.

## Distributed transport (opt-in)

Distributed transport lets certain categories run as separate processes over NATS
(discovery) + gRPC (port calls). In-process stays the default. See
[Plugins on another machine](remote.md#plugins-on-another-machine).

| Variable | Default | Meaning | Read by |
|----------|---------|---------|---------|
| `HERRSCHER_BACKEND` | none (⇒ default) | Agent backend vendor when a session doesn't set one via `--vendor`: `claude\|codex\|cursor`. Precedence: `session.Vendor` → `HERRSCHER_BACKEND` → default. | Host |
| `HERRSCHER_REMOTE` | none (⇒ all in-process) | Comma-separated categories to run out-of-process: `memory`, `orchestrator`, `backend`. | Host |
| `HERRSCHER_NATS` | `nats://127.0.0.1:4222` | The NATS server URL (required in remote mode). | Host (transport) |
| `HERRSCHER_ANNOUNCE_KEY` | none | Shared secret of at least 32 random bytes that signs the announcements a plugin-host publishes. Every host and plugin-host must hold the same one, and a host with a remote category refuses to start without a valid key. See [Plugins on another machine](remote.md#plugins-on-another-machine). | Host and plugin-host (transport) |
| `HERRSCHER_BIND` | `127.0.0.1:0` | The plugin-host bind, for multi-machine (take it off loopback). | Host (transport) |
| `HERRSCHER_TLS_CA` | none | The mTLS CA. All three `HERRSCHER_TLS_*` together or none, and a partial set fails closed. | Host (transport) |
| `HERRSCHER_TLS_CERT` | none | The mTLS certificate. | Host (transport) |
| `HERRSCHER_TLS_KEY` | none | The mTLS key. | Host (transport) |

With TLS unset the transport stays plaintext loopback, exactly as a single-host
deployment.

## Related files

The service sources its secrets from a separate `0600` file so they never live in
the unit:

- `~/.config/herrscher/herrscher.env`, the secrets file the service sources
  (`DISCORD_BOT_TOKEN=`, `DISCORD_USER_ID=`).
- root `.env`, auto-loaded by the host; seeded from `.env.example` by
  `herrscher init`.
- `~/.config/herrscher/config.json`, the config template.
