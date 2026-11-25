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

The token and the forge port only ever go to github.com. Opening a pull request
works on github.com only: the forge port does not support GitHub Enterprise
Server yet, so a pull request for another host is refused. A repository on
another host, whether its URL is `https://`, `git@` or `ssh://`, is cloned
anonymously or with your own git or ssh setup, never with the herrscher token.

If the GitHub App is not installed on the account or the instance, or the forge
refuses its client ID or has device sign-in turned off for it, the command says
so and suggests the two alternatives below.

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

The lock files stay in the state directory. Herrscher never deletes them:
removing a lock file that another process may hold would let two processes in
at once. The shared lock is `.forge-connections.json.lock`, and each host
whose token has been renewed leaves an empty
`.forge-connections.json.<sha256>.renew.lock` there. When no herrscher process
is running, you can delete these empty files by hand; herrscher creates them
again when it needs them.

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

`herrscher health` adds a GitHub line to the health table: the token source and the
login, the permission on the repository of the current directory (the session worktree
when an agent runs it) when its origin is on github.com, and the requests left before
the rate limit resets. Without a connection
it is a warning that points to `herrscher connect github`, and a refused token is an
error asking you to connect again. The token is never printed.

The same state shows in `setup`, in the first section of the dashboard named
`connections`. It reads the connection as `connected as <login>, token source: <source>` or
`not connected`, and offers a connect action that runs the same device sign-in as
`herrscher connect github`, with the code to enter on GitHub and the link to install
the App. Skipping the section changes nothing.

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
  `herrscher connect github` again. Herrscher warns once while saves keep
  failing, and again only after a save has succeeded and a later one fails.
- **The connections file cannot be read, but a renewed token is held in
  memory.** The token kept in memory is used while it is still valid, and
  herrscher tries to save it again at each use. Once it has expired, forge
  operations fail with the read error.

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

Cloning a repository for a session and opening its pull request from the
terminal use the same token, and only for `github.com`. An `owner/name` or an
`https://github.com/` URL is cloned with the token when one is held. A `git@` or
`ssh://` URL, and any repository on another host, is cloned without the token,
with your own git or ssh setup. A host name without a dot, such as an intranet
`gitlab`, is accepted in the `git@`, `ssh://` and `https://` forms and cloned
the same way. A pull request for a host other than github.com is refused. The
token goes to git through its environment, never on the command line or in the
clone's configuration.

## Pull requests of a session

A session worktree can open and follow its pull request from the terminal, with
the same token and only for github.com:

- `herrscher session pr open --name <session> --title <title> [--body <text>] [--base <branch>] [--draft]`
  opens the pull request of the session branch. It does not push: the local
  HEAD must already be on the remote branch, or the command refuses. The origin
  must be a github.com repository. The base defaults to the branch that the
  local `origin/HEAD` names, and when that ref is missing `--base` is required.
  The session state records the number, the state, the draft flag and the URL of
  the pull request, all at once, and a session that already has one is refused.
  A session that runs on another host is refused
  too, and a second open while the first is still running is refused. If GitHub
  opens the pull request but the number cannot be recorded, the command reports
  the URL and the number so the pull request is not lost.
- `herrscher session pr status --name <session>` reads the pull request again,
  stores its state, draft flag and URL in the session, and shows the title, URL,
  state, draft flag, mergeability and head commit.
- `herrscher session pr checks --name <session>` lists the check runs of its head
  commit, reading every page up to a fixed bound.
- `herrscher session pr comments --name <session>` lists the review threads, the
  unresolved ones first, with their comments. Forge text is sanitized before it
  is printed.

Each command accepts `--json` for the raw structure, whose state and check words
stay in English. Human output and errors are printed in the language of the
session, English or French. A missing connection names `herrscher connect github`
as the remedy, a rate limit gives the time of the reset, and a session without a
pull request says to run `session pr open` first. The token reaches git only
through its environment, never through arguments, and it does not appear in
output or errors.

A pull request opened from the terminal panel (`p` in the diff panel) goes through
the same `session pr open` command, so it is recorded in the session like one
opened from the command line.

`herrscher session list` shows the pull request of each session that has one,
after the channel of the session, as `PR #42 open`, `PR #42 draft`,
`PR #42 merged` or `PR #42 unknown state`. A draft is an open pull request
marked as a draft. The `--json` form adds a `pullRequest` object with `number`,
`state` (`open`, `closed`, `merged` or `unknown`, always in English), `draft` and
`url`. A session without a pull request omits the field.

A session whose pull request was recorded before its state was stored keeps its
number with the state `unknown`. The session info that the TUI and the gateways
receive carries no pull request for such a session, so they show nothing until
`session pr status` stores the state, draft flag and URL.

## Repositories to clone

When you create a session, the choice of repositories lists the workspace
checkouts already on disk, and the repositories of the forges the host is
connected to.

- **GitHub** lists every repository the connected account can see, shown as
  `owner/name` with its description. Without a GitHub connection, no GitHub
  repository is listed, and this is not an error. Choosing an entry clones
  `owner/name` from `github.com`, with the token when one is held. Without a
  token, the clone is anonymous, so a private repository cannot be cloned.
- **GitLab** is listed only when `glab` is installed, from `glab repo list`.
  The choice shows the ssh clone URL of each project, such as
  `git@gitlab.com:group/project.git`, because that URL is what the clone
  receives. Choosing an entry clones it over ssh with your own key, for any
  GitLab host: private repositories and self-hosted instances included, whether
  the host name has a dot (`gitlab.example.com`) or none (`gitlab`, an intranet
  name). A project that lists no ssh URL falls back to its https clone URL,
  cloned anonymously. The herrscher token never goes to GitLab, and a GitLab
  entry is never cloned from `github.com`, even when a GitHub repository has the
  same path.

## Troubleshooting

- **The App is not available for this host.** Either the host has no herrscher
  App (GitHub Enterprise Server without it), or the App's client ID is refused
  there, or the device sign-in is turned off in the App's settings. Install the
  GitHub App on the account or instance (the install link is printed by
  `connect status`), or use `GH_TOKEN` or `gh auth login`. If the host should
  accept the App, report the problem to the herrscher maintainers.
- **A saved token is marked as refused.** Run `herrscher connect github` again
  for that host. A refused token is never used, and the next source takes over
  until then.
- **The device code expired or was denied.** Run `herrscher connect github`
  again and approve within the time the code gives.
- **The saved connections file cannot be read.** Status names its path and
  says to run `herrscher connect logout`, which removes it, then connect again.
- **The forge does not accept the App's credentials.** The client settings of
  the App are wrong on that forge, so its renewals fail. This is not a refused
  token: the saved connection is not marked refused, and no other source takes
  its place.

  `connect status` shows that the herrscher GitHub App is misconfigured on the
  forge, and asks you to report it to the herrscher maintainers, only when that
  same `connect status` run tried a renewal and the forge refused the App's
  credentials. A renewal is tried when the saved access token is due, that is in
  its last five minutes or after it expired. A renewal run by another herrscher
  process, such as the daemon, is not visible to `connect status`.

  After such a refusal, herrscher does not send the renewal to the forge again
  for the same host for one minute. While the saved token is still valid, forge
  operations keep using it and log one warning for the refusal that names the
  host, never the token. Once it expires, forge operations fail with an error
  that ends with `the forge does not accept the herrscher app credentials`.
  Renewal is tried again when the token is due and that minute has passed, and
  fixing the App restores renewals.
