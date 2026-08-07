# Per-plugin versions implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an operator install any plugin at a chosen version, pin it against
bulk updates, and never end up with a composition that does not compile — from
the CLI and from the TUI.

**Architecture:** `go.mod` stays the source of truth for versions; a
`.herrscher-pins` file beside `plugins.go` records only which modules are
pinned. Every write to the composition runs through one transaction that warns
before writing, saves the three composition files, tries `get → tidy → build`,
and asks the operator what to do when the build fails. The CLI and the TUI
supply that transaction with different implementations of one decision
function, and share everything else.

**Tech Stack:** Go, the existing `manage/` package, `os/exec` around the Go
toolchain, `plugins/terminal/tui` (Bubble Tea) for the screen.

## Global Constraints

- The host stays platform-agnostic. Nothing under `core/` may name a concrete
  platform; `TestCoreNamesNoConcretePlatform` greps every file under `core/`,
  test files and comments included, for the string `discord`.
- No machine-specific absolute paths and no personal identifiers in committed
  files — the repository is public.
- No test shells out to the Go toolchain or the network. The toolchain is
  reached only through injected step functions.
- The pin file records module paths only, never versions.
- Both operator prompts are select menus in the TUI.
- Every new exported symbol carries a doc comment saying why it exists, matching
  the density already in `manage/`.
- `go build ./...`, `go vet ./...` and `go test ./...` must pass at the end of
  every task.

---

### Task 1: The pin file

**Files:**
- Create: `manage/pins.go`
- Test: `manage/pins_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func readPins(src string) (map[string]bool, error)` — parses pin-file text.
  - `func writePins(pins map[string]bool) string` — renders pin-file text, module paths sorted.
  - `const pinFile = ".herrscher-pins"` — the file name, resolved against the host dir by callers.

**Behaviour:** one module path per line. Blank lines and lines whose first
non-space character is `#` are ignored. Any other line that is not a plausible
module path (contains a space) is an error naming the 1-based line number.

- [ ] **Step 1: Write the failing tests**

Cases, all against `readPins`/`writePins` as strings:
1. Empty text → empty map, no error.
2. `"mod/a\nmod/b\n"` → both present.
3. Comments and blanks: `"# note\n\nmod/a\n"` → only `mod/a`.
4. Leading/trailing spaces around a path are trimmed.
5. `"mod/a\nnot a path\n"` → error mentioning line `2`.
6. Round-trip: `writePins(readPins(x))` on a valid input yields the modules
   sorted, one per line, trailing newline.

- [ ] **Step 2: Run the tests, confirm they fail**

Run: `go test ./manage/ -run TestPins -v`
Expected: FAIL, `undefined: readPins`.

- [ ] **Step 3: Implement `manage/pins.go`**

Pure string in, string out. No `os` import in this file.

- [ ] **Step 4: Run the tests, confirm they pass**

Run: `go test ./manage/ -run TestPins -v` → PASS.

- [ ] **Step 5: Commit**

```
git add manage/pins.go manage/pins_test.go
git commit -m "feat(manage): record which plugins are pinned"
```

---

### Task 2: Reading and parsing versions

