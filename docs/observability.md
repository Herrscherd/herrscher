# Observability

## Following a session from any client

`herrscher session watch --name <session>` follows the session's current turn in the
terminal: the prompt and who sent it, each tool call as its status line when it starts and as a
line when it ends (`✓` with its duration and tokens, or `✗` with its duration and error,
or `no reason given` when the backend gave none), the reply as it streams,
and the tokens and cost when the turn ends. In session watch, each call prints its finished line at its own
done. A call still open when the turn ends (the reply arrives, the turn is abandoned, or the
session closes) shows once as `…` with `unfinished`. Only session watch prints that line at a
session close: the TUI removes the session's tab on close, so it shows nothing for open calls then.
A gap in the daemon's memory does not close a call: when its done arrives later, session watch
prints `✓` or `✗` as usual. The bound notice appears once, when a turn goes past
4096 tool calls, and only the first 4096 are kept. It says when the session is idle or when
the daemon holds nothing of it yet, keeps following the next turns, follows the
session under its new name when it is renamed, and stops when the session is closed
or archived, or when it is interrupted. `--json` prints one event per line, the
same events the daemon sends on its events socket, so a script can consume them. A
tool call travels as `tool_call` on the `status` event that starts it and on the
`tool_done` event that ends it. The two lines are the only ones a call prints: the
`tool_done` event never prints a second status line.

The TUI shows the same calls differently. Each call shows its status line when it starts, but its
finished line waits: the TUI prints the finished calls together only when a gap in the daemon's
memory expires, or when the turn ends, before the reply. A call still open at the turn end shows
once as `…` with `unfinished`, as in session watch.

Every event the daemon publishes carries a sequence number (`seq`). It starts from
the clock when the daemon starts, so a new daemon always numbers above the one
before it. The daemon keeps the latest events of each session in memory: at most 512
entries and 1 MiB of cost per session, for the 64 sessions that spoke most recently,
with the text of a streamed reply folded into one entry. The cost of an entry is its
text, plus the `id`, `tool`, `detail` and `error` of its tool call, plus 128 bytes of
overhead. The `detail` and the `error` are cut to 4096 bytes before they are kept. The
bridge refuses a tool call whose `id` or `tool` is longer than 256 bytes: the event
keeps its text and loses its call.

Memory budget of one turn in a client (an estimate, not measured): 4096 calls × 2 fields
(`detail` and `error`) × 4096 bytes ≈ 32 MiB, plus the `id` and the `tool` of each call (at
most 256 bytes each, so at most 2 MiB for 4096 calls), so about 34 MiB before the data
structures themselves.

The catch-up happens on the events socket itself. A subscriber may write one line as
soon as it connects, `{"session":"<name>","since":<seq>}`, and the daemon first
sends what that session published after the seq, then a `caught_up` event whose
value says whether the turn is `running`, `idle`, or `empty` (nothing held), then
the live events, with nothing lost and nothing repeated in between. Without `since`,
the replay starts at the prompt of the current turn. When the daemon no longer holds
every event after the seq (it restarted, trimmed the session, or evicted it), the
replay opens with a `gap` event of value `expired`. A subscriber too slow to read
its lines loses some, and the daemon then sends a `gap` event of value `dropped`:
`session watch` reconnects with the last seq it saw and misses nothing. A
coordination event jumps the queue, so it can arrive before lines with a lower seq;
`session watch` shows it once and still shows those lines. It remembers the last 256
such events, so past that many in a row with no other line, a resume can show one
twice. When the daemon goes away, `session watch` prints that seq, to run `session
watch --since <seq>` once the daemon is back. A subscriber that writes nothing
within 200 ms receives the live events only. A line cut by that deadline, a line
that is not a cursor, or a cursor that arrives after it gets a `dropped` gap, so
`session watch` reconnects and sends it again. The `session watch` verb, called
through the command socket by another client, returns the same replay as a JSON
array and does not follow.

