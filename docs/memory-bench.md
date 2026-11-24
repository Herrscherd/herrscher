# Memory bench

The memory bench measures the memory settings against each other. It runs the
same tasks with a setting off and then on, each arm in its own isolated state
directory, and reports what changed: success, turns, tokens, the memory tokens
added per turn, and false recall.

It is an on-demand tool under `tools/membench`. It is never run by CI, and
`go test ./...` only runs its unit and fake-vendor tests, which make no real
model call. Running the bench itself reaches a real model and spends money, so
it requires a budget.

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
| `-credentials` | none | A credentials file copied into each run's config directory, mode 0600. |
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
items may have none). The sample is drawn with a seeded reservoir, so the file
is streamed and never loaded whole. The dataset is not bundled: pass its path.

## What is measured

For each task and setting, the report has one row with an off cell and an on
cell, each with its runs, successes, false recalls, turns and tokens.

- **Success**: the reply contains every expected value and no forbidden one, or,
  for abstention, says it does not know without giving the forbidden value.
- **Turns**: the calls of a run, one usage record each.
- **Tokens to success**: the tokens of all runs divided by their successes. The
  delta is on minus off.
- **Memory tokens per turn**: input tokens per turn on minus off. This is the
  cost the memory adds to each call.
- **False recall**: a forbidden value (the stale answer, the planted secret) is
  given. It is counted, not averaged into success.

The LongMemEval scoring is a substring check of the reference answer in the
reply. It is a proxy for the official judge, not a replacement for it.

## Budget semantics

Spend is read from the usage ledger of each run's state directory after every
call. Before each call the bench projects the spend with two of the widest calls
seen so far: if spent plus twice the widest call reaches the ceiling, it does
not start the call. When it stops, it discards the arm that was running, marks
the run partial, and counts the arms it did not run in the report.

Two limits follow from this. The first call cannot be projected, so it can
overshoot the ceiling by one call. A single call wider than the reserve can
also carry the spend past the ceiling, because the reserve is an estimate from
the calls already made, not a guarantee.

## Isolation

Each arm runs in a new directory under a temporary parent that is removed at the
end. The directory holds the state, the home and the config of that run, and
the run's environment is built from nothing: only `PATH` is inherited, the
state, home and config variables point into the arm's directory, and
`HERRSCHER_INSTANCE_ID` is unique to the arm. `ANTHROPIC_API_KEY` is never
passed, so a run uses only the credentials given with `-credentials`.

## Tests

The unit tests and the fake-vendor tests run under `go test ./...`. The fake
vendor is the test binary itself, re-entered through a shell wrapper that
stands in for `herrscher`: it keeps the facts and the usage ledger in the state
directory the bench gives it, so the whole pipeline runs without a model call.

The budget, isolation and scoring rules were checked by breaking them on
purpose: a reserve that ignores the widest call, an environment that inherits
the caller's secrets, a scorer that accepts a forbidden answer, and an
abstention that counts as success. Each break was caught by a named test.

## Lint exclusions

Three gosec findings are excluded by path in `.golangci.yml`, each because the
code is right and the rule does not apply:

- G204 in `tools/membench/runner.go`: the binary run is the one the operator
  names with `-herrscher`, and running it is the purpose of the tool.
- G404 in `tools/membench/longmemeval.go`: the reservoir sampling uses a seeded
  PCG generator, so a seed reproduces the sample. It is not a security boundary.
- G101 in `core/clitext/keys_membench.go`: the names are catalogue keys
  (`cli.membench.*`), not credentials.

## Limitations

- The settings gated by time of day are not exercised by the bench.
- The effect of `HERRSCHER_OWNER_ID` on `--print` is not verified; the bench
  sets it, but its result is not measured.
- The layout of the usage ledger is read from `core/usage`; a change there must
  be followed here.
- Sample sizes are small, so one run difference can move a rate by a lot. The
  report gives counts beside the rates for that reason. It has no confidence
  intervals.
- The first call can overshoot the ceiling by one call, as described above.
