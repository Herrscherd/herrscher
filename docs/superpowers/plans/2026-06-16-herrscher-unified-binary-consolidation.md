# Herrscher Unified Binary Consolidation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fold `herrscherd` + `herrscher-cli` + `herrscher-core` into the umbrella repo `herrscher` so it builds a single `herrscher` binary, then delete the three absorbed repos.

**Architecture:** The umbrella becomes a `package main` Go module at `github.com/Herrscherd/herrscher`. The daemon/verbs (from herrscherd) live at the root; management commands (from herrscher-cli) become subpackage `manage`; the engine (from herrscher-core) becomes `core/` guarded by a purity test. `contracts`, the two plugins, and `dctl` are untouched and wired via local `replace`.

**Tech Stack:** Go 1.23, go modules with local `replace`, rtk-proxied `go build`/`go test`.

**Baseline (verified 2026-06-16):** all 19 packages across the family build and test green. Preserve that at every task.

**Identity:** all commits use `git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com`.

---

## File Structure

| Path (new) | From | Responsibility |
|---|---|---|
| `herrscher/go.mod` | new | module `github.com/Herrscherd/herrscher`, requires+replaces dctl/contracts/2 plugins |
| `herrscher/main.go` | herrscherd/main.go (+ cli dispatch) | unified dispatcher: daemon/verbs + plugin/update/install |
| `herrscher/{serve,bridge,service,channel,envfile}.go` (+tests) | herrscherd | daemon wiring + legacy verbs |
| `herrscher/plugins.go` | herrscherd | managed blank-import manifest |
| `herrscher/manage/{lifecycle,manifest}.go` (+test) | herrscher-cli | `PluginCmd`/`UpdateCmd`/`InstallCmd` |
| `herrscher/core/**` | herrscher-core/** | bridge/config/host/service/internal engine |
| `herrscher/core/purity_test.go` | new | forbids concrete imports in core/... |

Repos deleted at the end: `herrscherd`, `herrscher-cli`, `herrscher-core`.

---

## Task 1: Create the umbrella module skeleton on a branch

**Files:** Create `herrscher/go.mod`, branch `consolidate-unified-binary`.

- [ ] **Step 1: Branch the umbrella**

```bash
cd /home/shan/dev/herrscher && git checkout -b consolidate-unified-binary
rm -rf @herrscher           # drop the broken symlink scheme
```

- [ ] **Step 2: Write `herrscher/go.mod`**

```
module github.com/Herrscherd/herrscher

go 1.23

require (
	github.com/Herrscherd/dctl v0.0.0
	github.com/Herrscherd/herrscher-claude-backend v0.0.0
	github.com/Herrscherd/herrscher-contracts v0.0.0
	github.com/Herrscherd/herrscher-discord-gateway v0.0.0
)

require github.com/coder/websocket v1.8.14 // indirect

replace github.com/Herrscherd/dctl => ../dctl
replace github.com/Herrscherd/herrscher-contracts => ../herrscher-contracts
replace github.com/Herrscherd/herrscher-claude-backend => ../herrscher-claude-backend
replace github.com/Herrscherd/herrscher-discord-gateway => ../herrscher-discord-gateway
```

- [ ] **Step 3: Commit the skeleton**

```bash
git add -A && git commit -m "chore(herrscher): module skeleton for unified binary"
```

## Task 2: Fold herrscher-core into herrscher/core

**Files:** Move `herrscher-core/{bridge,config,host,service,internal}` → `herrscher/core/...`; rewrite imports.

- [ ] **Step 1: Copy the packages**

```bash
cd /home/shan/dev
mkdir -p herrscher/core
cp -R herrscher-core/bridge herrscher-core/config herrscher-core/host \
      herrscher-core/service herrscher-core/internal herrscher/core/
```

- [ ] **Step 2: Rewrite every internal import path**

Replace `github.com/Herrscherd/herrscher-core/` with `github.com/Herrscherd/herrscher/core/` across the copied files:

```bash
grep -rl 'Herrscherd/herrscher-core/' herrscher/core | \
  xargs sed -i 's#github.com/Herrscherd/herrscher-core/#github.com/Herrscherd/herrscher/core/#g'
```

- [ ] **Step 3: Tidy and build the core packages**

Run: `cd herrscher && rtk proxy go build ./core/...`
Expected: success (core depends only on contracts + stdlib).

- [ ] **Step 4: Run the moved core tests**

Run: `cd herrscher && rtk proxy go test ./core/...`
Expected: the 12 former herrscher-core packages pass (0 fail).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher && git add -A && git commit -m "feat(herrscher): fold herrscher-core into core/"
```

## Task 3: Add the core purity test

**Files:** Create `herrscher/core/purity_test.go` (package `core_test`).

- [ ] **Step 1: Write the failing test**

```go
package core_test

import (
	"go/build"
	"path/filepath"
	"strings"
	"testing"
)

// TestCorePurity asserts core/... never imports a concrete adapter (dctl) or any
// plugin. core is the agnostic engine: arrows point only at contracts + stdlib.
func TestCorePurity(t *testing.T) {
	forbidden := []string{
		"github.com/Herrscherd/dctl",
		"github.com/Herrscherd/herrscher-discord-gateway",
		"github.com/Herrscherd/herrscher-claude-backend",
	}
	roots := []string{"bridge", "config", "host", "service",
		"internal/control", "internal/forge", "internal/health",
		"internal/instanceid", "internal/manager", "internal/state",
		"internal/supervisor", "internal/worktree"}
	for _, r := range roots {
		pkg, err := build.ImportDir(filepath.Join(".", r), 0)
		if err != nil {
			t.Fatalf("%s: %v", r, err)
		}
		imports := append(append([]string{}, pkg.Imports...), pkg.TestImports...)
		for _, imp := range imports {
			for _, bad := range forbidden {
				if imp == bad || strings.HasPrefix(imp, bad+"/") {
					t.Errorf("core/%s imports forbidden %q", r, imp)
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run it**

Run: `cd herrscher && rtk proxy go test ./core/ -run TestCorePurity -v`
Expected: PASS (core is already clean). If it FAILS, a concrete import leaked during the move — fix the offending file before proceeding.

- [ ] **Step 3: Commit**

```bash
git add core/purity_test.go && git commit -m "test(core): purity guard forbids concrete imports"
```

## Task 4: Fold herrscher-cli into herrscher/manage

**Files:** Move `herrscher-cli/{lifecycle,manifest,manifest_test}.go` → `herrscher/manage/`; convert `package main` → `package manage`; export the three entry points.

- [ ] **Step 1: Copy and re-package**

```bash
cd /home/shan/dev
mkdir -p herrscher/manage
cp herrscher-cli/lifecycle.go herrscher-cli/manifest.go herrscher-cli/manifest_test.go herrscher/manage/
sed -i 's/^package main$/package manage/' herrscher/manage/*.go
```

- [ ] **Step 2: Export the entry points**

In `herrscher/manage/` rename the three dispatch funcs from `herrscher-cli/main.go` (copy them in from there) to exported names and place them in a new `herrscher/manage/manage.go`:
- `pluginCmd` → `PluginCmd(args []string) int`
- `updateCmd` → `UpdateCmd(args []string) int` (currently in lifecycle.go — export in place)
- `installCmd` → `InstallCmd(args []string) int` (lifecycle.go — export in place)

Copy `pluginCmd`, `resolveHost`, `rebuild`, `run`, `pastTense` from `herrscher-cli/main.go` into `herrscher/manage/manage.go` (package `manage`), renaming `pluginCmd`→`PluginCmd`. Adjust `resolveHost` candidates to self-host first:

```go
candidates := []string{explicit, ".", "herrscher", "herrscherd"}
```

- [ ] **Step 3: Build the manage package**

Run: `cd herrscher && rtk proxy go build ./manage/`
Expected: success.

- [ ] **Step 4: Run the manage tests**

Run: `cd herrscher && rtk proxy go test ./manage/`
Expected: the former herrscher-cli manifest test passes.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(herrscher): fold herrscher-cli into manage/"
```

## Task 5: Fold herrscherd into the herrscher root and merge the dispatcher

**Files:** Move `herrscherd/{serve,bridge,service,channel,envfile,plugins}.go` (+tests) → `herrscher/`; write the unified `herrscher/main.go`.

- [ ] **Step 1: Copy the host files**

```bash
cd /home/shan/dev
cp herrscherd/serve.go herrscherd/bridge.go herrscherd/service.go \
   herrscherd/channel.go herrscherd/envfile.go herrscherd/plugins.go \
   herrscherd/bridge_flags_test.go herrscherd/envfile_test.go herrscher/
```

- [ ] **Step 2: Repoint core imports in the copied host files**

```bash
cd herrscher
sed -i 's#github.com/Herrscherd/herrscher-core/#github.com/Herrscherd/herrscher/core/#g' \
  serve.go bridge.go service.go channel.go envfile.go *_test.go
```

- [ ] **Step 3: Write the unified `herrscher/main.go`**

Start from `herrscherd/main.go` (the verb dispatcher) and add the management cases delegating to `manage`. Replace the package header comment (drop the `dctl` self-reference) and extend the switch:

```go
// Command herrscher is the composition root and CLI for a Herrscher host: it wires
// the registered gateway/backend plugins and the core (bridge/serve) into one
// binary, exposes the daemon (serve/bridge/service) and the host management verbs
// (plugin/update/install), plus the low-level channel verbs.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Herrscherd/dctl"
	"github.com/Herrscherd/herrscher/manage"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]

	// Management verbs need no Discord client; dispatch them first.
	switch cmd {
	case "plugin":
		os.Exit(manage.PluginCmd(args))
	case "update":
		os.Exit(manage.UpdateCmd(args))
	case "install":
		os.Exit(manage.InstallCmd(args))
	}

	token := os.Getenv("DISCORD_BOT_TOKEN")
	client := dctl.New(token, os.Getenv("DISCORD_CHANNEL_ID"))
	ctx := context.Background()

	var err error
	switch cmd {
	case "send":
		err = runSend(ctx, client, args)
	case "reply":
		err = runReply(ctx, client, args)
	case "read":
		err = runRead(ctx, client, args)
	case "watch":
		err = runWatch(ctx, client, args)
	case "bridge":
		err = runBridge(ctx, args)
	case "react":
		err = runReact(ctx, client, args)
	case "thread":
		err = runThread(ctx, client, args)
	case "channel":
		err = runChannel(ctx, client, args)
	case "serve":
		err = runServe(ctx, client, token, args)
	case "service":
		err = runService(ctx, args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "herrscher: unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
		os.Exit(1)
	}
}
```

Keep `runSend/runReply/runRead/runWatch/runReact/runThread`, `channelFlag`, and `line` from herrscherd/main.go (move them into this file). Update the `usage()` text: rename the header to `herrscher — Discord bot CLI + host` and add the three management verbs; replace every `dctl ` command-prefix literal with `herrscher `.

- [ ] **Step 4: Build the whole module**

Run: `cd herrscher && rtk proxy go mod tidy && rtk proxy go build ./...`
Expected: success — one `herrscher` binary.

- [ ] **Step 5: Run the full module test suite**

Run: `cd herrscher && rtk proxy go test ./...`
Expected: all packages pass (former herrscherd + core + manage tests), 0 fail.

- [ ] **Step 6: Smoke-test the binary**

Run: `cd herrscher && rtk proxy go run . --help`
Expected: usage text shows `herrscher` (no `dctl`), lists send/read/serve/plugin/update/install.

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat(herrscher): fold herrscherd host into unified binary"
```

## Task 6: Verify the family still builds without the absorbed repos

**Files:** none (verification only). Confirms nothing outside the deleted repos referenced them.

- [ ] **Step 1: Confirm only the absorbed repos reference the old paths**

Run: `cd /home/shan/dev && grep -rl 'Herrscherd/herrscher-core\|Herrscherd/herrscherd\|Herrscherd/herrscher-cli' herrscher-contracts herrscher-discord-gateway herrscher-claude-backend dctl`
Expected: no output (these four never imported the absorbed modules).

- [ ] **Step 2: Build/test the untouched modules**

Run: for `herrscher-contracts herrscher-discord-gateway herrscher-claude-backend dctl`: `rtk proxy go build ./... && rtk proxy go test ./...`
Expected: green, unchanged from baseline.

## Task 7: Push the umbrella and open its PR

- [ ] **Step 1: Push the branch**

```bash
cd /home/shan/dev/herrscher && git push -u origin consolidate-unified-binary
```

- [ ] **Step 2: Open the PR**

```bash
gh pr create --repo Herrscherd/herrscher --base master --head consolidate-unified-binary \
  --title "Unified herrscher binary: fold in herrscherd, herrscher-cli, herrscher-core" \
  --body "Folds the daemon (herrscherd), management CLI (herrscher-cli) and engine (herrscher-core) into a single \`herrscher\` binary. core/ guarded by a purity test. contracts/plugins/dctl untouched. The herrscherd, herrscher-cli, herrscher-core repos are deleted as part of this change. See docs/superpowers/specs/2026-06-16-herrscher-unified-binary-plugin-cli-design.md."
```

Expected: PR URL printed.

## Task 8: Delete the absorbed repos (destructive — explicit go-ahead given)

- [ ] **Step 1: Delete local working copies**

```bash
cd /home/shan/dev && rm -rf herrscherd herrscher-cli herrscher-core
```

- [ ] **Step 2: Delete the GitHub repos**

```bash
gh repo delete Herrscherd/herrscherd --yes
gh repo delete Herrscherd/herrscher-cli --yes
gh repo delete Herrscherd/herrscher-core --yes
```

Expected: each confirms deletion. If `gh` lacks `delete_repo` scope, run `gh auth refresh -h github.com -s delete_repo` first (interactive — surface to the user).

## Task 9: Final recap

- [ ] **Step 1: Produce the changed-repository recap table** (status per repo: changed / deleted / untouched, with the PR link for the umbrella).
