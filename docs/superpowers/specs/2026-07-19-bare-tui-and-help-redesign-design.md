# Bare-command TUI launch + help redesign

**Date:** 2026-07-19
**Status:** Design approved

## Problem

Two rough edges in the `herrscher` CLI entrypoint (`main.go`):

1. Running `herrscher` with no arguments prints usage and exits 2. The terminal
   TUI — the most natural interactive entrypoint — is only reachable via
   `herrscher serve` on a TTY. Bare invocation should *just open the TUI*.
2. `usage()` is one dense, flat wall of multi-line verb descriptions on stderr.
   It is hard to scan and does not surface the TUI at all.

## Goals

- `herrscher` (no args) launches the multi-session terminal TUI when possible.
- A restructured, colored, grouped help screen that reads at a glance.
- No behavior change to existing verbs, `serve`, or any plugin.

## Non-goals

- No new flags. No changes to `serve.go` / plugins / the TUI itself.
- No color/styling framework beyond the already-vendored `lipgloss`.

## Design

### 1. Bare invocation → TUI

The TUI already runs as the foreground of `runServe` whenever a gateway
implementing `contracts.Foreground` (the `terminal` gateway) is compiled in and
stdout is a TTY (`serve.go:105-110`). Bare `herrscher` reuses that path.

In `main.go`, after the `.env` load block (which is moved *above* the
no-args check so the TUI/serve path sees secrets), replace the current
`if len(os.Args) < 2 { usage(); os.Exit(2) }` early-exit with:

```go
if len(os.Args) < 2 {
    if term.IsTerminal(int(os.Stdout.Fd())) && hasTerminalGateway() {
        if err := runServe(ctx, nil); err != nil {
            fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
            os.Exit(1)
        }
        return
    }
    usage()
    os.Exit(2)
}
```

Fallback (chosen): when there is **no** TTY (piped/redirected output) **or** no
`terminal` gateway is compiled in, print the help and exit 2 — never start a
surprise background daemon.

`ctx` (a plain `context.Background()`, matching the other runtime verbs) is
established before this branch.

Detection helper (in `main.go`), which inspects the plugin registry without
building the hub:

```go
// hasTerminalGateway reports whether a terminal (TUI) gateway plugin is
// compiled in, so bare `herrscher` knows it can open the TUI.
func hasTerminalGateway() bool {
    for _, p := range contracts.Default.Gateways() {
        if p.Manifest.Kind == "terminal" {
            return true
        }
    }
    return false
}
```

`term` (`golang.org/x/term`) and `contracts`
(`github.com/Herrscherd/herrscher-contracts`) are added to `main.go`'s imports;
both are already used elsewhere in the binary.

### 2. Help redesign

`usage()` moves out of `main.go` into a new file `usage.go` (package `main`)
and is rewritten with `lipgloss`. A renderer bound to `os.Stderr`
(`lipgloss.NewRenderer(os.Stderr)`) auto-detects the color profile, so output
degrades to plain text under `NO_COLOR`, `TERM=dumb`, or a non-TTY stderr —
matching the existing convention in `manage/style.go`.

Layout (content preserved from the old help, reorganized; one line per verb,
per the density decision — flag detail stays behind each verb's own FlagSet):

```
  ⛧ HERRSCHER
  modular Discord ⇄ Claude agent harness host

  DÉMARRER
    herrscher                          open the multi-session terminal TUI
    herrscher init                     compose the plugin stack + secrets (wizard)

  SESSIONS & AGENTS
    session <create|close|list|who>    bridged channel + worktree + backend
    agent   <create|list>              durable companion agents (persona/MCP)
    memory  <locate|forget|record>     inspect/edit the memory graph

  DAEMON & SERVICE
    serve                              always-on gateway daemon (24/7)
    bridge   --cmd '<command>'         link a channel to one command
    service  <install|uninstall|…>     run serve as a native boot service

  SETUP & MAINTENANCE
    plugin  <list|add|remove>          edit the compiled-in plugin set + rebuild
    update                             bump every plugin + rebuild
    install                            build then run the service install

  Run `herrscher <command> --help` for flags and options.

  env: DISCORD_BOT_TOKEN (required), DISCORD_CHANNEL_ID (default channel)
       DCTL_OWNER_ID (instance-id fallback), DCTL_STATE_DIR (state dir)
```

Styling: section headers bold + cyan; the banner bold; verb names bold; the
one-line arg/description hints dim. The whole block is assembled into a single
string and written to `os.Stderr` (unchanged stream, so `2>` redirection and
existing scripts keep working).

## Files touched

- `main.go` — move `.env` load above the no-args branch; new bare-invocation
  branch; `hasTerminalGateway()` helper; add `term`/`contracts` imports; remove
  the old `usage()` body.
- `usage.go` (new) — styled `usage()` via lipgloss.

## Testing

- `go build ./...` and `go vet ./...`.
- `herrscher` on a TTY with the terminal gateway compiled → TUI opens.
- `herrscher | cat` (no TTY) → styled-but-plain help, exit 2.
- `herrscher --help` / `herrscher help` → same help, exit 0 (unchanged path).
- `NO_COLOR=1 herrscher 2>&1 | cat` → no ANSI codes.
- Existing `manage` help/wizard snapshot tests (if any) remain green.

## Risks

- Bare `herrscher` in a script that expected exit-2 usage now opens a TUI *only*
  if that script runs on a TTY — scripts virtually always pipe/redirect, so the
  non-TTY fallback keeps them on the old help+exit-2 behavior.
