# Herrscher

**A self-hosted harness for running fleets of AI coding agents.**

A session is a conversation, an agent, and an isolated git worktree. It is supervised, so it survives a restart. It has a memory, so it knows what happened last week. It can hand work to another session, on another model vendor, without you in the middle. You talk to it from Discord, from the terminal UI that ships in the binary, or from a gateway you write yourself.

## Install and run

```bash
curl -fsSL https://raw.githubusercontent.com/Herrscherd/herrscher/master/scripts/install.sh | sh
```

The script downloads the latest release for Linux or macOS (amd64 or arm64),
checks it against `checksums.txt` and installs `herrscher` in `~/.local/bin`
(`HERRSCHER_INSTALL_DIR` changes the directory, `HERRSCHER_VERSION` pins a tag).
The checksum comes from the same release as the archive, so it catches a
corrupted download, not a release replaced by someone with write access to the
repository.
Windows archives, and every other platform, are on the
[releases page](https://github.com/Herrscherd/herrscher/releases). With Go 1.27:

```bash
go install github.com/Herrscherd/herrscher@latest
```

On Arch Linux, `makepkg -si` next to the `PKGBUILD` builds the `herrscher`
package from the `master` branch on GitHub. It replaces the older `herrscherd`
package, whose binary was `/usr/bin/herrscherd`: a service installed from it still
starts that path, so run `herrscher service install` again after the upgrade.

A prebuilt binary carries the default stack. Changing the compiled modules
(`herrscher plugin`, `herrscher update`, the modules screen of `setup`) rebuilds
from source and needs Go 1.27 and a checkout:

```bash
git clone https://github.com/Herrscherd/herrscher.git
cd herrscher
go build -o herrscher .    # the only binary; plugins are compiled in
```

## First turn

Then:

```bash
herrscher setup            # pick modules, sign in, configure everything
herrscher                  # serves, with the terminal UI in the foreground
```

`setup` opens an interactive screen: you check the modules you want (gateways, backends, memory, skills), then each one is configured in turn. The terminal path needs no Discord token; only the Discord gateway asks for one.

A first task needs no daemon to start: `herrscher "read the thread and propose a split"` opens a session on that task and shows its turn in the terminal UI.

## The five commands to know

- `herrscher setup`: choose the modules and sign-ins, and write the configuration.
- `herrscher "<task>"`: open a session on a task and watch its turn.
- `herrscher session create|list|close|set-budget`: create, list, close and budget sessions.
- `herrscher serve --health-addr :8787`: run the daemon in the foreground.
- `herrscher service install|status|restart|update`: keep the daemon running as a boot-started service.

## Documentation

Start with the [documentation index](docs/README.md). The main guides are:

- [Install and run](docs/install.md): building, the first session, starting work, sockets.
- [Sessions](docs/sessions.md): what a session gives you, from memory to delegation.
- [Remote sessions](docs/remote.md): sessions on another machine, and their trust model.
- [Approvals](docs/approvals.md): tool calls under policy, and who may change the policy.
- [Observability](docs/observability.md): following a session, health, usage, model routing.
- [Terminal UI](docs/terminal-ui.md): the window, its keys, and the keys not bound yet.
- [Plugins](docs/plugins.md): the compiled-in plugins, managing them, languages.
- [Architecture](docs/architecture.md) and [Testing](docs/testing.md).
- Reference: [service](docs/service.md), [skills](docs/skills.md), [budgets](docs/budgets.md), [environment variables](docs/env.md).
