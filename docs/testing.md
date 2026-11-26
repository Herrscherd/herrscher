# Testing

Tests must pass an explicit `Layout` (`testRuntime` or `isolatedRuntimeDir`) to every
function that reaches a socket or a lock. A zero `Layout` names no path, and its checks
return an error. `runtimedir` exports neither the roots of the production runtime
directory nor a resolver, so code outside the package cannot build one.

`internal/productionlayout` checks the rest in `go test ./...`. It parses every `.go`
file of the module with `go/parser`, hidden directories included (only `.git` and
`node_modules` are skipped), and fails when:

- a file other than `main.go`, `core/host/runtime_dir.go` and the unix socket wrappers names `ProductionRuntime`;
- a file other than `core/host/runtime_dir.go` and the runtimedir package names `Production`;
- a test names `CommandSocketPath` or `EventsSocketPath`, except the one read-only contract test;
- a production file names `CommandSocketPath` or `EventsSocketPath`, except `main.go` and the two wrapper files;
- a file that imports runtimedir names `Roots`, `ProductionRoots`, `Resolve` or `DirOf`, even through a dot import;
- a production file other than `core/host/runtime_dir.go` and the runtimedir package itself refers to `runtimedir.At` or `host.RuntimeAt`, called or not, under whatever local name the file imports them (an alias or a dot import included, the import path decides), or refers to a bare `RuntimeAt` in `core/host` or in a file that dot-imports `core/host`. Production code builds no layout from an arbitrary path: only tests, that one file and the runtimedir package may. The production layout comes from `ProductionRuntime`, which only `main.go` and the socket wrappers call.

What it cannot detect: it matches identifiers by name in the syntax tree, so it does not
see a socket path built from a string, a call made through reflection, code outside this
module, or a hand-written path such as `/run/user/1000`. Those are not covered by the
checker and rely on review.

One more gap: the arbitrary path rule matches calls only, so a reference without a call
such as `f := runtimedir.At` is not seen.

## Scenarios on the real binary

`e2e/` drives the built `herrscher` binary from its command line, with no network and
no vendor account. `scripts/check.sh` runs it on Linux, and says it skips the step
anywhere else. That step runs three `go test` invocations under the `e2e` build tag: `./e2e/...`
(the testscript scenarios below), the `core/host` tests of that tag selected by name, and
the TUI tests of the root package (`TestE2EAttachedWindow*`), which drive an attached
window with teatest. The e2e-ssh job runs the `TestE2E` tests of `core/host` against an
ssh host, as described in the last section.

Two limits bound the suite:

- Duration: the three `check.sh` invocations together must finish within 120 seconds.
  Before the clock starts, `check.sh` compiles the test binaries of the same packages
  with the same tags (`-run '^$'`) and builds `herrscher` with the flags `TestScenarios`
  uses (`CGO_ENABLED=0`), so the build cache is warm. The clock then covers the three
  invocations, including the build of `herrscher` that `TestScenarios` runs itself, and
  `check.sh` fails above 120 seconds and prints the measured duration. The budget applies
  locally too, on purpose. Each test binary also runs with `-timeout 2m` as a safety net.
  The `TestE2E` invocation of the e2e-ssh job runs with `-timeout 2m` only.
- Memory: the e2e invocations run under a cgroup that caps the sum of their processes at
  6 GiB (`MemoryMax=6G`, 6442450944 bytes): the same RAM ceiling as on the development
  machine. CI also forbids swap (`MemorySwapMax=0`), which the development machine does
  not do. In CI the cgroup is created with `sudo systemd-run --scope`, and
  `check.sh` first checks that the cgroup of a command reads that limit in `memory.max`,
  so a missing or wrong cap fails the job. A process over the ceiling is killed by the
  kernel, which fails the job. The e2e-ssh job has no memory ceiling.

The `e2e` build also reads two variables that the production binary ignores:
`HERRSCHER_E2E_CLOCK` (an RFC 3339 instant the scheduler takes as now) and
`HERRSCHER_E2E_SCHEDULER_TICK` (a positive duration in place of the one-minute tick).

- Each `.txtar` file in `e2e/testdata/scenarios` is a
  [testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) scenario.
  It gets its own `HOME`, a fresh git repository as its work directory and a unique
  `HERRSCHER_INSTANCE_ID`, so its sockets and lock never touch a daemon already running
  on the machine.
- `waitdaemon` waits until the daemon of the scenario answers, and `! waitdaemon` until
  it is gone. `waitlog <regexp>` waits until the log of the fake vendor matches.
