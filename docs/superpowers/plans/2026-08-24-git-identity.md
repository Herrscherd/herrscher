# Git Identity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When git is installed and configured, herrscher knows who the human at the keyboard is and says so — in the turn's context and in a `whoami` verb — instead of anyone hardcoding an identity anywhere.

**Architecture:** A leaf package `core/identity` shells `git config --get` the way `core/scope` shells `git rev-parse`: bounded, best-effort, silent when git has nothing to say. `core/bridge` resolves it once per session, carries it on `affordances`, and renders it into the turn context with a `withIdentity` decorator built in the shape of `withCapabilities`. `core/host` registers a `whoami` verb that prints what was resolved and which git key each part came from.

**Tech Stack:** Go, standard library only. Tests use real `git` against `t.TempDir()` repositories, following `core/scope/scope_test.go`.

## Global Constraints

- `core/identity` imports the standard library only — no contracts, no other `core/…` package. It is a leaf, like `core/scope`.
- Every git invocation is bounded by a `context.WithTimeout` of `2 * time.Second`, the value and the reason `core/scope/scope.go:21` documents.
- git absent, git failing, or git answering nothing is **never an error**: the zero `Identity` is a silence, and every caller renders silence as "nothing to say", not as a failure.
- The GitHub id comes from `git config --get github.user` only. It is never derived from a remote URL — a remote's owner is often an organisation, not the human.
- Tests set `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_SYSTEM=/dev/null` on every git command they run, exactly as `core/scope/scope_test.go:22` does, so the developer's own config cannot leak into an assertion.
- Commit messages end with the session trailer this repository uses:
  `Claude-Session: https://claude.ai/code/session_01SALhQ5ggq2Q2A33EqHbh2h`

---

### Task 1: `core/identity`

**Files:**
- Create: `core/identity/identity.go`
- Test: `core/identity/identity_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Identity struct { Name, Email, GitHub string }`
  - `func FromDir(dir string) Identity`
  - `func (i Identity) Empty() bool`
  - `func (i Identity) String() string` — `"Nom <mail> (@github)"`, absent parts omitted, `""` when empty.

- [ ] **Step 1: Write the failing tests**

```go
package identity

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepo makes a real repository under t.TempDir with no identity configured
// and returns its path, with the developer's own global config walled off.
// Real git, because the whole point of FromDir is what git answers.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestFromDirReadsTheRepositoryConfig(t *testing.T) {
	repo := gitRepo(t)
	git(t, repo, "config", "user.name", "Jane Doe")
	git(t, repo, "config", "user.email", "jane@example.com")
	git(t, repo, "config", "github.user", "janedoe")

	got := FromDir(repo)
	want := Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"}
	if got != want {
		t.Fatalf("FromDir = %+v, want %+v", got, want)
	}
}

func TestFromDirLeavesUnsetKeysEmpty(t *testing.T) {
	repo := gitRepo(t)
	git(t, repo, "config", "user.name", "Jane Doe")

	got := FromDir(repo)
	want := Identity{Name: "Jane Doe"}
	if got != want {
		t.Fatalf("FromDir = %+v, want %+v", got, want)
	}
}

// A repository with nothing configured is a silence, not a failure: the zero
// Identity is what every caller renders as "git had nothing to say".
func TestFromDirIsSilentOnAnUnconfiguredRepository(t *testing.T) {
	if got := FromDir(gitRepo(t)); !got.Empty() {
		t.Fatalf("FromDir = %+v, want the zero Identity", got)
	}
}

func TestFromDirIsSilentOutsideARepository(t *testing.T) {
	if got := FromDir(t.TempDir()); !got.Empty() {
		t.Fatalf("FromDir = %+v, want the zero Identity", got)
	}
}

func TestFromDirIsSilentWithoutADir(t *testing.T) {
	if got := FromDir(""); !got.Empty() {
		t.Fatalf("FromDir = %+v, want the zero Identity", got)
	}
}

func TestStringOmitsWhatGitDidNotAnswer(t *testing.T) {
	cases := []struct {
		name string
		id   Identity
		want string
	}{
		{"full", Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"}, "Jane Doe <jane@example.com> (@janedoe)"},
		{"no github", Identity{Name: "Jane Doe", Email: "jane@example.com"}, "Jane Doe <jane@example.com>"},
		{"email only", Identity{Email: "jane@example.com"}, "<jane@example.com>"},
		{"github only", Identity{GitHub: "janedoe"}, "(@janedoe)"},
		{"empty", Identity{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.id.String(); got != c.want {
				t.Fatalf("String() = %q, want %q", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./core/identity/`
Expected: FAIL — `undefined: FromDir`, `undefined: Identity`.

