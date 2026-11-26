# Plugins

Plugins compile **into** the binary, the xcaddy pattern: add a blank import,
rebuild. `herrscher update` bumps every unpinned compiled-in plugin, rebuilds and
reinstalls the binary. Restart the service afterwards to run it.

| Category | Port | Official plugin |
|----------|------|-----------------|
| Gateway | `Gateway` | [herrscher-discord-gateway], [herrscher-telegram-gateway], in-tree `terminal` |
| Backend | `Backend` | [herrscher-claude-backend], [herrscher-codex-backend], [herrscher-cursor-backend], [herrscher-pixellab-backend] |
| Memory | `Memory` | [herrscher-obsidian-memory] |
| Orchestrator | `Orchestrator` | [herrscher-orchestrator] |
| Skills | playbooks only | [herrscher-superset-skills] |
| Compactor | `Compactor` | [herrscher-jev-compactor] |

## Versions and pins

Each plugin carries its own version. `herrscher plugin list` reports, per module,
the version installed, whether it is pinned, and the newest published. That last
one needs the network and reads `?` without it.

```bash
herrscher plugin add <module>@<version>   # install a chosen version
herrscher plugin pin <module> [<version>] # move there first if given, then pin
herrscher plugin unpin <module>           # drop the record
```

`herrscher update` skips every pinned module and names each one it skipped, so a
pin never looks like a silent no-op. The pins live in `.herrscher-pins` beside
`plugins.go`: one module path per line, `#` comments ignored, no versions, since
`go.mod` already holds those and a second copy could disagree.

## Every change is a transaction

Before writing, the CLI reports what can be known against the change (that it
moves a module backwards, or that it wants a different `herrscher-contracts` than
the composition resolves) and asks to proceed. It then saves `go.mod`, `go.sum`
and `plugins.go`, and runs `go get`, `go mod tidy`, `go build ./...`,
`go install .`.

When the build refuses, it prints the compiler's own error and offers two
outcomes: restore the three files and leave the tree exactly as it was, or keep
it as it is to repair by hand. A run with `--yes`, or with no terminal to ask,
takes the safe branch at both. It proceeds, and it restores.

The same flow is available from the UI's `/plugins` screen, where both questions
are select menus and a successful change ends on a line saying it applies at the
next restart.

## Contributed commands and skills

A plugin may also contribute commands of its own and the skills that teach an
agent to use them. The daemon namespaces each contributed verb under the plugin's
kind, so the Discord gateway declares `channel read` and an operator types
`discord channel read`. The prefix is imposed by the host, and two plugins can
never collide.

A plugin's skills install only when that plugin is in the build, so a Discord
playbook never sits in the context of a machine that has no Discord. Skills are a
category of their own too: a plugin may contribute nothing but playbooks, and it
is added, listed, pinned and removed by the same commands as any other.

What a plugin hands over is decided at startup rather than at compile time. A
factory that looks at the machine and finds the tool its playbook describes is
not installed returns nothing, and the daemon says which plugin declined and why.

## Languages

Operator warnings follow the operator language, read live at the moment they are printed. A warning about a named session uses the language recorded for that session, or the live operator language when none is recorded; operator-level warnings always use the operator language.


## Managing the compiled-in set

The same binary manages its own plugin composition (the `manage/` package). Four verbs (`init`, `plugin`, `update`, `install`) compose and maintain the compiled-in plugin set. They do **not** run the runtime; they edit **`plugins.go`** (the blank-import list between the `herrscher:plugins` / `herrscher:end` markers) and rebuild.

Plugins are compiled **into** the single binary (the xcaddy pattern): you add a blank import and rebuild, and there is no dynamic loading. Each plugin self-registers into the global `contracts.Default` registry from its `init()`. For authoring your own plugin, see Writing a plugin.


### `init`: compose the stack from a catalog

`herrscher init` builds a fresh plugin stack from a built-in **catalog** of published modules, picking **one kind per category** (gateway / backend / memory / orchestrator / extractor / skills).

With no flags it writes the batteries-included **default stack**:

| Category | Kinds | Default kind | Module |
|----------|-------|--------------|--------|
| Gateway | `discord` \| `telegram` | `discord` | herrscher-discord-gateway, herrscher-telegram-gateway |
| Backend | `claude` \| `codex` \| `cursor` | `claude` | herrscher-claude-backend |
| Memory | `obsidian` | `obsidian` | herrscher-obsidian-memory |
| Orchestrator | `basic` | `basic` | herrscher-orchestrator |
| Extractor | `llm` | `llm` | herrscher-llm-extractor |
| Skills | `superset` | `superset` | herrscher-superset-skills |
| Compactor | `jev` | `jev` | herrscher-jev-compactor |

The extractor is what turns the basic orchestrator into a **Learner**. Without one the host keeps the plain persist-only `Curator`.

The skills plugin builds no port at all: it contributes playbooks, and it costs nothing to keep in a build that does not need them: its factory declines on a machine without Superset, so all it adds there is one line at startup saying so.