`herrscher session history --name <session>` replays the past turns from the
session's transcript, which survives restarts: for each turn its id and start time,
the prompt, the reply, the notices, and the duration, tokens and cost.
`--since <turn>` keeps the turns after that one, and an id the transcript does not
hold is an error, as is a session the daemon does not know. `--json` prints the
turns as an array. Each turn carries a `tools` array, one entry per tool call in the
order they started, with `id`, `tool`, `detail` (omitted when empty), `state`
(`started`, `succeeded` or `failed`), `started_at`, `ended_at` and `duration_ms`
(the last two omitted while the call is open), `error` (on a failed call) and
`tokens`. `tokens` counts the output tokens between the previous tool event of the
turn and this call's start, and is zero for the first call. A backend that reports
fewer tokens than the turn had can make it negative: `--json` keeps that raw value,
while the text form and the terminal UI show it as zero. A call that never got
its outcome keeps the state `started` and has no end. The text form lists the calls
before the usage line of the turn, with the same `✓`, `✗` and `…` lines as the watch.
A turn with tool calls and no reply is kept. The `detail` and the `error` of a call are
kept up to 4096 bytes: a longer one is cut at the last whole character under that bound,
and the rest is dropped. Within that bound they are stored verbatim: the `detail` is what
the backend reported, and it is written as it is to the session's local transcript, a file
created with mode `0600` and read back by `session history`. `session watch` shows the same
values from the events the daemon keeps in memory. Both verbs need the same right, the
`observer` role or above, the one that already lets a user read the turns. A `detail` can
carry sensitive arguments, such as a path, a command or a query, so grant that role only to
people who may read the session's content.

The terminal UI shows the same lines at the end of each turn, before the reply, for
the turns it follows live. Past turns replayed from the transcript do not get their
tool lines in the terminal UI yet.

## Health, one command away

`herrscher health` asks the running daemon for a snapshot of everything it depends
on, and prints it as a table grouped by what the checks cover: the daemon and its
key proxy, the gateways, the backends with each account and profile, the
compaction stages, the memory plugins and the key pools. Each line says the state
(ok, degraded, down or off), how long the probe took, and the facts the host knows
about the service. Each compaction stage shows when it last ran and how it ended
(done, short, failed, dry run or skipped), read from the usage ledger, or that there
was no compaction in the last 7 days. When the ledger cannot be read, the line says
the last compaction is unknown when no readable run is left to show, and flags the
data as partial. A compaction stage that reads its API key from the environment also
shows whether that variable is set, by its name only (`TYPESAFE_API_KEY`), never its
value, with the time and outcome of its last successful run and the time and note of
its last failed one.

A gateway the daemon does not run reads as off, and so does a memory plugin that is
not the one in use: the first local memory plugin with a factory is the live one,
so in remote mode (`HERRSCHER_REMOTE=memory`) the local memories show off.

`herrscher health <service>` shows one service, or every account of it, with its
cause and the remedy when there is one, for instance the login command to run.
A service is named by its plugin, or by `plugin:account` for one account of a
backend. `--json` prints the same snapshot as structured data for a script.

Each probe runs under a bounded pool with a five second limit. `--deep` lifts the
limit to thirty seconds and asks the plugins that support it for a real round trip
rather than a local check, which costs requests, so it is opt-in. `--watch`
refreshes the table every fifteen seconds until interrupted, and `--quiet` prints
nothing and only sets the exit code.

The exit code tells a script the outcome: `0` when nothing is degraded or down,
`2` when the worst state is degraded, and `1` when something is down or the
daemon cannot be reached. An unreachable daemon is reported on stderr with the
socket it was looked for, so a stopped daemon reads as a finding rather than a
silent failure.

## Usage, from the ledger

`herrscher usage` asks the running daemon for the figures it recorded in the usage
ledger, so the daemon must be up. A one-shot run without a daemon, such as
`herrscher --print` or `herrscher session seed`, writes its usage to the same ledger
through the same recorder as the daemon. When a daemon is up, the one-shot is
forwarded to it and recorded once, by the daemon. A view is named after the command,
and the default is the limits of each account:

