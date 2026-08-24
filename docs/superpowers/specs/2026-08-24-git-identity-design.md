# What git already knows about the human

Date: 2026-08-24
Status: approved, implementing
Scope: `core/identity`, the turn context, `herrscher whoami`. The companion
change in `herrscher-obsidian-memory` (de-personalising the example vault) ships
as its own pull request in that repository.

## Problem

`herrscher-obsidian-memory/examples/vault/user/preferences.md` carries a real
person's identity, committed in the open:

```
Identity: GitHub Akayashuu, commit email sauvageleo1@gmail.com.
```

That file is only the golden vault the tests in `golden_test.go` read, so nothing
in the product depends on those values. Which is the actual finding: **no code
path ever produces a `user` node.** `Provisioner` (`herrscher-contracts/memory.go:123`)
ensures a project root and an agent root; a `KindUser` node exists in the kind
list (`memory.go:20`) and in the CLI's kind filter (`core/host/cli.go:265`), but
only an agent writing one by hand ever creates it.

So the identity in the example vault is not a configuration with a bad default.
It is a hardcoded stand-in for something herrscher never learned to find out.

Meanwhile the daemon already runs `git` to learn where it is:
`scope.ProjectFromDir` (`core/scope/scope.go:37`) shells one `git rev-parse` to
name the memory project a directory belongs to, and answers `""` — never an
error — when git is absent or silent. The same machine knows *who* it is
working for, in the config git has kept all along, and herrscher never asks.

## What we build

Three pieces, none of which touch a contract.

### 1. `core/identity` — resolution

A leaf package, the twin of `core/scope`: standard library only, no import of
either gateway, git absent or mute means silence rather than failure.

```go
// Identity is what git knows about the human at the keyboard. Every field is
// optional; the zero value means git had nothing to say.
type Identity struct {
	Name   string
	Email  string
	GitHub string
}

func FromDir(dir string) Identity
func (i Identity) Empty() bool
func (i Identity) String() string
```

`FromDir` runs three `git -C <dir> config --get <key>` calls — `user.name`,
`user.email`, `github.user` — each bounded by the 2s timeout `core/scope`
already uses, for the reason it uses it: a launch must not hang on a repository
whose objects live on a stalled network mount.

Precedence is not our problem to solve. `git config --get` already answers with
the repository's local value where there is one and the global value otherwise,
which is exactly the behaviour that makes this feature need no configuration:
an operator who sets a per-repository `user.email` is honoured for free.

`String()` renders `"Nom <mail> (@github)"`, omitting the parts git did not
answer, and `""` when it answered nothing.

**The GitHub id is not derived from the `origin` remote.** A remote's owner is
frequently an organisation — this project's own remote is `Herrscherd`, and the
human is `Akayashuu` — so deriving it would manufacture a wrong identity wearing
the shape of a right one. `github.user` is the conventional key `gh` and `hub`
write; when it is unset we say nothing, which is the same contract every other
field holds.

### 2. Exposure in the turn

`core/bridge/hub.go:260` builds the turn's context by stacking decorators:

```go
Context: withCapabilities(withDelegation(withSkills(memCtx, eng), aff.roster), aff.caps)
```

`withIdentity` joins that stack in the shape `withCapabilities`
(`core/bridge/capabilities.go:24`) established: it emits a `<user>` block, and
returns `baseCtx` untouched when the identity is empty — so a machine where git
said nothing pays zero bytes of context.

The identity is resolved **once per session** and carried on `affordances`, not
re-shelled per turn.

The agent is not made to write anything. It knows who is talking to it, and if
it judges that worth keeping it records the node with the memory verbs that
already exist. No new capability, no new contract, no forced vault write.

### 3. `herrscher whoami`

Prints what herrscher believes, and where each part came from, so a wrong
identity is diagnosed in one command instead of a billed agent turn:

```
name    Léo Sauvage        (git config user.name)
email   leo@example.com    (git config user.email)
github  —                  (git config github.user, unset)
```

Outside a repository, or with nothing configured, it says git had nothing to say
and exits 0. A silence is not a failure.

## Testing

- `core/identity`: temporary repositories built the way `core/scope/scope_test.go`
  builds them — a local value overriding a global one, a repository with no
  identity at all, a directory in no repository, `github.user` unset. Every case
  asserts a field-by-field `Identity`, never a formatted string.
- `withIdentity`: an empty identity leaves the context byte-identical; a full one
  appends a well-formed block after the capabilities section.
- `whoami`: golden output for the full case and the empty case.

## Out of scope

De-personalising the example vault removes the address from `HEAD`, not from the
repository's history, which stays readable. Purging it would mean rewriting
history and force-pushing — a separate decision, deliberately not taken here.