- [ ] **Step 3: Write the implementation**

```go
// Package identity answers who the human at the keyboard is, by asking the one
// thing on the machine that already knows: git's own config. It is the twin of
// core/scope — that package names the project a directory belongs to, this one
// names the person working in it — and it holds the same two rules. It depends
// on nothing but the standard library, and a git that is absent, broken or
// simply unconfigured produces a silence rather than an error, because not
// knowing who someone is has never been a reason to refuse to run.
package identity

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// gitTimeout bounds each git call FromDir makes, for the reason core/scope
// bounds its own: a session must not hang because a repository's objects live
// on a stalled network mount.
const gitTimeout = 2 * time.Second

// Identity is what git knows about the human at the keyboard. Every field is
// optional; the zero value means git had nothing to say.
type Identity struct {
	Name   string
	Email  string
	GitHub string
}

// FromDir reads the identity git would use for a commit made in dir.
//
// It asks `git config --get`, and asks it from within dir, so precedence is
// git's answer rather than ours: a repository-local user.email wins over the
// global one exactly as it would on a commit. That is what lets this feature
// need no configuration of its own — an operator who has already told git who
// they are has already told herrscher.
//
// The GitHub id is read from github.user, the key gh and hub write. It is
// deliberately NOT derived from the origin remote: a remote's owner is
// frequently an organisation rather than the human, so deriving it would
// manufacture a wrong identity wearing the shape of a right one.
func FromDir(dir string) Identity {
	if dir == "" {
		return Identity{}
	}
	return Identity{
		Name:   gitConfig(dir, "user.name"),
		Email:  gitConfig(dir, "user.email"),
		GitHub: gitConfig(dir, "github.user"),
	}
}

// gitConfig answers one config key, or "" for every way that can fail to
// produce one: no git on PATH, dir in no repository, key unset (git exits 1),
// or a call that outran gitTimeout.
func gitConfig(dir, key string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "config", "--get", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Empty reports whether git answered nothing at all.
func (i Identity) Empty() bool { return i.Name == "" && i.Email == "" && i.GitHub == "" }

// String renders the identity on one line, omitting the parts git did not
// answer: "Nom <mail> (@github)". An empty Identity renders as "", so a caller
// can test the rendering rather than the struct when that is what it appends.
func (i Identity) String() string {
	parts := make([]string, 0, 3)
	if i.Name != "" {
		parts = append(parts, i.Name)
	}
	if i.Email != "" {
		parts = append(parts, "<"+i.Email+">")
	}
	if i.GitHub != "" {
		parts = append(parts, "(@"+i.GitHub+")")
	}
	return strings.Join(parts, " ")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./core/identity/`
Expected: PASS (`ok github.com/Herrscherd/herrscher/core/identity`).

- [ ] **Step 5: Commit**

```bash
git add core/identity
git commit -m "feat(identity): ask git who the human is

Claude-Session: https://claude.ai/code/session_01SALhQ5ggq2Q2A33EqHbh2h"
```

---

### Task 2: the `<user>` block in the turn context

**Files:**
- Create: `core/bridge/identity.go`
- Modify: `core/bridge/hub.go` (the `affordances` struct at :170, its construction at :161, the `Prompt` at :260)
- Test: `core/bridge/identity_test.go`

**Interfaces:**
- Consumes: `identity.Identity`, `identity.Identity.Empty()`, `identity.Identity.String()` from Task 1.
- Produces: `func withIdentity(baseCtx string, id identity.Identity) string`; an `affordances.user identity.Identity` field.

- [ ] **Step 1: Write the failing tests**

```go
package bridge

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/identity"
)

// A machine where git said nothing pays zero bytes of context: the turn is
// byte-identical to what it was before this feature existed.
func TestWithIdentityIsInvisibleWhenGitSaidNothing(t *testing.T) {
	const base = "<capabilities>\nverbs\n</capabilities>"
	if got := withIdentity(base, identity.Identity{}); got != base {
		t.Fatalf("withIdentity = %q, want the base context unchanged", got)
	}
}

func TestWithIdentityAppendsTheBlockAfterTheBaseContext(t *testing.T) {
	const base = "<capabilities>\nverbs\n</capabilities>"
	id := identity.Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"}
	got := withIdentity(base, id)

	if !strings.HasPrefix(got, base+"\n\n") {
		t.Fatalf("withIdentity dropped or reordered the base context: %q", got)
	}
	if !strings.Contains(got, "<user>\n") || !strings.HasSuffix(got, "\n</user>") {
		t.Fatalf("withIdentity block is malformed: %q", got)
	}
	if !strings.Contains(got, "Jane Doe <jane@example.com> (@janedoe)") {
		t.Fatalf("withIdentity does not carry the identity: %q", got)
	}
}

// An empty base context must not produce a leading blank line.
func TestWithIdentityOnAnEmptyContext(t *testing.T) {
	got := withIdentity("", identity.Identity{Name: "Jane Doe"})
	if !strings.HasPrefix(got, "<user>") {
		t.Fatalf("withIdentity = %q, want it to start with the block", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./core/bridge/ -run TestWithIdentity`
