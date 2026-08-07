# Per-plugin versions design

## The problem

A plugin's version is not something the operator can choose. `herrscher plugin
add <module>` runs `go get -- <module>`, which takes whatever is newest;
`herrscher update` runs `go get -u` over every compiled-in module at once. There
is no way to install a plugin at a chosen version, no way to say "leave this one
alone", and nothing that tells the operator a version is available before they
take it.

There is also nothing between a bad choice and a broken tree. `go get` succeeds
on any version that exists — the failure surfaces one step later, at compile
time, typically because the plugin was built against a different
`herrscher-contracts`. Today that leaves `go.mod` holding the refused version and
the operator repairing by hand.

## What this builds

Three capabilities, one safety mechanism, and one screen.

**Reading versions.** `herrscher plugin list` reports, per module: the version
installed, whether it is pinned, and the newest version published. The newest is
resolved with `go list -m -versions`, which needs the network — when it is
unavailable the column reads `?` and the rest of the listing still prints.

**Choosing a version.** `herrscher plugin add <module>@<version>` installs a
named version. `herrscher plugin pin <module> [<version>]` records the module as
pinned, moving it to `<version>` first when one is given. `herrscher plugin
unpin <module>` drops the record.

**Respecting a pin.** `herrscher update` skips pinned modules and names each one
it skipped, so a pin never looks like a silent no-op.

### Where a version lives

`go.mod` stays the single source of truth for versions. Every version-setting
operation is a `go get <module>@<version>`; every version read comes from
`go list -m`. Go keeps resolving the shared dependencies — notably
`herrscher-contracts`, which every plugin imports — and this project does not
reimplement that resolution.

`go.mod` can say *which* version. It cannot say *do not move this one*. So a pin
is a separate, tiny fact: a `.herrscher-pins` file beside `plugins.go`, one
module path per line, blank lines and `#` comments ignored. It sits in the
repository rather than in the state directory because a pin describes the code
that gets compiled, not the machine that runs it — it belongs to the composition
and should be visible in a review of it.

The file records only the module path, never a version. The version is already
in `go.mod`, and duplicating it would create two places that can disagree.

### The safety mechanism

Every operation that writes to the composition runs as a transaction with the
operator inside it, and it warns twice: once from what can be known before
writing, once from what only the compiler can say.

1. **Warn from what is known.** Before writing anything, compare the requested
   version against the composition: is it older than the version installed, and
   does its `go.mod` require a different major/minor of `herrscher-contracts`
   than the composition resolves today? Report what is found and ask the
   operator to confirm. When nothing is found, say so in one line and proceed —
   the confirmation is still asked, because "nothing known against it" is not
   the same as "it will work".

2. **Save.** Copy `go.mod`, `go.sum` and `plugins.go` before the first write.

3. **Try.** `go get <module>@<version>`, `go mod tidy`, `go build ./...`.

4. **Ask again on failure.** When the build fails, print the compiler's own
   error — not a paraphrase — and offer two outcomes: restore the three saved
   files and leave the tree exactly as it was, or keep the tree as it is to
   repair by hand. Neither is chosen automatically.

5. **Install on success.** `go install .`, as today. The deployed binary is
   never replaced by a composition that does not compile, because the build
   gates it.

A non-interactive run (no TTY, or `--yes`) takes the safe branch at both
prompts: it proceeds past the first warning and restores on failure. A tool
running unattended must not leave a broken tree behind.

## File structure

New files, all in `manage/`:

- `manage/version.go` — resolve and query versions. Reads the installed set
  (`go list -m -json all` filtered to the compiled-in modules), lists available
  versions for a module, parses `<module>@<version>` arguments, and reads a
  module's declared `herrscher-contracts` requirement for the pre-write warning.
- `manage/pins.go` — read and write `.herrscher-pins`. Pure text in, pure text
  out, no filesystem knowledge beyond the path handed to it.
- `manage/apply.go` — the transaction: save, try, restore. Takes the steps to
  run and a decision function for the two prompts, so the whole mechanism is
  exercisable in a test with no compiler and no terminal.

Modified:

- `manage/manage.go` — `plugin list` gains the version columns; `plugin add`
  accepts `@version`; `pin` and `unpin` are new subcommands. The existing
  `rebuild` helper is replaced by a call into `apply.go`.
- `manage/lifecycle.go` — `update` filters the pinned modules out of its loop
  and names them, then applies through the same transaction.

The decision function is the seam that keeps this testable and keeps the TUI
honest: the CLI implements it with a terminal prompt, the TUI with a select
menu, and neither owns any of the logic around it.

## The TUI screen

A dedicated plugins screen, opened from the palette. It lists each compiled-in
plugin with its installed version, its pin state, and the newest version
available, and carries four actions: bump, pin/unpin, choose a version from the
published list, and remove.

It calls the same `manage/` entry points as the CLI, supplying a select-menu
implementation of the decision function. Both warnings appear as select menus.
The build output streams into the screen rather than into the transcript — it is
the screen's own work, and it is long.

The screen does not restart anything. A successful apply reinstalls the binary
underneath the running process, so the screen ends on a line saying the change
is on disk and takes effect at the next restart. A TUI that tried to reload
itself out from under its own event loop would be a second, harder problem
riding on this one.

## Errors

- **No network.** Version listing degrades to `?`; every other operation still
  works, since `go get` of an already-cached version does not need the network.
- **Version does not exist.** `go get` fails before anything is built; the
  transaction restores and reports Go's message verbatim.
- **Pin file missing or malformed.** A missing file means no pins. An
  unparseable line is reported with its line number and the operation refuses —
  silently ignoring it would silently un-pin a module.
- **Pinning a module that is not compiled in.** Refused by name: a pin on
  something absent from `plugins.go` would never be consulted.

## Testing

`pins.go` and `version.go` are pure enough to test directly on strings —
argument parsing, pin round-trips, malformed lines, the `?` degradation.

`apply.go` is tested with injected steps and an injected decision function: a
failing build restores all three files and returns the compiler's text; a
successful one does not restore; the non-interactive path takes the safe branch
at both prompts. No test shells out to the Go toolchain.

The TUI screen is tested the way the rest of `tui/` is — driving the model with
key messages and asserting on the rendered frame, with a fake `manage/` seam.
