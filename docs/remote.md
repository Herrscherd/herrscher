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


## Discovery

`herrscher host discover` lists the machines a session could run on. It reads
three sources and proposes what it finds, without adding anything. Every source
has the same deadline of two seconds, and a source that does not answer by then
is listed as unavailable while the others are still shown:

- **Local network (`lan`):** a DNS-SD browse for `_herrscher._tcp` lasting two
  seconds. It only sends multicast questions and changes nothing on the network.
  The browse reads answers as they arrive. The multicast library drops an answer
  when its buffer is full, so a burst of more than 256 answers that the reader
  has not taken yet is lost without a message.
- **Tailnet (`tailnet`):** the online peers that tailscaled reports on its local
  socket, read up to 4 MiB. Without Tailscale on the machine, this source is empty
  and silent. A socket that accepts and never answers is unavailable.
- **SSH configuration (`ssh`):** the concrete `Host` aliases of `~/.ssh/config`.
  Patterns with wildcards (`*`, `?`) or negations (`!`) are skipped, and files
  named by `Include` directives are not followed.

Every candidate is unverified, including a daemon that announces itself. Names
and addresses that come from the network are cleaned before they are shown: control
characters, the C1 range and bidirectional overrides are removed, so a machine
cannot write to the terminal of the operator. Addresses are compared in lowercase
without the trailing dot.

Candidates that share a route, a name, an address and an ssh target are listed
once. The same machine found on two routes stays on two lines, one per route,
because each route leads to a different `herrscher host add` command. The lines are
not merged across routes.

The output shows the route, the fingerprint when one is announced, and the
`herrscher host add` command that would register the candidate. The command is
proposed only when the name and the target are plain host names (letters, digits,
`.`, `-`, `_`, `@`, `:` and square brackets, starting with a letter, a digit or a
bracket). Any other name gets no command, only a line that says so. The ssh alias
and the tailnet name shown are the real ones; the placeholders stay for what the
operator still has to choose: `<name>` for a daemon found on the local network,
`<user>` when no ssh alias exists, and `<path>` for the workspace. Nothing is
written until the command runs.

### What a daemon announces

This is the planned behavior of the announcement. It is not wired yet: no daemon
publishes it today, and `device pair` does not use it. Once wired, an announcement
only exists while a pairing window is open, and a window lasts at most ten minutes. It is a DNS-SD service instance named `herrscher-<fingerprint>` with
two TXT fields:

- `proto=1`, the revision of the discovery protocol;
- `fp=<fingerprint>`, sixteen lowercase hex digits from the daemon's key.

Its SRV target is `herrscher-<fingerprint>.local.`, so the machine's own host name
is never published. The port and the addresses of the daemon go in the SRV and A
records, since a client needs them to connect. An announcement without any address
is refused, because a resolver would then look up the instance by name and learn
the fingerprint.

The announcement never carries a user name, a session name, a build version, or
any other TXT field. `TestTheAnnouncementNeverNamesTheMachine` and
`TestAnAnnouncementPublishesOnlyPresenceAndFingerprint` in `core/discovery` hold
that rule. When the announcer is wired to `device pair`, it follows the device
work (#323) and stays behind a config flag that is off by default.

### What discovery never does

- It never trusts a machine it finds. A daemon on the local network still needs
  to be paired or added, and its key checked, before a session runs there.
- It never adds a host, and it never writes the ssh configuration or the tailnet.
- It never reads the content of a session, a token or a secret from a peer.