- `limits`: the quota windows of each account.
- `cost`: the spend of the period, by session and by model.
- `tokens`: the tokens in and out of the period.
- `compaction`: each compaction stage, with its runs and how they ended (done,
  short, failed, dry run, skipped), the calls it examined, kept, truncated and
  pruned, the share of characters it removed, the tokens it removed, its model
  requests, their average duration and the estimated saving. Above the stages, the
  Jev share gives the part of Jev runs that succeeded (done or short), and the part of
  sessions where the backend stage ran behind Jev, linked to the Jev run by the
  passage both stages share. A Jev run is an attempt: its outcome is done, short or
  failed, so a dry run or a skipped Jev is in neither count. A session counts once it
  has an attempt. The backend stage counts as behind Jev when its outcome is anything
  but skipped, so a dry run of the backend does count.

`--range` takes `24h`, `7d`, `30d` or `90d`, and `--account` and `--vendor` narrow the
figures to one account or one vendor, with the same rule as for turns: a compaction is
kept when its vendor and account match the filter. A compaction recorded before the
account was stored has no account, so it is counted when no filter is set and left out
by a filter it cannot match, as an unaccounted turn is. `--json` prints the same
figures as structured data for a script.

The turns read from the transcripts are priced from the LiteLLM price list. A request whose context (input, cache
read and cache write together) goes past a long context threshold of its model is priced
at the long context rates the list gives for that threshold, for example the
`input_cost_per_token_above_200k_tokens` of a model billed higher past 200k tokens. A
rate the list leaves out for that threshold stays at the base rate.

The limits view reads the quota windows of every account in parallel, and a reading
shared by several callers stops after ten seconds: a vendor slower than that shows a
failed reading. `HERRSCHER_LIMITS_TIMEOUT`, read by the daemon when it starts, sets
another cap as a Go duration between `1s` and `5m` (`30s`, `1m`), so a change takes a
restart of the daemon. A malformed value or one out of those bounds is reported in the
daemon log and the default applies. The cap is counted from the start of the reading, and
a reading that ends before it is kept even when its callers gave up, so the next call
shows it.

The transcript index assumes a transcript only grows: a file read before resumes at the
offset it was left at. The first start after an upgrade that changes the index format
reads every transcript of the retention again, once.

The estimated saving of a compaction is the tokens it removed, priced at the cache-read
rate of the session's model in the LiteLLM price list, counted once per compaction. The
session model is stored with the record when the compaction runs. A model missing from
the price list shows `unknown price` and never `$0`; a model listed without a cache-read
rate is priced at its input rate, as for turns. When only some
compactions are unpriced, the line gives the amount and how many runs have no price.

The fast speed is not priced for compactions. The turns are priced at the fast rate
when their transcript says the fast tier was used, but a compaction record carries no
speed: the session does not expose it to the host when the compaction runs. The saving
is therefore priced at the standard cache-read rate even when the session ran fast.

A failed compaction keeps the class of its error in the ledger, never its text: an
unknown error is recorded as `unknown failure`, a stage that failed without an error
as `fell back`. `herrscher health` shows that class in words. A note written by an
earlier version as free text is never shown: it reads as an unknown failure.

## Model routing

Each backend declares the models it offers in its manifest, so the catalog is
queryable without instantiating anything. A route policy picks which of them a
build may use. `all` runs on this machine's own vendor logins. `gateway-only`
runs every turn through a gateway you supply, and refuses the two ways around the
catalog: a free-form `--cmd`, or a session with no model at all.

When a session names no model, the first of these that fits applies:

1. the session's own `--model`;
2. the agent's model, read from the `model` file in its home;
3. the vendor's default, from `HERRSCHER_DEFAULT_MODEL_<VENDOR>` (for example `HERRSCHER_DEFAULT_MODEL_CLAUDE`);
4. the global default, from `HERRSCHER_DEFAULT_MODEL`;
5. the backend's own default, when none of the above is set.

A fallback that belongs to another vendor than the one requested is skipped, and
logged unless it is the global default. An explicit `--model` that the catalog
refuses is an error.

Backends that can list their models are asked at startup, in the background, and
their answers are cached on disk. Discovered models are added to the static
catalog; they never remove one. `herrscher models list` shows where each model
comes from (`catalog` or `discovered`), and `herrscher models refresh` asks again
right away. Both take `--state FILE`, the daemon state file whose directory holds the
cache; without it they use the daemon default, the same cache the daemon reads.