- Runtime root: each scenario runs its daemon under its own runtime root, `$WORK/run`,
  which the harness exports as `HERRSCHER_E2E_RUNTIME_ROOT`. The harness creates
  `<root>/<uid>` with mode 0700, and the daemon creates its `herrscher` directory inside
  it, so sockets, locks and the cleanup stay in the work directory. When that directory
  would leave no room for a socket name within the 103-byte limit, the scenario takes a
  short directory from `os.MkdirTemp` instead and removes it at the end. The binary is
  built with `-tags e2e`: `core/internal/runtimedir/roots_e2e.go` reads the variable,
  and `roots_production.go` (build tag `!e2e`) keeps `/run/user/<uid>` and `/tmp` for
  every other build. The variable must be absolute. When it is missing, the daemon
  targets a path that does not exist and fails, rather than fall back to the real
  directory.
- `herrscher serve` exits with status 130 on SIGINT, and testscript sends no other
  signal than SIGINT and SIGKILL, so a scenario starts it as
  `! exec herrscher serve &daemon&` and stops it with `kill -INT daemon`, `wait daemon`
  then `! waitdaemon`.
- `descendantgone <pid file>` waits up to five seconds for the `sleep` named in the file,
  one the fake started with `spawn`, to be gone. It recognises that process by the
  instance marker of the scenario and its exact argv, so a recycled pid does not pass,
  and it fails the scenario if the process still runs. A pid whose environment or
  command cannot be read fails the scenario too, instead of counting as gone. It fails
  at once if the file is older than the sleep's sixty seconds less the five-second wait,
  since the sleep may already have ended on its own. It kills that process at the end
  of the scenario if it still runs. `! descendantgone` is refused.
- These waits poll with a short sleep: they wait on other processes, which a
  `testing/synctest` bubble cannot control.
- At the end of a scenario, `settle` sends SIGINT to every process still alive that
  carries the instance of the scenario (the daemon, the fakes and their descendants),
  then SIGKILL to the survivors once `reapGrace` has passed. A process that this SIGINT
  stops fails the scenario with `errStopped`, a process that already ended is neither
  signalled nor counted, and a survivor of both signals is a leak (`errLeaked`).
- The cleanup removes every runtime entry named after the instance, and the names
  `core/host` gives its command and events sockets and its lock, hashed ones included.
  It only touches a runtime directory that `core/host` accepts as private to the user.
  The check creates a missing directory, so if the directory vanishes between the
  moment the cleanup sees it and the check, it comes back empty and private, as the
  daemon would create it. A session socket whose path passes 103 bytes takes a hashed
  name, `hs-<24 hex>.sock`, the cleanup cannot tell apart, so a scenario fails when
  such a socket appears in a runtime directory during it, from this scenario or
  another herrscher of the machine, since both run as the same user and nothing tells
  their sockets apart. The socket stays there: shorten the session name, and remove
  the socket only once no running herrscher holds it. The harness looks for the
  daemon of a scenario in that scenario's runtime root only (see below).
- `claude` on the `PATH` of a scenario is the fake of `e2e/vendortest`. It is a whole
  process standing in for the vendor CLI, so it speaks the stream-json protocol rather
  than recording calls. In stream-json mode it replays `$VENDORTEST_SCRIPT`, which is
  `$WORK/claude.script`: a `claude.script` file in the archive of the scenario
  provides it. The script holds one block per turn, separated by `---`, and each line
  is one step: `text <words>`, `tool <Name> <json input>`, `error <message>`,
  `apierror <message>`, `spawn <file>`, `echo`, `hang` or `crash`, the last three alone
  on their line, and blank lines are skipped. `spawn` starts a `sleep` of sixty seconds (`DescendantSleep` in
  `e2e/vendortest`) that the fake does not wait for, and writes its pid to `<file>` in
  the work directory. Stream and one-shot turns run the spawns of their turn. A turn with
  neither a step nor a spawn is an error. The turn count is shared by every process of
  the scenario, under a lock file beside it. A fake killed while it holds that lock
  leaves it behind, and the next turn fails after ten seconds with an error that names
  the lock. A turn past the end of the script, and a missing script, echo the last line
  of the prompt as `echo: <line>`. A `-p` call is a one-shot turn: it consumes a turn and
  runs its spawns, and it answers with the `text` steps of that turn joined by a blank
  line, or with the `echo` line when the turn has no text. Its other steps (`tool`,
  `error`, `apierror`, `hang`, `crash`) are ignored. The health probe is the exception:
  it is recognised by its argv, `--tools` and `--no-session-persistence`, as
  herrscher-claude-backend v0.15.6 sends it, and it consumes no turn and spawns nothing.
  The one-shot mode is reached with `herrscher session create --backend oneshot` or
  `herrscher bridge --backend oneshot`, not with `CLAUDE_KIND`, which the stream backend
  of a session overrides. Every start and every prompt is appended to `$VENDORTEST_LOG`.

