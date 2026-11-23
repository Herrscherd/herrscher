# GitHub connection

Herrscher reads a GitHub token for every forge operation: pull requests, issues,
reviews, checks. This guide shows how to give it one, check which one it uses,
and remove it.

## Connect with the GitHub App

```sh
herrscher connect github
herrscher connect github --host ghe.example.com
```

The command starts the device flow of the herrscher GitHub App. It prints a
verification URL and a one-time code. Open the URL, enter the code, and approve.
Herrscher then saves the access token and the refresh token in
`forge-connections.json` in the state directory (`HERRSCHER_STATE_DIR`, by
default `~/.config/herrscher`). The file is readable by its owner only (mode
`0600`). Tokens are never printed, never passed on the command line and never
written to a log.

The access token expires after eight hours. Herrscher renews it with the refresh
token five minutes before it expires, and saves the new access token together
with the new refresh token: GitHub replaces the refresh token at each renewal.
You do not have to reconnect.

The refresh token has its own lifetime. When it runs out, the connection cannot
be renewed. Status warns you 14 days before that date, and you run
`herrscher connect github` again once it has passed.

For GitHub Enterprise Server, pass `--host` with the instance name. The device
flow then runs against that host.

If the GitHub App is not installed on the account or the instance, the command
says so and suggests the two alternatives below.

## Several herrscher processes

Herrscher processes that share a state directory coordinate through lock files
in that directory:

- A renewal holds a lock for its own host only. The lock file name is derived
  from a hash of the host, so a host with a port is handled like any other. A
  slow renewal of one host never blocks the token of another host.
- A process that needs a token while another process renews the same host waits
  for that renewal, then uses the token it saved. The refresh token is spent
  once.
- Each change to `forge-connections.json` runs under a short lock that is held
  only while the file is read, changed and written. No lock is held during a
  network call.
- A renewal never overwrites a pair saved meanwhile, and never brings back a
  connection that was logged out while it was running.

## Check the connection

```sh
herrscher connect status
```

For each known host, the output gives:

- the source of the active token: `saved`, `env` (an environment variable),
  `cli` (the `gh` login), or `none`;
- the login of the account, when it can be read;
- until when the saved connection can be renewed, that it expired, that it was
  refused, or that the GitHub App is misconfigured on the forge;
- the link to install the GitHub App and the link to revoke it.

Status reads the saved connection through the same path as forge operations, so
a saved token that is due for renewal is renewed by `status` too.

A saved token the forge has refused is marked as refused. The token itself is
kept until you connect again or log out. An expired connection is not sent to
the forge: no renewal is attempted with it, and status falls back to the other
sources as forge operations do.

## Warnings

Herrscher prints these warnings. Each one names the file or the host, never a
token.

- **The connections file is readable by other users.** Status warns with its
  path. Connect again to rewrite the file with owner-only access.
- **The connections file cannot be read.** Status names its path and says to run
  `herrscher connect logout`, which removes it, then connect again.
- **The renewed connection could not be saved.** The renewed token is kept in
  memory and used by this process. GitHub has already replaced the refresh token
  that the file still holds, so when this process stops before the file is saved
  again, the saved connection is refused at the next start and you run
  `herrscher connect github` again.
- **The connections file cannot be read, but a renewed token is held in
  memory.** The token kept in memory is used while it is still valid.

## Log out

```sh
herrscher connect logout
herrscher connect logout --host ghe.example.com
```

This removes the saved tokens of the host from the file. It does not revoke
them on GitHub: the grant of the herrscher App stays active until you revoke it.
Logout prints the page where you revoke it, and status prints the same link. If
the file of saved connections cannot be read, logout removes the whole file.

## Without the GitHub App

Herrscher also takes a token from the environment or from the GitHub CLI, in
this order (see [Environment variables](env.md#forge-github)):

1. the token saved by `herrscher connect github` for the host;
2. `GH_TOKEN`, then `GITHUB_TOKEN`, for `github.com`, or for the host named by
   `GH_HOST`;
3. the login of `gh auth token --hostname <host>`.

The first valid token wins. If none of them holds a token, forge operations
fail with a message saying herrscher is not connected to that host.

## Troubleshooting

- **The App is not available for this host.** Install the GitHub App on the
  account or instance (the install link is printed by `connect status`), or use
  `GH_TOKEN` or `gh auth login`.
- **A saved token is marked as refused.** Run `herrscher connect github` again
  for that host. A refused token is never used, and the next source takes over
  until then.
- **The device code expired or was denied.** Run `herrscher connect github`
  again and approve within the time the code gives.
- **The saved connections file cannot be read.** Status names its path and
  says to run `herrscher connect logout`, which removes it, then connect again.
- **The forge does not accept the App's credentials.** The client settings of
  the App are wrong on that forge, so its renewals fail. Status then shows that
  the herrscher GitHub App is misconfigured on the forge and asks you to report
  it to the herrscher maintainers. This is not a refused token: the saved
  connection is not marked refused, and no other source takes its place.

  While the saved token is due for renewal, forge operations fail with an error
  that ends with `the forge does not accept the herrscher app credentials`, even
  if the saved token still works. Once the token is no longer due for renewal, it keeps
  being used until it expires, and renewal is tried again only when it is due.
  Fixing the App restores renewals.