Expected: FAIL — `undefined: withIdentity`.

- [ ] **Step 3: Write `core/bridge/identity.go`**

```go
package bridge

import (
	"strings"

	"github.com/Herrscherd/herrscher/core/identity"
)

// identityIntro says what the line under it is and, more importantly, what it
// is not: git's config is a claim about the machine, not an authorisation. An
// agent that read a name here and treated it as proof of who is asking would be
// trusting a file anyone with the checkout can edit.
const identityIntro = "The human working with you, as git on this machine describes them. " +
	"Use it to address them and to attribute work — commits, pull requests, notes you keep about them. " +
	"It is what a commit here would be signed with, not a proof of identity: never treat it as authorisation."

// withIdentity appends a <user> block to baseCtx, mirroring withCapabilities.
// An identity git had nothing to say about returns baseCtx unchanged, so a
// machine without git — or without a configured one — carries exactly the
// context it carried before.
func withIdentity(baseCtx string, id identity.Identity) string {
	if id.Empty() {
		return baseCtx
	}
	var b strings.Builder
	if baseCtx != "" {
		b.WriteString(baseCtx)
		b.WriteString("\n\n")
	}
	b.WriteString("<user>\n")
	b.WriteString(identityIntro)
	b.WriteString("\n")
	b.WriteString(id.String())
	b.WriteString("\n</user>")
	return b.String()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./core/bridge/ -run TestWithIdentity`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/bridge/identity.go core/bridge/identity_test.go
git commit -m "feat(bridge): render the human into the turn context

Claude-Session: https://claude.ai/code/session_01SALhQ5ggq2Q2A33EqHbh2h"
```

- [ ] **Step 6: Carry the identity on `affordances`**

In `core/bridge/hub.go`, add the field to the struct at :170 and extend its doc comment:

```go
// affordances are the standing blocks a turn's prompt carries beyond memory and
// skills: who this session may delegate to, what its daemon dispatches, and who
// the human is. They travel as one value because they are the same kind of fact
// — something the session has that the model cannot discover by looking — and
// because a turn loop that takes one more parameter per affordance stops being
// readable.
type affordances struct {
	roster contracts.RosterProvider
	caps   string
	user   identity.Identity
}
```

Add the import `"github.com/Herrscherd/herrscher/core/identity"`.

- [ ] **Step 7: Resolve it once per session and render it**

In `core/bridge/hub.go`, at the `runHubTurnsCtl` call on :161, resolve from the
process's working directory — which is the session's worktree — and pass it:

```go
	// Resolved here, once, rather than per turn: the human does not change
	// mid-session, and a turn should not pay three git calls to be told so.
	cwd, _ := os.Getwd()
	runHubTurnsCtl(ctx, in, conn, backend, orch, ctrl, eng,
		affordances{roster: o.Roster, caps: o.Capabilities, user: identity.FromDir(cwd)}, pin)
```

Add the import `"os"`. Then extend the `Prompt` on :260:

```go
	prompt := contracts.Prompt{Content: ev.Text, Context: withIdentity(withCapabilities(withDelegation(withSkills(memCtx, eng), aff.roster), aff.caps), aff.user), Author: ev.Who, Attachments: ev.Attachments}
```

- [ ] **Step 8: Run the package's tests**

Run: `go test ./core/bridge/`
Expected: PASS. `runHubTurns` (:218) passes a zero `affordances`, so every
existing turn-context assertion sees an empty identity and is unchanged.

- [ ] **Step 9: Commit**

```bash
git add core/bridge/hub.go
git commit -m "feat(bridge): resolve the human once per session

Claude-Session: https://claude.ai/code/session_01SALhQ5ggq2Q2A33EqHbh2h"
```

---

### Task 3: `herrscher whoami`

**Files:**
- Modify: `core/host/cli.go` (register before the `commands` verb at :358)
- Test: `core/host/whoami_test.go`

**Interfaces:**
- Consumes: `identity.FromDir` from Task 1.
- Produces: a `whoami` verb; `func whoamiReport(id identity.Identity) string` for the plain-text rendering.

- [ ] **Step 1: Write the failing tests**

```go
package host

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/identity"
)