A model that a backend declares `Hidden`, or that `HERRSCHER_MODELS_HIDDEN` names, is
left out of the selector and of `herrscher models list`, but it still resolves: a
session recorded on it resumes it, and an explicit `--model` naming it works. Each
time the host resolves such a model it logs a warning, and it never switches to
another model in its place.

`herrscher models list --json` gives each model its prices and its window. `--all`
also lists the hidden models, marked `hidden: true`, which still resolve when requested;
without it the output is unchanged. Prices
are in USD per million tokens (`inputPrice`, `outputPrice`, `cacheReadPrice`,
`cacheWritePrice`), and a price of 0 means the price is unknown, never free.
`contextWindow` is the model's window in tokens, 0 when its backend declares none.
A turn's cost is the one its backend reports. When the backend reports none, the host
estimates it from the turn's tokens and the resolved model's prices: uncached input,
output, cache read and cache write, each times its price, summed and divided by
1000000. The estimate is stored as the turn's cost, so the transcript, the cost line
and `CostCap` all count it. It is computed only when every category the turn used has
a known price: a turn that uses a category priced 0 keeps no estimate and shows no
cost, so a partial figure is never shown as a cost. A gateway route has no prices and
gets no estimate. Claude reports its cost on each turn's result, so the estimate is a
fallback for it. Claude's `input_tokens` leaves out cache, which is why the estimate
prices cache separately. Codex and cursor report neither token counts nor a cost, so
their turns show no cost.

| Variable | Default | Effect |
|---|---|---|
| `HERRSCHER_MODELS_DISCOVER` | every backend | comma-separated vendors that may be asked (`claude,codex`), or `none`; with `HERRSCHER_ROUTE_POLICY=gateway-only` vendors are asked only through the gateway, and only when its credentials are set |
| `HERRSCHER_MODELS_TTL` | `24h` | how long a cached list is kept before it is asked again |
| `HERRSCHER_MODELS_TIMEOUT` | `15s` | longest wait for one vendor's answer |
| `HERRSCHER_MODELS_HIDDEN` | none | comma-separated model IDs hidden from the list and the selector; a session already on one still resumes it, with a warning |
| `HERRSCHER_DEFAULT_MODEL_<VENDOR>` | none | default model of one vendor |

The cache is `models-discovered.json`, next to the daemon state file: the directory of
`--state` when given, otherwise `HERRSCHER_STATE_DIR`, otherwise `~/.config/herrscher`.
It is created on first write, readable by its owner only. Writers take an exclusive lock
(`flock` on Unix, `LockFileEx` on Windows), so two writers never drop each other's vendor.
A vendor that fails or stays silent past its timeout keeps its last known list, and the
daemon logs a warning. The daemon never waits for discovery to start.
A bridge started by a local daemon receives that daemon's `--state` and reads its cache; a
remote bridge reads the cache of its own machine and receives no `--state`. A clash warning
is logged once per model ID for the life of the process, and each bridge keeps one
model discovery for its whole life. Under `gateway-only` the host drops every native answer
whatever the backend returned; a backend that ignores the policy would still launch its own
CLI, which only that backend's gate prevents.

## Also

Cross-backend skills, where one `SKILL.md` works on every backend. The playbooks
come from the plugins in the build and are installed into `~/.claude/skills`,
kept current, because a playbook is instructions and a stale one is the agent
following last release's rules. A copy still bytewise the one we wrote is
replaced when the shipped text changes, and removed when no build ships it any
more. A copy you edited is left alone and named at startup, so you learn the
rewrite is not in effect instead of finding your version gone. See
[Cross-backend skills](skills.md).

And per-node memory budgets, in [Budgets](budgets.md).

Each settled turn can also be exported to an OpenTelemetry collector, following the GenAI
semantic conventions. The export is off unless an endpoint is configured or the
`telemetry.enabled` key allows it; see [Turn telemetry](env.md#turn-telemetry-opentelemetry).

