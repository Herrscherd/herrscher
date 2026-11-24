# Memory bench

The memory bench measures the memory settings against each other. It runs the
same tasks with a setting off and then on, each arm in its own isolated state
directory, and reports what changed: success, turns, tokens, the memory tokens
added per turn, and false recall.

It is an on-demand tool under `tools/membench`. It is never run by CI, and
`go test ./...` only runs its unit tests, which make no model call. Running the
bench itself reaches a real model and spends money, so it requires a budget.

## Running it

Build the bench, then start it with a budget. The budget is required: the bench
refuses to start without `-budget-tokens` or `-budget-usd`.

```sh
go build -o "$BIN_DIR/membench" ./tools/membench
"$BIN_DIR/membench" -herrscher herrscher -budget-tokens 2000000 -format md
```

Cap the memory of the build itself as the rest of the repository does, one
heavy tool at a time.

| Flag | Default | Meaning |
|------|---------|---------|
| `-herrscher` | `herrscher` | The binary to run, looked up on `PATH` or given as a path. |
| `-model` | `claude-haiku-5-5` | The model the runs use, passed as `HERRSCHER_DEFAULT_MODEL`. |
| `-budget-tokens` | none | Ceiling in tokens (input, output and cache tokens of the ledger). |
| `-budget-usd` | none | Ceiling in US dollars, from the cost the ledger records. |
| `-reserve-tokens` | `200000` | Tokens reserved before each call, and the floor of every projection. Must be positive and not above `-budget-tokens`. |
| `-reserve-usd` | `0.5` | Dollars reserved before each call, and the floor of every projection. Must be positive and not above `-budget-usd`. |
| `-credentials` | none | A credentials file copied into each run's config directory, mode 0600. |
| `-api-key-env` | none | Name of an environment variable holding an API key passed to each run. Unset by default, so a run uses only `-credentials`. The key is never put on the command line and is redacted from a failed run's stderr. |
| `-dataset` | none | A LongMemEval JSON file. Without it only the pair suite runs. |
| `-samples` | `10` | How many dataset items to sample. |
| `-seed` | `1` | Seed of the sampling, so the same seed samples the same items. |
| `-settings` | `all` | `all`, or a comma list of `learned-skills`, `raw-archive`, `report`. |
| `-max-turns` | `6` | Longest run kept from the dataset, in turns. At least 3. |
| `-format` | `md` | `md` for a markdown report, `json` for the machine-readable one. |
| `-lang` | from the environment | `en` or `fr`, the language of the report. |

The settings are the environment variables `MEMORY_LEARNED_SKILLS`,
`MEMORY_RAW_ARCHIVE` and `MEMORY_REPORT_ENABLED`. Each is set to `true` in the
on arm and `false` in the off arm.

Exit codes: `0` complete, `1` failed, `2` usage error, `3` partial (the budget
stopped the run before every arm ran).

A failed run still writes its report. It keeps the arms that finished before
the error, marks the status as failed with the cause, and exits `1`.

## What runs

The pair suite has four tasks, each taught with one or more `Remember this:`
steps and then asked:

| Task | What it checks |
|------|----------------|
| same agent | A fact taught to one agent is recalled by that agent. |
| other agent | A fact taught to one agent is recalled by another. |
| stale | A fact is corrected, and the corrected value is recalled, not the old one. A recall of the old value is false recall. |
| never given | A fact that was never taught is not invented. Saying so is success; giving the secret the bench plants is false recall. |

With `-dataset`, the bench samples items of a LongMemEval file. Each item's
evidence sessions are taught in order, then its question is asked. The five
capabilities are extraction, multi-session, temporal reasoning, knowledge
update and abstention. An item is eligible when it has evidence sessions, fits
within `-max-turns`, has a known question type, and has an answer (abstention
items may have none).

The file is streamed, never loaded whole, and it is capped at 512 MiB and
100000 items. The sample keeps the items with the lowest seeded hash of their
id, so the same seed and file always give the same items. The dataset is not
bundled: pass its path.

## What is measured

For each task and setting, the report has one row with an off cell and an on
cell, each with its runs, successes, false recalls, turns and tokens.

- **Success**: the reply contains every expected value as whole words and no
  forbidden one, or, for abstention, says it does not know without giving the
  forbidden value. Matching is by whole word, so `42` does not match `1042`.
- **Turns**: the calls of a run, one usage record each.
- **Tokens to success**: the tokens of all runs divided by their successes. The
  delta is on minus off.