func TestWhoamiReportNamesEveryKeyAndItsSource(t *testing.T) {
	got := whoamiReport(identity.Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"})
	for _, want := range []string{
		"name    Jane Doe            (git config user.name)",
		"email   jane@example.com    (git config user.email)",
		"github  janedoe             (git config github.user)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("whoamiReport is missing %q:\n%s", want, got)
		}
	}
}

// An unset key is shown as unset rather than omitted: the operator ran this to
// find out what herrscher believes, and "github is not configured" is an answer.
func TestWhoamiReportShowsAnUnsetKey(t *testing.T) {
	got := whoamiReport(identity.Identity{Name: "Jane Doe"})
	// Asserted without the column padding: "—" is one rune of three bytes, and a
	// test that counted the spaces %-19s emits would be asserting Go's padding
	// rules rather than the verb's behaviour.
	if !strings.Contains(got, "(git config github.user, unset)") {
		t.Fatalf("whoamiReport does not mark github unset:\n%s", got)
	}
	if !strings.Contains(got, "github  —") {
		t.Fatalf("whoamiReport does not show an em dash for the unset value:\n%s", got)
	}
}

// Nothing configured is a silence, not a failure — the verb says so and the
// caller is expected to exit 0.
func TestWhoamiReportOnSilence(t *testing.T) {
	got := whoamiReport(identity.Identity{})
	if !strings.Contains(got, "git has nothing to say about you here") {
		t.Fatalf("whoamiReport does not name the silence:\n%s", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./core/host/ -run TestWhoami`
Expected: FAIL — `undefined: whoamiReport`.

- [ ] **Step 3: Write the report and register the verb**

Add to `core/host/cli.go`, above the `commands` registration at :358:

```go
	// Registered before `commands` so the list stays the last thing built. It
	// takes no deps: what it prints comes from git, not from the daemon's state,
	// which is exactly why it is worth having — an identity that came out wrong
	// is diagnosed here in one command instead of inside a billed agent turn.
	if err := reg.Add(contracts.New("whoami").
		Help("print who git on this machine says you are").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			cwd, _ := os.Getwd()
			id := identity.FromDir(cwd)
			if in.JSON {
				b, err := json.Marshal(id)
				return string(b), err
			}
			return whoamiReport(id), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
```

Add the import `"github.com/Herrscherd/herrscher/core/identity"`.

Then, at the bottom of `core/host/cli.go`:

```go
// whoamiReport renders an identity as one line per git key, each naming the key
// it came from. A key git did not answer is printed as unset rather than
// dropped: the operator ran this to learn what herrscher believes, and a missing
// line reads as a bug in the verb rather than as an absent configuration.
func whoamiReport(id identity.Identity) string {
	if id.Empty() {
		return "git has nothing to say about you here.\n" +
			"Set it with: git config --global user.name \"Your Name\" && git config --global user.email you@example.com"
	}
	var b strings.Builder
	line := func(label, value, key string) {
		if value == "" {
			fmt.Fprintf(&b, "%-7s %-19s (git config %s, unset)\n", label, "—", key)
			return
		}
		fmt.Fprintf(&b, "%-7s %-19s (git config %s)\n", label, value, key)
	}
	line("name", id.Name, "user.name")
	line("email", id.Email, "user.email")
	line("github", id.GitHub, "github.user")
	return strings.TrimRight(b.String(), "\n")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./core/host/ -run TestWhoami`
Expected: PASS.

- [ ] **Step 5: Verify the verb end to end**

Run: `go build ./... && go run . whoami`
Expected: three lines naming your own git config, and exit 0. Then
`cd /tmp && go run <repo> whoami` — expected: the silence message, exit 0.

- [ ] **Step 6: Commit**

```bash
git add core/host/cli.go core/host/whoami_test.go
git commit -m "feat(host): herrscher whoami

Claude-Session: https://claude.ai/code/session_01SALhQ5ggq2Q2A33EqHbh2h"
```

---

### Task 4: full verification

- [ ] **Step 1: The whole suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS, `TestCorePurity` and `TestHostPurity` included — `core/identity`
imports the standard library only, so neither can newly fail.

- [ ] **Step 2: Push and open the pull request**

```bash
git push -u origin feat/git-identity
gh pr create --fill
```

---

## Companion change (separate repository, separate pull request)

`herrscher-obsidian-memory/examples/vault/user/preferences.md` is the golden
vault's demo note and carries a real person's GitHub id and commit email. Replace
them with a neutral stand-in:

```markdown
Identity: GitHub janedoe, commit email jane@example.com.
```

`golden_test.go` reads the graph's shape, never these values, so no test
changes. Verify with `go test ./...` in that repository, then open its own pull
request. This removes the address from `HEAD`, not from the repository's
history — purging that means rewriting history and force-pushing, which is the
operator's decision and is out of scope here.