The `codex` and `cursor-agent` fakes of the same package read that script and share its
turn count, so each turn goes to whichever vendor asks for it next. `codex` answers the
`login status` probe and serves the JSON-RPC app-server that the codex backend drives in
stream mode, one turn per `turn/start` request. `cursor-agent` answers the `status` probe
and replays one turn per invocation, with the prompt on its standard input. Both record
their prompts in `$VENDORTEST_LOG` as `codex prompt "<text>"` and `cursor prompt "<text>"`.
The `grok` fake answers `models` with a signed-in listing, `inspect --json` with the
`PreToolUse` hooks of `.grok/hooks/*.json` listed as project hooks matching `*`, and
replays one turn per `-p --output-format streaming-json` invocation, with the prompt on
its standard input. Its `tool` steps go through every `PreToolUse` hook of
`.grok/hooks/*.json` in its working directory, a hook that fails counting as a denial only
when it sets `failClosed`, as Grok does. It records `grok prompt "<text>"` and `grok
result call-N allowed` or `denied: <reason>`. `approval_grok_hook_gates_tool_calls.txtar`
drives a Grok session created without `--cmd` through an allowed then a denied
`run_terminal_command`, gated by a `Bash` rule. The `agy` fake answers `models` with a
signed-in listing (its "Fetching available models..." line on standard error, as the real
CLI does) and `--version`, and replays one turn per `--input-format stream-json
--output-format stream-json` invocation, reading the prompt from the `user` event on its
standard input and resuming the conversation named by `--conversation`. It records
`antigravity prompt "<text>"`.

`sessionrunning <session> <vendor command>` fails the scenario unless a process of its
instance has `--session <session>` and `--cmd <vendor command>` on its command line, so a
switch that proves nothing cannot pass. `sessionstopped` takes the same arguments and waits
up to five seconds for no such process, failing otherwise. Neither can be negated.

`session_switch.txtar` covers a vendor switch on one session. It seeds a prompt through
`claude`, checks the overview, then switches the session to `codex`, then to
`cursor-agent`, then to `grok`, then to `agy`. Before the switches away from `claude` and
`codex`, `sessionrunning` checks that the previous vendor process runs, and after them
`sessionstopped` waits for that process to stop. `cursor-agent` exits after each turn, so
the switches to `grok` and then `agy` are checked through the overview and the prompt log
only. The `codex`, `cursor-agent`, `grok` and `agy` prompts get `echo: <prompt>` back. Its `claude.script` holds one
turn, so the later turns are past the end of the script and echo.

Every bug found by hand becomes a scenario before it is fixed: the scenario is written
first, fails on the bug, and passes with the fix.

## Testing the remote paths

`go test ./core/host/` runs the remote end-to-end tests (the `TestE2E` ones) against
an ssh host when two environment variables are set:

- `HERRSCHER_E2E_SSH` names that host as `user@host`, or as an alias from `~/.ssh/config`.
  The host must accept key authentication without a prompt. Without this variable the
  tests skip, and the e2e-ssh CI job fails when any of them skips. Before running, the
  tests resolve the target with `ssh -G` and refuse a host that is the account running
  them: same uid, and either a loopback address (after alias resolution), or an identity
  that cannot be read on either side, or the same machine id on both sides. Machine ids
  come from `/etc/machine-id` on Linux and from `sysctl -n kern.uuid` on macOS. An
  unreadable identity is refused, not assumed to be another machine.
- `HERRSCHER_E2E_DEDICATED=1` lifts that refusal, but only under GitHub Actions, where
  `GITHUB_ACTIONS=true` is set by the runner itself. On a developer machine the variable
  alone is ignored and the refusal stays.
- Under the same GitHub Actions condition, `HERRSCHER_E2E_DEDICATED=1` also allows the tests to remove the remote runtime directory
  with `rmdir` at the end of a run, and only when it did not exist before the run and
  is empty at the end. Set it only on a host whose runtime directory belongs to the
  tests. Without it the directory is left in place. A non-empty directory is never
  removed, and the tests remove only the socket files they created.