- **Memory tokens per turn**: input tokens per turn on minus off. This is the
  cost the memory adds to each call.
- **False recall**: a forbidden value (the stale answer, the planted secret) is
  given. It is counted, not averaged into success. The LongMemEval section does
  not report it, because its reference answers do not mark forbidden values.

The LongMemEval scoring is a whole-word check of the reference answer in the
reply. It is a proxy for the official judge, not a replacement for it.

## Budget semantics

Spend is read from the usage ledger of each run's state directory after every
call. Before each call the bench projects the cost of the next call as the
larger of the operator reserve and the widest call seen so far. It does not
start the call when spent plus that projection reaches the ceiling. When it
stops, it discards the arm that was running, marks the run partial, and counts
the arms it did not run in the report.

The reserve is set with `-reserve-tokens` (default 200000) and `-reserve-usd`
(default 0.5). It is charged before the first call and stays as a floor for
every later one, so the first call is reserved too. A reserve above the ceiling
is refused at startup, since no call could then start.

The guarantee is that the ceiling is exceeded only when a single call costs more
than the reserve. The reserve is an operator estimate of what one call can cost,
so a call wider than it can carry the spend past the ceiling by the excess.

## Isolation

Each arm runs in a new directory under a temporary parent that is removed at the
end. The parent lives under the user cache directory. The directory holds the
state, the home and the config of that run, and the run's environment is built
from nothing: only `PATH` is inherited, the state, home and config variables
point into the arm's directory, and `HERRSCHER_INSTANCE_ID` is unique to the
arm.

`ANTHROPIC_API_KEY` is never read from the caller's environment by default. A run
uses the credentials given with `-credentials`. The key is passed to each run
only when `-api-key-env` names the variable that holds it. It travels in the
run's environment, never on the command line, and the bench redacts it from the
stderr of a failed run before that stderr goes into the error.

## Tests

The unit tests run under `go test ./...`. They use no model: the budget, the
scoring, the sampling, the report and the sandbox environment are tested
directly.

Three end-to-end tests run under `-tags e2e` in `scripts/check.sh`. They build
the real `herrscher` and the fake `claude` of `e2e/vendortest`, then:

- `TestMemorySettingReachesTheRealBinary` runs `herrscher --print` with
  `MEMORY_RAW_ARCHIVE` set to `false`, then to `true`, and checks that the raw
  archive is written only in the second run. It fails if the bench does not
  forward the setting to the binary.
- `TestBenchMeasuresEachArmFromTheLedger` runs the task pair against the real
  binary, one arm off and one on. Each arm's turns, tokens and input come from
  the usage ledger the local `--print` writes, so the test fails if a run does
  not record its usage.
- `TestBenchStopsWithoutAUsageLedger` runs one arm against a binary that writes
  no ledger. The bench must stop with the missing-ledger error, keep no outcome,
  and mark the run stopped.

The budget, isolation and scoring rules were checked by breaking them on
purpose. Each break was caught by a named test: the sandbox dropping the
setting assignments fails `TestMemorySettingReachesTheRealBinary`, a reserve
that ignores the reserve fails `TestMayStartKeepsTheReserveInFront`, and a
scorer matching substrings fails `TestScoreMatchesWholeWordsOnly`.

## Lint exclusions

The only gosec finding excluded by path for this package is G204 in
`tools/membench/runner.go`: the binary run is the one the operator names with
`-herrscher`, and running it is the purpose of the tool. The G101 exclusion was
removed: the catalogue keys and flag names that tripped it are renamed, and their
values are unchanged.

The repository-wide exclusion of G304 and G703 under `tools/` still applies to
this package. The bench reads the dataset and credentials paths the operator
gives, which is the reason for that rule.

## Limitations

- The bench reads spend from the ledger of the binary it runs. A binary that
  writes no ledger stops the run with the missing-ledger error, as the
  `TestBenchStopsWithoutAUsageLedger` test checks.
- The settings gated by time of day are not exercised by the bench.
- The effect of `HERRSCHER_OWNER_ID` on `--print` is not verified; the bench
  sets it, but its result is not measured.
- The layout of the usage ledger is read from `core/usage`; a change there must
  be followed here.
- Sample sizes are small, so one run difference can move a rate by a lot. The
  report gives counts beside the rates for that reason. It has no confidence
  intervals.
- The ceiling can be exceeded by the part of a single call that is wider than
  the reserve, as described above.
