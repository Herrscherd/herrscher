
# System service

For an always-on deployment, don't run `serve` in the foreground: install a native OS service. `herrscher service install` writes a native unit for your OS and a `0600` secrets template. **It never bakes the token into the unit file.**

## Install

```bash
./herrscher service install \
  --cmd "claude --model claude-opus-5-5 --effort low" \
  --health-addr :8787
```

| Flag | Meaning |
|------|---------|
| `--cmd '…'` | The base bridged command baked into the unit. |
| `--health-addr ADDR` | The health endpoint bind. |
| `--env-file PATH` | The secrets file the service sources. |

## Per-OS units

`service install` writes a native unit appropriate to your platform:

| OS | What it creates |
|----|-----------------|
| **Linux** | systemd **user** unit `~/.config/systemd/user/herrscher.service` (`Restart=always`), enables it, runs `loginctl enable-linger` so it survives logout. |
| **macOS** | launchd LaunchAgent `~/Library/LaunchAgents/com.vskstudio.herrscher.plist` (`RunAtLoad`, `KeepAlive`). |
| **Windows** | a Task Scheduler task `herrscher` (on-logon trigger) wrapping `herrscher serve`. |

## Scaffolded files

It also scaffolds these (**never clobbering existing files**):

| File | Purpose |
|------|---------|
| `~/.config/herrscher/herrscher.env` | The `0600` secrets file the service sources: `DISCORD_BOT_TOKEN=`, `DISCORD_USER_ID=`. |
| `~/.config/herrscher/config.json` | The config template (see [Configuration](env.md)). |

Because the token lives only in `herrscher.env` (which the service sources at runtime) and never in the unit, the unit file is safe to inspect and commit patterns around.

## Fill the token and start

```bash
$EDITOR ~/.config/herrscher/herrscher.env      # set DISCORD_BOT_TOKEN
./herrscher service restart
./herrscher service status
```

| Command | Effect |
|---------|--------|
| `service restart` | Restart the service. |
| `service status` | Show the current service status. |

## Update an installed service

```bash
cd herrscher
./herrscher service update           # git pull --ff-only, rebuild the installed binary, restart
./herrscher service update --no-pull # rebuild from local source only
```

`service update` rebuilds the **installed** binary (not the one you invoked) and schedules the restart **out-of-band** (on Linux via `systemd-run`) so it survives the daemon being killed mid-restart.

| Flag | Effect |
|------|--------|
| (default) | `git pull --ff-only` in the `--source` checkout, rebuild, restart. |
| `--no-pull` | Rebuild from local source only (no `git pull`). |
| `--source DIR` | The checkout to pull and rebuild. Defaults to the current directory. |

This is the host-side update. To refresh the **plugin** modules instead, use `herrscher update`, see [Managing plugins](plugins.md).

## Uninstall

```bash
./herrscher service uninstall        # disable + remove the unit (leaves your config/secrets)
```

Uninstall disables and removes the unit but **leaves your `config.json` and `herrscher.env`** in place, so a later reinstall picks up where you left off.

## See also

- [Installation](install.md): build the binary before installing the service.
- [Configuration](env.md): the `config.json` fields and `.env` precedence.
- CLI reference: the full `service` flag table.
- [Managing plugins](plugins.md): `herrscher install` builds then delegates to `service install`.
