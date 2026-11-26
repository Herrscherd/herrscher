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


## Trust model of devices

A device is a client that drives the daemon from another machine. This section
describes what the daemon does today. The pairing, the registry and the authority
are in place, but no network listener accepts a device yet, so no device can
authenticate, and the signed certificate is not handed to anyone yet.

- **Principal and roles.** A device is the principal `device:<id>`. It is minted
  only from a client certificate that the daemon's own authority verified, and a
  caller that claims the prefix itself is treated as unverified, as `local` and
  `session:` are. A device holds a role through `role grant`, like any principal.
  `approver` runs what `observer` runs, plus `approve allow` and `approve deny`.
  A device with no assignment is an `observer`, even in an empty table.
- **No role table.** While nothing holds a role, a gateway account runs every
  verb the daemon has always let it run, except the device verbs: they are
  refused to any principal other than the local operator, with a message that
  points to `role grant`. Grant a role first, and the table then decides.
- **Who runs the device verbs.** `device pair`, `device list`, `device deny` and
  `device revoke` run for `owner`, `operator` and `local`. `device approve` runs
  for `owner` and `local` only. An `operator` may open a window with
  `device pair --role owner`, but only an owner can sign, so an operator can
  open a window that an owner must validate. `observer`, `approver` and a session
  never run any device verb.
- **Pairing.** `device pair` opens one window at a time and prints a code of ten
  characters from an alphabet without the ambiguous symbols, drawn from the
  operating system's random source. The code is valid for ten minutes and is
  usable once. The window accepts five wrong codes at most: the fifth wrong code
  closes it. The daemon keeps only a salted SHA-256 digest of the code and
  compares digests in constant time. Redeeming the code records the device's
  certificate request as pending and signs nothing. The code is passed on the
  command line, so a process of the same user can read it in `/proc/<pid>/cmdline`
  while the command runs. That is why approval needs more than the code.
- **Approval.** `device approve <code>` only shows the pending request: its name,
  its role and the short fingerprint of the device's key. Nothing is signed.
  `device approve <code> --fingerprint <fingerprint>` signs, and only when the
  fingerprint the operator copies from the device's own display matches the key
  the device sent. Case and separators are ignored, and the comparison is in
  constant time. A wrong fingerprint signs nothing and counts as one wrong attempt.
  The role given to `device pair --role` is granted at approval, in the same save
  as the device record, so a device is never registered without its role.
  `device deny <code>` closes the window without granting anything.
- **Authority.** The authority is an ECDSA P-256 key, created the first time a
  device is approved. P-256 rather than Ed25519 because the Secure Enclave and
  the platform keystores of phones sign with P-256 keys, and the Go TLS stack
  accepts P-256 for client certificates. Its files live in a `devices`
  subdirectory of the state directory, which is created with mode 0700 and holds
  nothing else. The state directory's own mode is not changed. Each key file has
  mode 0600 and is written through a fresh temporary file, then renamed. The key
  is written before the certificate, so a crash leaves a partial pair. A partial
  pair is rebuilt when no device was enrolled under it, and refused once one was,
  since replacing the authority would unpair the devices already signed. The
  daemon also keeps a server certificate signed by the authority, for clients to
  pin later.
- **Keys are not exposed by the commands, and that is all.** No command prints a
  key, and no log line carries one. This holds against the commands, not against
  a process of the same user: such a process can read the authority's key file,
  just as it can read `HERRSCHER_ANNOUNCE_KEY`. The user account is the boundary.
- **Shell administration from an agent.** A Bash command that runs `herrscher device`
  or `herrscher role` is always decided by a person. No rule, no remembered
  "always" answer, no bypass, auto or smart mode lets it through. The check reads
  the command text. A command that builds the verb from variables is not
  recognised, so this covers the commands written as herrscher names them. An
  agent's shell reaches the operator socket as `local`, so the user account stays
  the real boundary.
- **Certificates.** A client certificate is valid for thirty days and states that
  it is not a CA. It is not renewed yet. The approval signs it, but nothing keeps
  or delivers it: the network listener of a later pull request will hand it to the
  device. Until then, a paired device has no certificate the daemon can present.
- **Expiry and revocation.** A device that has not connected for thirty days is
  expired, and so is a device an operator revokes. Either refuses the certificate
  at every connection. `device revoke` keeps the device's role in the table, which
  does nothing while the device stays revoked.
- **Journal.** The daemon logs an opened window with its name and role, a denial
  with the window's name, role and whether a request had redeemed it, an approval
  with the device's identifier and role, and a revocation with the identifier and
  role. A log line never carries the pairing code or a key.
- **Not yet.** The mTLS listener, the ssh route and the Tailscale route are later
  pieces of #320. Until the listener is wired, no request reaches the pairing from
  the network, the daemon refuses any device it does not know, and nobody holds a
  certificate.
