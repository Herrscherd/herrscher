# Remote sessions

## Sessions that run on another machine

```bash
herrscher host add build1 --ssh me@build1 --workspace /srv/work
herrscher session create --name x --host build1
```

The whole session lives over there: its worktree, its agent files, its backend
process. An agent can carry a default host, so a fleet member always lands on the
machine its work belongs to.

Registering provisions. Herrscher reads the target's platform, cross-compiles a
binary for it, copies it into `~/.herrscher/bin`, and records the version it put
there. There is nothing to install by hand and nothing to keep in step.

The daemon stays the only one holding state, and the remote side is a runner
rather than a second daemon. The session's control socket and this daemon's
command socket are forwarded back over the same ssh connection, so the bridge
over there dials a unix socket exactly as it does here, and `herrscher <verb>` in
the agent's own shell reaches the daemon that started it. Each session forwards
its own path and each launch owns its connection, so a second session on the same
host cannot take the first one's socket away.

Every socket lives in the per-user directory on the remote machine too. The daemon
resolves that directory over ssh once per host, with the same rule as locally
(`/run/user/<uid>/herrscher` when the systemd user session provides it,
`/tmp/herrscher-<uid>` otherwise), and creates it with mode 0700 if it is missing.
A session forwarded by a build from before that change keeps dialling the old `/tmp`
paths while the new daemon listens in the new directory. Restart each remote session once after the upgrade to
forward the new paths. Until then its bridge cannot reach the daemon, and the
session shows no answer.

Three refusals replace a blind start: a host that was never provisioned, a host
running a different build than this daemon would ship, and `--clone` combined
with `--host`, since the forge client runs here and would clone onto the wrong
machine. `host check build1` says in four lines whether ssh, the binary, the
workspace and git are there.

## Trust model of remote sessions

A session on a remote backend is a runner over there, and the daemon keeps every
decision. What crosses the network and what stays here:

- **Crosses the network:** the prompt of each turn, as the backend receives it
  (its content and its settings). On an SSH placement, the remote environment also
  carries the session's name and its approval settings (mode, session, binary).
  The daemon's approval answers come back on the same connection.
- **Stays on this machine:** the session token (`HERRSCHER_SESSION_TOKEN`). It is
  never written into the remote environment, so no process on the remote side can
  answer an approval for any session. The local outbox path is dropped from the
  prompt, because it names a directory of this machine. A gateway model and an
  account are refused on a remote backend, since they resolve in this daemon's
  store.
- **Who decides an approval:** the daemon, always. The remote backend asks through
  the approval stream the turn carries, and the answer is the daemon's desk
  decision for that session. A turn that reaches the remote backend without an
  asker opens no approval stream, as
  `TestARemoteBackendWithoutAnAskerTakesThePlainPath` proves. An approval that
  reaches the transport proxy without an asker is refused, as
  `TestTheTransportProxyDeniesAnApprovalWithoutAnAsker` proves, so nothing is
  granted by default. A session's approvals reach the daemon desk over mTLS and
  the desk's decision comes back, as
  `TestAnApprovalOnTheRemoteTurnComesBackWithTheDaemonsDecision` proves.
- **Daemon unreachable:** a remote session needs the daemon's address and the
  backend's announcement to start. When the resolver cannot reach the remote
  backend, the turn fails with that error and nothing runs in its place on this
  machine.
- **Attribution:** the session's name reaches the remote environment on SSH, but
  the approval stream does not carry it. The daemon attributes each approval to
  the session whose turn asked it, through the asker bound to that turn in the
  daemon (`askerFor` in `core/host/approval_desk.go`), so the name is
  informational on the remote side and never the basis of a grant.

No token reaches a bridge on a remote machine: `remoteEnvBlock` does not carry
`HERRSCHER_SESSION_TOKEN`, so the bridge's local hook has no credential to present.
Its approvals go through the asker that the turn attaches (`core/bridge/approval.go`).


## Route ssh

`herrscher proxy <command|events>` relays one local socket of the daemon over its
standard input and output. A client that wants to drive a daemon on another machine
runs it through ssh, one channel per socket, so no port opens on either side:

```bash
ssh build1 herrscher proxy command    # one request line in, one answer out
ssh build1 herrscher proxy events     # NDJSON stream of events
```

The command socket takes the same line as on this machine: a JSON object with
`argv`, and optionally `token` and `lang`, then one answer line. The events socket
takes the cursor line that `SubscribeSessionEvents` writes first, the session and
the sequence number already seen, then streams. The proxy never reads these
messages: it copies bytes in both directions.

Trust model: the route is trusted as local. The ssh login authenticates the
client, and the proxy runs as the user that owns the daemon, so it reaches the
sockets exactly as a process on this machine would. Scopes do not apply on this
route, since they belong to the network routes. The session tokens stay in the
daemon: the proxy forwards whatever token the client sends and never creates one.

Ending a channel:

- Closing stdin ends the request side. When stdin ends before the daemon closes,
  every byte read from it is sent first, and the daemon then sees end of input on
  its socket.
- The proxy returns when the daemon closes its side, with status 0. It does not wait
  for stdin to end: a read still blocked on stdin, such as a terminal or a pipe that
  stays open, is left behind with the process, and input not yet read by then is not
  sent.
- When no daemon listens for the channel, the proxy prints that the daemon does
  not answer on this machine and exits with status 1. The client learns the
  daemon is absent from that exit status, not from a socket that hangs.

Go code that opens the route calls `host.DialProxy(ctx, target, bin, channel)`. It
starts `ssh <target> <bin> proxy <channel>`, where `bin` is the binary that
`host add` recorded in `~/.herrscher/bin`, so the login PATH does not matter. An
empty `bin` falls back to `herrscher` on the login PATH. The returned connection
reads what the daemon answers and writes to it.

The approval channel comes with its server and is not part of this route yet.
Meanwhile `herrscher approve` goes through the command socket, which
`herrscher proxy command` relays.


## Plugins on another machine

A plugin category can run in a plugin-host on another machine. Name the category
in `HERRSCHER_REMOTE` and point `HERRSCHER_NATS` at the shared NATS server. Two
settings guard it:

- `HERRSCHER_ANNOUNCE_KEY` authenticates the announcements a plugin-host publishes.
  Every host and plugin-host that exchanges them must hold the same secret of at
  least 32 random bytes, and a host with a remote category refuses to start
  without a valid one. The key signs announcements only: it neither encrypts the
  traffic nor grants a caller any right. The hub, its plugin-hosts and its bridges
  receive it. It is kept out of the environment the agent's vendor CLIs inherit,
  but processes running as the same user can still read it, from the `.env` file
  or from `/proc`, so the boundary is the user account.
- The gRPC connections use mTLS (`HERRSCHER_TLS_CA`, `HERRSCHER_TLS_CERT`,
  `HERRSCHER_TLS_KEY`). With mTLS configured, the host dials any address an
  authenticated announcement advertises. Without it, the host dials loopback
  addresses only, so plaintext traffic never leaves the machine.

