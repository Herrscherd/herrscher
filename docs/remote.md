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

### Pinning the host key

`host add` does not trust a machine on first contact. Without `--host-key` it
connects once, prints the SHA256 fingerprint of the key the server presented, and
registers nothing:

```bash
herrscher host add build1 --ssh me@build1 --workspace /srv/work
# host key of build1: SHA256:... Check it on the machine itself, then repeat ...
herrscher host add build1 --ssh me@build1 --workspace /srv/work --host-key SHA256:...
```

The fingerprint is checked on the machine itself (`ssh-keygen -lf
/etc/ssh/ssh_host_ed25519_key.pub` there), then passed back with `--host-key`. A
value that does not match what the server presents now registers nothing. Once
confirmed, the key is written to a known_hosts file that only herrscher reads
for this host, under the state directory. Every connection to the host uses it
with `StrictHostKeyChecking=yes`, and the user's own known_hosts is not consulted.

A key that later differs from the pinned one blocks the host. The connection
itself is refused, `host check build1` records the key it saw and reports the
change, and `host list` shows the host as blocked. Nothing repairs the pin on its
own. Once the change is verified on the machine, `host trust build1 --host-key
SHA256:...` accepts the new key, and the same command without the flag only shows
the fingerprint, as `host add` does.

### Binary digest

Provisioning records the SHA256 of the herrscher binary it copied to the host, and
checks that the copy matches the staged file before it is ever run. Every
launch of a remote session digests the binary again and refuses to start when it
differs from the record, in addition to the version check at session create.
`host check` reports a binary that changed or has no recorded digest.

### Projects, session cap and load

A host can restrict what runs on it and how much:

```bash
herrscher host add build1 --ssh me@build1 --workspace /srv/work \
  --host-key SHA256:... --allow-project app,api --max-sessions 4 --when-full wait
herrscher host set build1 --allow-project app
herrscher host set build1 --allow-all-projects
```

- `--allow-project` takes a comma-separated list of project names, as the daemon
  resolves them in the workspace. A session on another project is refused on that
  host before anything is cloned. An empty list, the default, allows every
  project, which is what hosts registered before this change keep doing.
- `--max-sessions` caps the sessions running on the host at once. Zero, the
  default, means no cap.
- `--when-full refuse` (the default) fails a session that meets a full or
  unreachable host. `--when-full wait` holds it until a slot frees, for at most
  ten minutes, and then fails it. A session never moves to another host on its
  own: placing it elsewhere is always the operator's choice.
- `host check` reads the 1-minute load average on the host and stores it, and
  `host list` shows each host's key state, sessions and last measured load. The
  same figures are in `host list --json`.

### Hosts registered before pinning

A host registered before this change has no pinned key and no binary digest. It
keeps working with the ssh defaults and with no project or cap limit, and each
session start on it prints a warning until it is pinned. `host check` shows the
fingerprint to confirm, and `host trust <name> --host-key SHA256:...` pins it.
`host provision <name>` records the binary digest.

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