**Files:**
- Create: `manage/version.go`
- Test: `manage/version_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces:
  - `func splitModuleVersion(arg string) (module, version string)` — splits
    `mod@v1.2.3`; version is `""` when absent. Must not split on the `@` of a
    module path that has none.
  - `type PluginVersion struct { Module, Installed, Latest string; Pinned bool }`
  - `type toolchain interface { List(ctx context.Context, args ...string) (string, error) }`
    — the seam every toolchain read goes through, so tests inject a fixture and
    no test runs `go`.
  - `func installedVersions(ctx context.Context, tc toolchain, modules []string) (map[string]string, error)`
    — parses `go list -m -json` output.
  - `func availableVersions(ctx context.Context, tc toolchain, module string) ([]string, error)`
    — parses `go list -m -versions` output; a network error yields `nil, nil`
    rather than an error, so the caller renders `?` and carries on.
  - `func contractsRequirement(ctx context.Context, tc toolchain, module, version string) (string, error)`
    — the `herrscher-contracts` version that `module@version` requires, for the
    pre-write warning; `""` when it requires none.

- [ ] **Step 1: Write the failing tests**

With a fake `toolchain` returning fixture text captured from real output
(embed the fixtures as string constants in the test file):
1. `splitModuleVersion("m@v1.2.3")` → `("m", "v1.2.3")`; `splitModuleVersion("m")` → `("m", "")`.
2. `installedVersions` parses two modules out of a `-json` stream.
3. `availableVersions` returns the versions in the order printed.
4. `availableVersions` returns `nil, nil` when the fake returns a network error.
5. `contractsRequirement` finds the requirement, and returns `""` when absent.

- [ ] **Step 2: Run the tests, confirm they fail**

Run: `go test ./manage/ -run TestVersion -v` → FAIL, undefined symbols.

- [ ] **Step 3: Implement `manage/version.go`**

Plus one real implementation of `toolchain` that runs `go` with `cmd.Dir` set to
the host directory, used only by production callers.

- [ ] **Step 4: Run the tests, confirm they pass**

- [ ] **Step 5: Commit**

```
git commit -m "feat(manage): read the versions a composition is built from"
```

---

### Task 3: The apply transaction

**Files:**
- Create: `manage/apply.go`
- Test: `manage/apply_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Decision int` with `Proceed`, `Abort`, `Restore`, `Keep`.
  - `type Decider interface { Warn(ctx context.Context, findings []string) Decision; Failed(ctx context.Context, buildOutput string) Decision }`
    — `Warn` returns `Proceed` or `Abort`; `Failed` returns `Restore` or `Keep`.
  - `func NewAutoDecider() Decider` — the non-interactive one: `Warn` → `Proceed`,
    `Failed` → `Restore`.
  - `type step func(ctx context.Context) (output string, err error)`
  - `func apply(ctx context.Context, dir string, findings []string, d Decider, steps []step) error`

**Behaviour, in order:** call `d.Warn(findings)`; on `Abort` return without
touching anything. Save `go.mod`, `go.sum`, `plugins.go` into memory. Run each
step in order. On the first error, call `d.Failed` with that step's combined
output: `Restore` writes the three saved files back and returns an error
carrying the build output; `Keep` returns the same error without restoring.

**The three files are saved even when one does not exist** — a missing file is
restored by being removed again, so a `go.sum` created by the attempt does not
survive a restore.

- [ ] **Step 1: Write the failing tests**

Against a `t.TempDir()` holding the three files, with fake steps and a scripted
`Decider`:
1. `Warn` returns `Abort` → no step ran, files byte-identical.
2. All steps succeed → no restore, files hold what the steps wrote.
3. Step 2 of 3 fails, `Failed` returns `Restore` → step 3 never ran, all three
   files back to their original bytes, error contains the failing step's output.
4. Same, `Failed` returns `Keep` → files keep the modified content.
5. `go.sum` absent at save time, created by a step, then `Restore` → it is gone
   again.
6. `NewAutoDecider` drives case 3's shape without any prompt.

- [ ] **Step 2: Run the tests, confirm they fail**

- [ ] **Step 3: Implement `manage/apply.go`**

- [ ] **Step 4: Run the tests, confirm they pass**

Run: `go test ./manage/ -run TestApply -v` → PASS.

- [ ] **Step 5: Commit**

```
git commit -m "feat(manage): a composition change the operator can back out of"
```

---

### Task 4: Wire the CLI verbs

**Files:**
- Modify: `manage/manage.go`, `manage/lifecycle.go`
- Create: `manage/prompt_decider.go`
- Test: `manage/manage_version_test.go`

**Interfaces:**
- Consumes: everything produced by Tasks 1-3.
- Produces: `func NewPromptDecider(in *bufio.Reader, s style) Decider` — the
  terminal implementation, reusing the existing `manage/prompt.go` helpers and
  `manage/style.go`.

**Surface:**
- `plugin list` prints one row per module: path, installed version, `pinned` when
  pinned, and the newest available or `?`.
- `plugin add <module>[@<version>]` — the version is passed to `go get`.
- `plugin pin <module> [<version>]` — moves first when a version is given, then
  records the pin.
- `plugin unpin <module>` — drops the record. Refuses, by name, a module absent
  from `plugins.go`.
- `update` skips pinned modules and prints `skipped <module> (pinned <version>)`
  for each.
- A `--yes` flag on `plugin` and `update` selects `NewAutoDecider`; so does the
  absence of a TTY.

Every path that writes goes through `apply` with the steps
`go get …`, `go mod tidy`, `go build ./...`, `go install .`.

- [ ] **Step 1: Write the failing tests**

Table tests over argument parsing and the pin-aware update selection, with the
toolchain and the decider injected:
1. `plugin add m@v1.2.3` produces a `go get m@v1.2.3` step.
2. `plugin pin m` on a module absent from `plugins.go` → exit 1, message names
   the module, pin file unchanged.
3. `update` with `m` pinned → no `go get -u m` step, and the skip line printed.
4. No TTY → the auto decider is used.

- [ ] **Step 2: Run the tests, confirm they fail**

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the whole suite**

Run: `go build ./... && go vet ./... && go test ./...` → all pass.

- [ ] **Step 5: Commit**

```
git commit -m "feat(plugin): choose a version, and pin it against an update"
```

---

### Task 5: The TUI plugins screen

**Files:**
- Create: `plugins/terminal/tui/plugins_screen.go`
- Test: `plugins/terminal/tui/plugins_screen_test.go`
- Modify: `plugins/terminal/tui/palette.go` (one entry opening the screen)

**Interfaces:**
- Consumes: the `manage` surface from Task 4 through a narrow interface declared
  in the TUI package (list, add, pin, unpin, update), so the screen is testable
  against a fake and the TUI does not depend on the toolchain.
- Produces: nothing consumed elsewhere.

**Behaviour:** a list of plugins with installed version, pin marker and
available version; `↑↓` to select; actions for bump, pin/unpin, choose version,
remove. Both decision points render as select menus. Build output streams into
the screen. On success the screen ends on a line stating the change is on disk
and applies at the next restart — the screen never restarts anything.

- [ ] **Step 1: Write the failing tests**

Driving the model with key messages against a fake manage seam:
1. The screen lists what the seam reports, pin marker included.
2. Choosing "version…" opens a select menu of the available versions.
3. A seam reporting findings renders the warning as a select menu, and choosing
   the abort option leaves the seam uncalled for any write.
4. A failing build renders the compiler output and the restore/keep menu.
5. A successful apply ends on the restart line.

- [ ] **Step 2: Run the tests, confirm they fail**

- [ ] **Step 3: Implement the screen and the palette entry**

- [ ] **Step 4: Run the whole suite**

Run: `go build ./... && go vet ./... && go test ./...` → all pass.

- [ ] **Step 5: Commit**

```
git commit -m "feat(tui): a screen for the plugins the binary is made of"
```

---

### Task 6: Documentation

**Files:**
- Modify: `README.md`
- Modify (in the docs site repo, as a separate PR): `plugins/model.svx`,
  `guide/managing-plugins.svx`, EN and FR both.

- [ ] **Step 1: Update the README's plugin section** with `@version`, `pin`,
      `unpin`, the pin file, and the two prompts.
- [ ] **Step 2: Run the suite** — `go test ./...` (the README is asserted by
      some tests in this repo).
- [ ] **Step 3: Commit**

```
git commit -m "docs: versions, pins, and how a refused build backs out"
```

The docs-site changes are a separate PR in `herrscher-docs`, mirroring EN and
FR, and are not part of this branch.
