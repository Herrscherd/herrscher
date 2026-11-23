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

The access token expires after eight hours. Herrscher refreshes it with the
refresh token a few minutes before it expires, and saves the new pair. You do
not have to reconnect.

For GitHub Enterprise Server, pass `--host` with the instance name. The device
flow then runs against that host.

If the GitHub App is not installed on the account or the instance, the command
says so and suggests the two alternatives below.

## Check the connection

```sh
herrscher connect status
```

For each known host, the output gives:

- the source of the active token: `saved`, `env` (an environment variable),
  `cli` (the `gh` login), or `none`;
- the login of the account, when it can be read;
- when the saved access token expires, or that it was refused;
- the link to install the GitHub App and the link to revoke it.

A saved token the forge has refused is marked as refused. The token itself is
kept until you connect again or log out.

## Log out

```sh
herrscher connect logout
herrscher connect logout --host ghe.example.com
```

This removes the saved tokens of the host. It does not revoke them on GitHub:
to revoke the authorization itself, use the revoke link printed by
`herrscher connect status`.

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
  for that host. A refused token is never used, and the next fallback takes
  over until then.
- **The device code expired or was denied.** Run `herrscher connect github`
  again and approve within the time the code gives.