**In-tree plugins are always re-emitted.** The terminal gateway (`github.com/Herrscherd/herrscher/plugins/terminal`) lives inside the host module itself, so `init` puts it back into `plugins.go` on every run (dropping it would silently remove a compiled-in gateway), and it is excluded from `go get`, since it is not an external module.

It rewrites `plugins.go` to exactly that set, seeds a `.env` from `.env.example` (never clobbering an existing one), then `go get`s each external module, runs `go mod tidy`, and rebuilds. **If the build fails it restores the original `plugins.go`.**

#### Interactive wizard

Run on a terminal with no stack flags, `herrscher init` prompts for each category (pick a kind or `none`) then reads the active gateway's secrets. The Discord bot token is read with **echo disabled**, so it never appears on screen, and is upserted into `.env` (existing keys preserved).

Pass `--yes`, any stack flag, or run non-interactively (a service / CI pipe) to skip the wizard and take the flags/defaults silently.

#### Config-only mode

When `init` finds **no source checkout** (the common case for a pacman-installed `herrscher`, whose plugins are already compiled in) it runs in **config-only** mode: it skips the plugin menu and the rebuild, collecting only the gateway secrets and writing them to `$HERRSCHER_ENV_FILE` (else `./.env`), then points you at `herrscher serve`.

#### Examples

```bash
herrscher init                                     # interactive wizard on a tty, else default stack
herrscher init --yes                               # the default stack, no prompts, then build
herrscher init --list                              # print the module catalog and exit
herrscher init --memory none --orchestrator none   # drop a category ("none"), no wizard
herrscher init --backend claude --with github.com/acme/extra-plugin   # pin an extra module
herrscher init --no-build                          # rewrite plugins.go + seed .env only
```

#### Flags

| Flag | Meaning |
|------|---------|
| `--gateway K` | Choose the gateway kind (or `none` to drop the category). |
| `--backend K` | Choose the backend kind (or `none`). |
| `--memory K` | Choose the memory kind (or `none`). |
| `--orchestrator K` | Choose the orchestrator kind (or `none`). |
| `--extractor K` | Choose the extractor kind (or `none`). |
| `--skills K` | Choose the skills kind (or `none`). |
| `--with MODULE` | Pin an extra module path verbatim (repeatable). |
| `--list` | Print the catalog and exit. |
| `--no-build` | Stop after writing `plugins.go` (and seeding `.env`). |
| `--yes` | Skip the wizard; take flags/defaults silently. |

Passing any stack flag (`--gateway/--backend/--memory/--orchestrator/--extractor/--skills/--with`) counts as non-interactive intent and skips the wizard.

### `plugin`: list / add / remove

```bash
herrscher plugin list                              # compiled-in plugins
herrscher plugin add github.com/acme/slack-gateway # blank-import, go get, tidy, build
herrscher plugin remove github.com/acme/slack-gateway
```

| Subcommand | Effect |
|------------|--------|
| `list` | Print the currently compiled-in plugins. |
| `add MODULE` | Add the blank import, `go get`, `go mod tidy`, rebuild. |
| `remove MODULE` | Remove the blank import and rebuild. |

### `update`: refresh the plugin modules

```bash
herrscher update                                   # go get -u every plugin, tidy, rebuild
```

`herrscher update` refreshes the **plugin** modules, the counterpart to `herrscher service update`, which pulls the **host's own** source. See [System service](service.md) for the host-side update.

### `install`: build then hand off to the service installer

```bash
herrscher install -- --health-addr :8787           # build the host, then delegate to `service install`
```

`herrscher install` builds the binary then forwards everything after `--` to `herrscher service install`, and it never reimplements the systemd/launchd glue. For what `service install` does per OS, see [System service](service.md).

### Relationship to the runtime CLI

These verbs are distinct from the runtime commands (`serve`, `bridge`, `session`, `agent`, `service`) documented in the CLI reference. The runtime CLI drives a running daemon; the management verbs shape which plugins that daemon is built from.

### See also

- Writing a plugin: implement a contracts port and register it.
- The plugin model: the six categories and how they self-register.
- Layout & wiring: how the module and its plugin siblings fit together.

[herrscher-discord-gateway]: https://github.com/Herrscherd/herrscher-discord-gateway
[herrscher-telegram-gateway]: https://github.com/Herrscherd/herrscher-telegram-gateway
[herrscher-claude-backend]: https://github.com/Herrscherd/herrscher-claude-backend
[herrscher-codex-backend]: https://github.com/Herrscherd/herrscher-codex-backend
[herrscher-cursor-backend]: https://github.com/Herrscherd/herrscher-cursor-backend
[herrscher-pixellab-backend]: https://github.com/Herrscherd/herrscher-pixellab-backend
[herrscher-obsidian-memory]: https://github.com/Herrscherd/herrscher-obsidian-memory
[herrscher-orchestrator]: https://github.com/Herrscherd/herrscher-orchestrator
[herrscher-superset-skills]: https://github.com/Herrscherd/herrscher-superset-skills
[herrscher-jev-compactor]: https://github.com/Herrscherd/herrscher-jev-compactor
