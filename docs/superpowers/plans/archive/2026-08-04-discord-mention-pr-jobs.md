# Discord mention-driven PR jobs — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Discord gateway's channel binding with an owner (user-id) binding, so the bot listens everywhere, reads everyone as context, and starts a turn only when the owner @mentions it or replies to it — carrying screenshots into an isolated worktree whose finishing procedure lives in an editable `SKILL.md`.

**Architecture:** Everything Discord stays in the gateway plugin. The core gains one neutral push seam (`SessionControl.Submit`/`Pick`/`Repos` + `CreateSession.ChannelID`) and nothing else; it never learns what a mention or an owner is. The gateway's websocket starts dispatching `MESSAGE_CREATE` under two non-privileged intents, filters to owner+mention, resolves the channel to a session (asking with a select menu the first time), assembles context over REST, and renders per-conversation instead of into one global channel.

**Tech Stack:** Go 1.25, three repos — `herrscher-contracts` (ports), `herrscher-discord-gateway` (the edge), `herrscher` (core + host). `dctl` is the Discord REST client and is **not** modified.

**Spec:** `docs/superpowers/specs/2026-08-04-discord-mention-pr-jobs-design.md`

## Global Constraints

- **Go 1.25.0** — the `go` directive in all three repos.
- **The core stays agnostic.** No Discord identifier may appear under `core/`. `TestHostPurity`, `TestCorePurity`, `TestCoreNamesNoConcretePlatform` are the enforcement and must pass untouched.
- **`dctl` is not modified.** Any Discord payload field it does not expose (`mentions`, `message_reference`) is unmarshalled by a plugin-local type in the gateway.
- **No privileged intents.** Only `GUILD_MESSAGES` (`1<<9`) and `DIRECT_MESSAGES` (`1<<12`). `MESSAGE_CONTENT` (`1<<15`) must never appear.
- **Select menus only.** No buttons, no modals.
- **Local checkouts are shared with other agents.** `~/dev/herrscher-contracts` is on branch `feat/model-routing` and `~/dev/herrscher-discord-gateway` is on `master`. Never `checkout`/`reset` them — always `git worktree add` a scratch worktree for this work, as the tasks below do.
- **Every repo's tests run with `go test ./...`** and must be green before a commit.
- **Commit style:** conventional commits (`feat(scope):`, `fix(scope):`, `docs(scope):`), matching the existing history.

## File Structure

**`herrscher-contracts`** (worktree at `/tmp/wt-contracts`)
- Modify `session_control.go` — add `Submit`, `Pick`, `Repos` to the `SessionControl` interface; add `RepoRef`; add `ChannelID` to `CreateSession`.
- Modify `session_control_test.go` (or create) — compile-time proof a fake satisfies the grown interface.

**`herrscher`** (this worktree)
- Modify `core/host/turnloop.go` — extract the poll body into `sessionDriver.submit`; add package-level `Submit`.
- Modify `core/host/hub.go` — implement `Submit`, `Pick`, `Repos`.
- Modify `core/internal/manager/commands.go` — declare the `channel_id` param.
- Modify `core/internal/manager/session.go` — the adopt branch in `sessionCreateRun`.
- Create `skills/pr-job/SKILL.md` — the editable finishing procedure.
- Modify `go.mod` / `go.sum` — bumped dependency versions.

**`herrscher-discord-gateway`** (worktree at `/tmp/wt-gateway`)
- Modify `register.go` — `owner` replaces `channel`; new `context_messages` and `playbook` settings; wire the new pieces.
- Modify `ws.go` — intents, `MESSAGE_CREATE` dispatch.
- Create `message.go` — the plugin-local `MESSAGE_CREATE` payload type.
- Create `mention.go` — the owner + mention/reply trigger filter.
- Create `router.go` — channel↔session bindings, the repo menu, the submit path.
- Create `routerstore.go` — the persisted binding store.
- Modify `choice.go` — a second custom_id namespace for repo binding.
- Modify `slash.go` — route component interactions.
- Modify `sink.go` — per-conversation sinks.
- Modify `gateway.go` — `EmitTo`, the routed sink.
- Modify `adapters.go` — `Read` suppression for push-driven channels.
- Modify `README.md` — the documented surface.

---

## Task 1: Grow the `SessionControl` port

**Files:**
- Modify: `/tmp/wt-contracts/session_control.go`
- Test: `/tmp/wt-contracts/session_control_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `SessionControl.Submit(name string, in Inbound) bool`, `SessionControl.Pick(name, value string) bool`, `SessionControl.Repos(ctx context.Context) ([]RepoRef, error)`, `type RepoRef struct { Name, Description string; Local bool }`, `CreateSession.ChannelID string`. Tasks 2, 3, 4, 10, 11, 12 depend on these exact names.

- [ ] **Step 1: Create the scratch worktree**

```bash
git -C /home/shan/dev/herrscher-contracts worktree add -b feat/mention-seam /tmp/wt-contracts master
cd /tmp/wt-contracts && go test ./...
```

Expected: `ok  github.com/Herrscherd/herrscher-contracts` — a green baseline before any edit.

- [ ] **Step 2: Write the failing test**

Append to `/tmp/wt-contracts/session_control_test.go` (create the file with `package contracts` and the imports below if it does not exist):

```go
// stubControl proves a type can satisfy the grown SessionControl interface —
// the compile-time contract every gateway codes against.
type stubControl struct {
	submitted []Inbound
	picked    []string
}

func (s *stubControl) Dispatch(context.Context, []string) (string, error)      { return "", nil }
func (s *stubControl) Create(context.Context, CreateSession) (string, error)   { return "", nil }
func (s *stubControl) Close(context.Context, string, bool) (string, error)     { return "", nil }
func (s *stubControl) Sessions() []SessionInfo                                 { return nil }
func (s *stubControl) Scrollback(string) []ScrollbackLine                      { return nil }
func (s *stubControl) Resume(string) error                                     { return nil }
func (s *stubControl) Interrupt(string) bool                                   { return false }
func (s *stubControl) Submit(_ string, in Inbound) bool                        { s.submitted = append(s.submitted, in); return true }
func (s *stubControl) Pick(_, value string) bool                               { s.picked = append(s.picked, value); return true }
func (s *stubControl) Repos(context.Context) ([]RepoRef, error)                { return nil, nil }

var _ SessionControl = (*stubControl)(nil)

func TestSubmitCarriesAttachments(t *testing.T) {
	var ctrl SessionControl = &stubControl{}
	ok := ctrl.Submit("s1", Inbound{
		Conversation: Conversation{Gateway: "discord", ID: "c1"},
		Author:       "leo",
		Text:         "fix this",
		Attachments:  []Attachment{{Filename: "shot.png", URL: "https://cdn/x.png"}},
	})
	if !ok {
		t.Fatal("Submit reported no live session")
	}
	got := ctrl.(*stubControl).submitted
	if len(got) != 1 || len(got[0].Attachments) != 1 || got[0].Attachments[0].Filename != "shot.png" {
		t.Fatalf("attachments did not survive the seam: %+v", got)
	}
}

func TestCreateSessionAdoptsAChannel(t *testing.T) {
	spec := CreateSession{Name: "ch-123", ChannelID: "123"}
	if spec.ChannelID != "123" {
		t.Fatalf("ChannelID = %q, want 123", spec.ChannelID)
	}
}

func TestRepoRefDistinguishesLocalFromRemote(t *testing.T) {
	local := RepoRef{Name: "herrscher", Description: "workspace", Local: true}
	remote := RepoRef{Name: "Herrscherd/dctl", Description: "github"}
	if !local.Local || remote.Local {
		t.Fatalf("Local flag wrong: %+v %+v", local, remote)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd /tmp/wt-contracts && go test ./... -run 'TestSubmit|TestCreateSessionAdopts|TestRepoRef'`
Expected: FAIL to **compile** — `undefined: RepoRef`, and `*stubControl does not implement SessionControl`.

- [ ] **Step 4: Add the three methods to the interface**

In `/tmp/wt-contracts/session_control.go`, inside `type SessionControl interface`, after the `Interrupt` method:

```go
	// Submit injects one inbound message into the named session's turn queue, as
	// if the session had read it from its own conversation. The host resolves
	// in.Attachments against that session's own download allowlist, so a gateway
	// never fetches remote content itself. Reports false when no live session by
	// that name is driving — the caller's binding is stale.
	Submit(name string, in Inbound) bool
	// Pick answers the named session's pending choice with a menu value. Reports
	// false when no live session by that name is driving (mirror of Submit).
	Pick(name, value string) bool
	// Repos lists the targets a session can be created on: the workspace
	// sub-directories already on disk, plus the repositories the configured forge
	// can clone. A gateway uses it to offer the operator a choice; contracts never
	// learns how either list is obtained.
	Repos(ctx context.Context) ([]RepoRef, error)
```

- [ ] **Step 5: Add `RepoRef`**

In the same file, after the `SessionInfo` type:

```go
// RepoRef is one selectable work target returned by SessionControl.Repos. Local
// reports whether Name is a workspace sub-directory already on disk — create it
// with CreateSession.Project — rather than a remote to clone, which uses
// CreateSession.Clone.
type RepoRef struct {
	Name        string
	Description string
	Local       bool
}
```

- [ ] **Step 6: Add `ChannelID` to `CreateSession`**

In the same file, inside `type CreateSession struct`, after the `Name` field:

```go
	// ChannelID adopts an existing conversation instead of creating one: the new
	// session binds to it and posts there, and no home needs to be configured.
	// Empty keeps the default — create a channel under the configured home. A
	// gateway already talking to the operator in a conversation sets this so the
	// session lands where the conversation already is.
	ChannelID string
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd /tmp/wt-contracts && go test ./...`
Expected: PASS, all packages `ok`.

- [ ] **Step 8: Commit**

```bash
cd /tmp/wt-contracts
git add session_control.go session_control_test.go
git commit -m "feat(control): add Submit/Pick/Repos and CreateSession.ChannelID"
```

---

## Task 2: Core — the inbound push path

**Files:**
- Modify: `core/host/turnloop.go:320-360` (extract `submit` from `poll`), and near `core/host/turnloop.go:271-308` (the registry lookups)
- Test: `core/host/turnloop_test.go`

**Interfaces:**
- Consumes: `contracts.Inbound` (Task 1).
- Produces: `func (d *sessionDriver) submit(ctx context.Context, in contracts.Inbound) bool` and package-level `func Submit(session string, in contracts.Inbound) bool`. Task 4 wires the latter into the hub.

**Setup:** all remaining core work happens in this worktree (`/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/discord-ai-pr-automation`) on the current branch `discord-ai-pr-automation`. To compile against the un-tagged contracts, add a temporary replace **now** and remove it in Task 8:

```bash
go mod edit -replace github.com/Herrscherd/herrscher-contracts=/tmp/wt-contracts
go build ./...
```

- [ ] **Step 1: Write the failing test**

Append to `core/host/turnloop_test.go`:

```go
func TestSubmitEnqueuesAnInputFrame(t *testing.T) {
	d := newSessionDriver("s1", nil, nil, nil)
	registerDriver("s1", d)
	defer unregisterDriver("s1", d)

	if !Submit("s1", contracts.Inbound{Author: "leo", Text: "fix the login bug"}) {
		t.Fatal("Submit reported no live session for a registered driver")
	}
	select {
	case ev := <-d.queue:
		if ev.T != "input" || ev.Who != "leo" || ev.Text != "fix the login bug" {
			t.Fatalf("queued %+v, want an input frame from leo", ev)
		}
	default:
		t.Fatal("Submit queued nothing")
	}
}

func TestSubmitOnUnknownSessionReportsFalse(t *testing.T) {
	if Submit("nobody-home", contracts.Inbound{Text: "hi"}) {
		t.Fatal("Submit reported success for an unregistered session")
	}
}

func TestSubmitJournalsTheAuthor(t *testing.T) {
	dir := t.TempDir()
	d := newSessionDriver("s2", nil, nil, nil)
	d.participants = filepath.Join(dir, "participants")
	registerDriver("s2", d)
	defer unregisterDriver("s2", d)

	Submit("s2", contracts.Inbound{Author: "leo", AuthorID: "42", Text: "hi"})
	<-d.queue
	b, err := os.ReadFile(d.participants)
	if err != nil || !strings.Contains(string(b), "42") {
		t.Fatalf("author id not journalled: %q err=%v", b, err)
	}
}
```

Note the test uses `Inbound.AuthorID`. `contracts.Inbound` has `Author` (display name) but no id, and `journal` needs the **id** (`turnloop.go:120`). Add the field in `/tmp/wt-contracts/gateway.go` inside `type Inbound struct`, then re-run Task 1 Step 7-8 with an amended commit:

```go
	// AuthorID is the platform's stable id for the author, used for participant
	// journalling. Author is the display name shown to the agent.
	AuthorID string
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./core/host/ -run TestSubmit -v`
Expected: FAIL to compile — `undefined: Submit`.

- [ ] **Step 3: Extract the shared body out of `poll`**

In `core/host/turnloop.go`, replace the body of the `for _, m := range msgs` loop (currently lines 340-351) so both paths share one implementation:

```go
			for _, m := range msgs {
				if m.AuthorBot {
					continue
				}
				last = m.ID
				if !d.submit(ctx, contracts.Inbound{
					Author:      m.AuthorName,
					AuthorID:    m.AuthorID,
					Text:        m.Content,
					Attachments: m.Attachments,
					MessageID:   contracts.MessageID(m.ID),
				}) {
					return
				}
			}
```

and add, immediately after `poll`:

```go
// submit records the author, resolves the message's attachments host-side, and
// enqueues one input frame. It is the single body behind both inbound paths: the
// poller (a gateway the core pulls from) and SessionControl.Submit (a gateway
// that pushes). It reports false only when ctx was cancelled while enqueueing,
// which is the poller's signal to stop.
func (d *sessionDriver) submit(ctx context.Context, in contracts.Inbound) bool {
	d.journal(in.AuthorID)
	atts := d.resolveAttachments(ctx, contracts.Message{
		ID:          string(in.MessageID),
		Content:     in.Text,
		AuthorID:    in.AuthorID,
		AuthorName:  in.Author,
		Attachments: in.Attachments,
	})
	select {
	case d.queue <- contracts.Event{T: "input", Who: in.Author, Text: in.Text, Attachments: atts}:
		return true
	case <-ctx.Done():
		return false
	}
}
```

- [ ] **Step 4: Add the package-level lookup**

In `core/host/turnloop.go`, after the existing `Seed` function (line 299-308), mirroring its shape exactly:

```go
// Submit injects one inbound message into the named session's turn queue,
// returning false when no live session by that name is driving (mirror of Pick).
// It is the push counterpart of the driver's own poll loop: a gateway that
// receives messages by push calls this instead of being polled.
func Submit(session string, in contracts.Inbound) bool {
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return false
	}
	return d.submit(context.Background(), in)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./core/host/ -run TestSubmit -v`
Expected: PASS — three tests.

- [ ] **Step 6: Run the full core suite, purity included**

Run: `go test ./core/... ./...`
Expected: PASS everywhere, including `TestCorePurity` and `TestCoreNamesNoConcretePlatform`.

- [ ] **Step 7: Commit**

```bash
git add core/host/turnloop.go core/host/turnloop_test.go
git commit -m "feat(host): push inbound path — sessionDriver.submit + host.Submit"
```

---

## Task 3: Core — adopt an existing conversation

**Files:**
- Modify: `core/internal/manager/commands.go:10-31`
- Modify: `core/internal/manager/session.go:355-384` and `:527-545`
- Test: `core/internal/manager/handler_test.go`

**Interfaces:**
- Consumes: `CreateSession.ChannelID` (Task 1).
- Produces: the `session create --channel_id <id>` flag. Task 4 maps the spec field onto it.

- [ ] **Step 1: Write the failing test**

Append to `core/internal/manager/handler_test.go`:

```go
func TestCreateAdoptsAnExistingChannel(t *testing.T) {
	h, d, _ := newTestHandler(t)
	h.st.Home = state.HomeRef{} // no home configured at all
	out, err := h.sessionCreateRun(context.Background(), contracts.Input{Args: map[string]string{
		"name": "ch-123", "channel_id": "123", "shared": "true",
	}})
	if err != nil {
		t.Fatalf("create with channel_id failed: %v", err)
	}
	if len(d.created) != 0 {
		t.Fatalf("adopting a channel still created one: %+v", d.created)
	}
	sess, ok := h.st.FindSession("ch-123")
	if !ok || sess.ChannelID != "123" || sess.Type != "text" {
		t.Fatalf("session not bound to the adopted channel: %+v ok=%v", sess, ok)
	}
	if out == "" {
		t.Fatal("create returned no banner")
	}
}

func TestCreateWithoutChannelIDStillNeedsAHome(t *testing.T) {
	h, _, _ := newTestHandler(t)
	h.st.Home = state.HomeRef{}
	_, err := h.sessionCreateRun(context.Background(), contracts.Input{Args: map[string]string{
		"name": "plain", "shared": "true",
	}})
	if err == nil || !strings.Contains(err.Error(), "no home set") {
		t.Fatalf("err = %v, want the existing no-home error", err)
	}
}
```

`newTestHandler` and the fake admin's `created` slice already exist in this file — reuse them rather than adding new fakes. If the existing helper is named differently, use the constructor the neighbouring `TestSessionCreate*` tests use and record `CreateUnder`/`ForumPost` calls the same way they do.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./core/internal/manager/ -run TestCreateAdopts -v`
Expected: FAIL — `no home set — run `set home` first`.

- [ ] **Step 3: Declare the flag**

In `core/internal/manager/commands.go`, in the `session create` chain after the `name` param:

```go
				Param("channel_id", "adopt this existing conversation instead of creating a channel (no home needed)", false).
```

- [ ] **Step 4: Skip the home requirement when adopting**

In `core/internal/manager/session.go`, replace the home guard at line 382-384:

```go
	adopted, _ := in.Lookup("channel_id")
	// Adopting an existing conversation needs no home: the caller already knows
	// where the session must live, so there is nothing to create under.
	if home.ID == "" && adopted == "" {
		return "", fmt.Errorf("no home set — run `set home` first")
	}
```

- [ ] **Step 5: Bypass channel creation when adopting**

In the same file, replace the `switch home.Type` head (line 527-529) so the adopt case is decided before it:

```go
	var sess state.Session
	switch {
	case adopted != "":
		// The conversation already exists — bind to it rather than creating a
		// channel. This is what lets a gateway start a session in the channel the
		// operator is already talking in.
		sess = state.Session{Name: name, ChannelID: adopted, Type: "text", Cmd: cmd, Backend: backend, Vendor: vendor, Worktree: worktree, Dir: runDir, Project: project, Agent: agentName, Parent: parent, Gateways: gateways, Extractor: extractor, Journal: journal, ConsolidateEvery: consolidateEvery, CostCap: costCap, TokenCap: tokenCap, CohortCostCap: cohortCostCap, CohortTokenCap: cohortTokenCap}
	case home.Type == "category" || home.Type == "terminal":
```

then change the `case "forum":` line to `case home.Type == "forum":` and the `default:` arm's error to keep naming the home type. The two existing `sess = state.Session{...}` bodies are unchanged.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./core/internal/manager/ -v`
Expected: PASS — the two new tests and every pre-existing `TestSessionCreate*`.

- [ ] **Step 7: Commit**

```bash
git add core/internal/manager/commands.go core/internal/manager/session.go core/internal/manager/handler_test.go
git commit -m "feat(manager): session create --channel_id adopts an existing conversation"
```

---

## Task 4: Core — implement the seam on the hub

**Files:**
- Modify: `core/host/hub.go:187-233` (`create`) and near `:321-327`
- Test: `core/host/hub_test.go`

**Interfaces:**
- Consumes: `host.Submit` (Task 2), the `channel_id` flag (Task 3), `contracts.RepoRef` (Task 1).
- Produces: `hub` satisfying the grown `contracts.SessionControl`. Tasks 10-12 call it through the bound seam.

- [ ] **Step 1: Write the failing test**

Append to `core/host/hub_test.go`:

```go
func TestHubStillSatisfiesSessionControl(t *testing.T) {
	// The compile-time assertion at the bottom of hub.go is the real guard; this
	// test exists so a reviewer sees the intent named.
	var _ contracts.SessionControl = (*hub)(nil)
}

func TestReposListsLocalProjectsAndRemotes(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "myproj", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "notrepo"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := workspaceRepos(ws)
	if len(got) != 1 || got[0].Name != "myproj" || !got[0].Local {
		t.Fatalf("workspaceRepos(%q) = %+v, want one local myproj", ws, got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./core/host/ -run 'TestHubStill|TestReposLists' -v`
Expected: FAIL to compile — `undefined: workspaceRepos`.

- [ ] **Step 3: Map `ChannelID` through `create`**

In `core/host/hub.go`, inside `func (h *hub) create`, next to the other `setStr` calls:

```go
	setStr("channel_id", spec.ChannelID)
```

- [ ] **Step 4: Implement `Submit` and `Pick`**

In `core/host/hub.go`, next to `Interrupt` (line 321-325):

```go
// Submit injects one inbound message into the named session's turn queue,
// delegating to the live driver via the session registry. It implements
// contracts.SessionControl.
func (h *hub) Submit(name string, in contracts.Inbound) bool {
	return Submit(name, in)
}

// Pick answers the named session's pending choice, delegating to the live driver
// via the session registry. It implements contracts.SessionControl.
func (h *hub) Pick(name, value string) bool {
	return Pick(name, value)
}
```

- [ ] **Step 5: Implement `Repos`**

In `core/host/hub.go`, after `Pick`:

```go
// Repos lists the targets a session can be created on: the workspace
// sub-directories already checked out, plus whatever the configured forge can
// clone. Forge failures (no gh/glab, no auth) are not fatal — the local list is
// still useful on its own. It implements contracts.SessionControl.
func (h *hub) Repos(ctx context.Context) ([]contracts.RepoRef, error) {
	out := workspaceRepos(h.workspace)
	repos, err := forge.New().List(ctx)
	if err != nil {
		return out, nil
	}
	for _, r := range repos {
		out = append(out, contracts.RepoRef{Name: r.Name, Description: r.Description})
	}
	return out, nil
}

// workspaceRepos lists the immediate sub-directories of the workspace that are
// git checkouts, as local RepoRefs. A missing or unreadable workspace yields
// none rather than an error: the forge list alone is still a usable answer.
func workspaceRepos(workspace string) []contracts.RepoRef {
	if workspace == "" {
		return nil
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return nil
	}
	var out []contracts.RepoRef
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(workspace, e.Name(), ".git")); err != nil {
			continue
		}
		out = append(out, contracts.RepoRef{Name: e.Name(), Description: "workspace checkout", Local: true})
	}
	return out
}
```

Add `"github.com/Herrscherd/herrscher/core/internal/forge"`, `"os"` and `"path/filepath"` to the file's imports. Resolve `h.workspace` against how the hub already knows its workspace root — if the `hub` struct has no such field, add one and populate it in the hub's constructor from the same value `buildRegistry` passes the manager (search `workspace` in `core/host/serve.go` and `core/internal/manager/handler.go` for the existing accessor and reuse it rather than re-deriving the path).

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./core/... ./...`
Expected: PASS everywhere. Purity tests included — `forge` is an internal core package, not a platform.

- [ ] **Step 7: Commit**

```bash
git add core/host/hub.go core/host/hub_test.go
git commit -m "feat(host): implement Submit/Pick/Repos on the hub seam"
```

---

## Task 5: Gateway — render per conversation

**Files:**
- Modify: `/tmp/wt-gateway/sink.go:28-134`
- Modify: `/tmp/wt-gateway/gateway.go:19-65`
- Modify: `/tmp/wt-gateway/adapters.go:205-227`
- Test: `/tmp/wt-gateway/sink_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `type sinks struct` with `func (s *sinks) for(convID string) *sink` and `Gateway.EmitTo(conv contracts.Conversation, e contracts.Event)`. Tasks 10-11 call `noteUser` through it.

This task is independently valuable and changes no behaviour for a single-channel deployment: it un-pins rendering from `DefaultChannel()`.

- [ ] **Step 1: Create the scratch worktree**

```bash
git -C /home/shan/dev/herrscher-discord-gateway worktree add -b feat/mention-flow /tmp/wt-gateway master
cd /tmp/wt-gateway
go mod edit -replace github.com/Herrscherd/herrscher-contracts=/tmp/wt-contracts
go test ./...
```

Expected: `ok  github.com/Herrscherd/herrscher-discord-gateway` — green baseline.

- [ ] **Step 2: Write the failing test**

Append to `/tmp/wt-gateway/sink_test.go`:

```go
func TestSinksRenderPerConversationIndependently(t *testing.T) {
	rc := &fakeRenderClient{} // the existing fake in this file
	set := newSinks(context.Background(), rc, "full")

	set.at("chanA").noteUser("mA")
	set.at("chanB").noteUser("mB")
	set.at("chanA").handle(contracts.Event{T: "human"})
	set.at("chanB").handle(contracts.Event{T: "human"})
	set.at("chanA").handle(contracts.Event{T: "reply", Text: "answer A", Done: true})

	if got := rc.postsTo("chanB"); len(got) != 0 {
		t.Fatalf("conversation B received %v; A's reply leaked across channels", got)
	}
	if got := rc.postsTo("chanA"); len(got) != 1 || got[0] != "answer A" {
		t.Fatalf("conversation A posts = %v, want [answer A]", got)
	}
	if set.at("chanA") != set.at("chanA") {
		t.Fatal("at() returned a fresh sink for a known conversation — per-turn state would be lost")
	}
}
```

Extend the file's existing fake render client so it records the channel of every `Post`/`React`/`Unreact`/`UpsertStatusMessage` call and exposes `postsTo(ch string) []string`. Its `DefaultChannel()` must return `""` — proving nothing falls back to a global channel.

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd /tmp/wt-gateway && go test ./... -run TestSinksRender -v`
Expected: FAIL to compile — `undefined: newSinks`.

- [ ] **Step 4: Bind each sink to its own channel**

In `/tmp/wt-gateway/sink.go`, give `sink` its channel and stop asking the client for a default. Change the struct and constructor:

```go
type sink struct {
	ctx  context.Context
	rc   renderClient
	ch   string // the conversation this sink renders into
	level string

	mu       sync.Mutex
	pv       *progressView
	lastUser string
	acked    string
}

func newSink(ctx context.Context, rc renderClient, ch, level string) *sink {
	if level == "" {
		level = "full"
	}
	return &sink{ctx: ctx, rc: rc, ch: ch, level: level}
}
```

In `handle`, replace `ch := s.rc.DefaultChannel()` with `ch := s.ch`. Nothing else in `handle` or `clearAck` changes.

- [ ] **Step 5: Add the per-conversation registry**

Append to `/tmp/wt-gateway/sink.go`:

```go
// sinks is the set of live per-conversation renderers. The gateway is no longer
// mono-channel: one sink per conversation, each with its own in-flight turn
// state, created on first use and kept for the process's life (a conversation
// the bot has spoken in is cheap to keep, and its ack/progress ids must survive
// between turns).
type sinks struct {
	ctx   context.Context
	rc    renderClient
	level string

	mu sync.Mutex
	m  map[string]*sink
}

func newSinks(ctx context.Context, rc renderClient, level string) *sinks {
	return &sinks{ctx: ctx, rc: rc, level: level, m: map[string]*sink{}}
}

// at returns the renderer for one conversation, creating it on first use.
func (s *sinks) at(convID string) *sink {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v := s.m[convID]; v != nil {
		return v
	}
	v := newSink(s.ctx, s.rc, convID, s.level)
	s.m[convID] = v
	return v
}
```

- [ ] **Step 6: Route events by conversation**

In `/tmp/wt-gateway/gateway.go`, change the `Gateway` struct's `sink *sink` field to `sinks *sinks`, add the routed-sink assertion, and replace `Emit`:

```go
var (
	_ contracts.Gateway                = (*Gateway)(nil)
	_ contracts.SessionControlReceiver = (*Gateway)(nil)
	_ contracts.EventSink              = (*Gateway)(nil)
	_ contracts.RoutedEventSink        = (*Gateway)(nil)
)

// EmitTo renders one live turn event into the conversation the host routed it
// to. It satisfies contracts.RoutedEventSink, which the host prefers over the
// flat EventSink — so each session renders into its own channel instead of one
// global default.
func (g *Gateway) EmitTo(conv contracts.Conversation, e contracts.Event) {
	if g.sinks == nil || conv.ID == "" {
		return
	}
	g.sinks.at(conv.ID).handle(e)
}

// Emit is the unrouted fallback for a host that does not route events. Without a
// conversation there is nothing to render into, so it drops rather than guessing
// a channel.
func (g *Gateway) Emit(contracts.Event) {}
```

- [ ] **Step 7: Drop `DefaultChannel` from the render surface**

In `/tmp/wt-gateway/sink.go`, remove `DefaultChannel() string` from the `renderClient` interface. In `/tmp/wt-gateway/adapters.go`, delete `renderAdapter.DefaultChannel`. `Platform.DefaultChannel` stays — it still satisfies `contracts.ChannelReader`.

- [ ] **Step 8: Update the wiring**

In `/tmp/wt-gateway/register.go`, replace the sink construction:

```go
	// One shared set of per-conversation renderers: the gateway feeds it routed
	// events (EmitTo) and the platform records the last user message id (Read) for
	// the ACK of the conversation that message belongs to.
	s := newSinks(ctx, renderAdapter{plat}, "full")
	gw.sinks = s
	plat.sinks = s
```

In `/tmp/wt-gateway/adapters.go`, change `Platform.sink *sink` to `sinks *sinks` and the `Read` call site to `p.sinks.at(r.msg.ChannelID).noteUser(r.id)` — the ack now lands in the channel the message came from, which is exactly what the multi-channel flow needs.

- [ ] **Step 9: Run the tests to verify they pass**

Run: `cd /tmp/wt-gateway && go test ./... -v`
Expected: PASS. Pre-existing sink tests need their `newSink` calls updated with the new `ch` argument — do that, do not delete them.

- [ ] **Step 10: Commit**

```bash
cd /tmp/wt-gateway
git add sink.go sink_test.go gateway.go adapters.go register.go
git commit -m "feat(gateway): render per conversation via RoutedEventSink"
```

---

## Task 6: Gateway — intents and `MESSAGE_CREATE`

**Files:**
- Modify: `/tmp/wt-gateway/ws.go:19-63`, `:155-189`, `:191-222`
- Create: `/tmp/wt-gateway/message.go`
- Test: `/tmp/wt-gateway/ws_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type messageCreate struct` (fields `ID`, `ChannelID`, `GuildID`, `Content`, `Author`, `Mentions`, `Attachments`, `Referenced`) and `newWS(token string, onInteraction func(context.Context, dctl.Interaction), onMessage func(context.Context, messageCreate)) *ws`. Tasks 7 and 10 consume `messageCreate`.

- [ ] **Step 1: Write the failing test**

Append to `/tmp/wt-gateway/ws_test.go`:

```go
func TestIdentifyRequestsOnlyNonPrivilegedMessageIntents(t *testing.T) {
	w := newWS("tok", nil, nil)
	var got identifyMsg
	b, err := json.Marshal(identifyMsg{Op: opIdentify, D: identifyData{Token: w.token, Intents: wsIntents}})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.D.Intents != (1<<9)|(1<<12) {
		t.Fatalf("intents = %d, want GUILD_MESSAGES|DIRECT_MESSAGES", got.D.Intents)
	}
	if got.D.Intents&(1<<15) != 0 {
		t.Fatal("MESSAGE_CONTENT is privileged and must never be requested")
	}
}

func TestDispatchRoutesMessageCreate(t *testing.T) {
	seen := make(chan messageCreate, 1)
	w := newWS("tok", nil, func(_ context.Context, m messageCreate) { seen <- m })
	w.dispatch(context.Background(), "MESSAGE_CREATE", json.RawMessage(`{
		"id":"m1","channel_id":"c1","content":"<@app> fix it",
		"author":{"id":"owner","username":"leo"},
		"mentions":[{"id":"app"}],
		"attachments":[{"filename":"shot.png","url":"https://cdn/x.png","content_type":"image/png","size":10}]
	}`))
	select {
	case m := <-seen:
		if m.ID != "m1" || m.ChannelID != "c1" || m.Author.ID != "owner" {
			t.Fatalf("decoded %+v", m)
		}
		if len(m.Mentions) != 1 || m.Mentions[0].ID != "app" {
			t.Fatalf("mentions = %+v", m.Mentions)
		}
		if len(m.Attachments) != 1 || m.Attachments[0].Filename != "shot.png" {
			t.Fatalf("attachments = %+v", m.Attachments)
		}
	case <-time.After(time.Second):
		t.Fatal("MESSAGE_CREATE never reached the message handler")
	}
}

func TestDispatchIgnoresUnknownEvents(t *testing.T) {
	w := newWS("tok", nil, func(context.Context, messageCreate) {
		t.Fatal("TYPING_START must not reach the message handler")
	})
	w.dispatch(context.Background(), "TYPING_START", json.RawMessage(`{}`))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /tmp/wt-gateway && go test ./... -run 'TestIdentify|TestDispatch' -v`
Expected: FAIL to compile — `undefined: wsIntents`, `undefined: messageCreate`, and `newWS` takes 2 arguments.

- [ ] **Step 3: Create the payload type**

Create `/tmp/wt-gateway/message.go`:

```go
package discord

import "github.com/Herrscherd/dctl"

// messageCreate is the subset of a Discord MESSAGE_CREATE dispatch the gateway
// reads. dctl.Message covers the REST shape but carries neither `mentions` nor
// `referenced_message`, and both are load-bearing here: they are how the trigger
// filter tells "the owner is talking to the bot" from ordinary channel chatter.
// Declaring the shape here keeps dctl untouched.
type messageCreate struct {
	ID          string            `json:"id"`
	ChannelID   string            `json:"channel_id"`
	GuildID     string            `json:"guild_id"`
	Content     string            `json:"content"`
	Author      dctl.Author       `json:"author"`
	Mentions    []dctl.Author     `json:"mentions"`
	Attachments []dctl.Attachment `json:"attachments"`
	// Referenced is the message this one replies to, present only when Discord
	// resolves it. A reply to a message the bot wrote is a trigger; nil is not.
	Referenced *struct {
		Author dctl.Author `json:"author"`
	} `json:"referenced_message"`
}
```

- [ ] **Step 4: Add the intents constant**

In `/tmp/wt-gateway/ws.go`, in the opcode const block:

```go
	// wsIntents is GUILD_MESSAGES | DIRECT_MESSAGES. Both are non-privileged.
	// MESSAGE_CONTENT (1<<15) is deliberately absent: without it Discord still
	// populates content and attachments for messages that mention the app (and
	// for DMs), which is exactly the set the trigger filter keeps — and channel
	// context is read over REST, which the intent does not gate.
	wsIntents = (1 << 9) | (1 << 12)
```

- [ ] **Step 5: Take a message handler and dispatch to it**

In `/tmp/wt-gateway/ws.go`, add the field and parameter:

```go
type ws struct {
	token     string
	handle    func(context.Context, dctl.Interaction)
	onMessage func(context.Context, messageCreate)
	// … existing fields unchanged
}

func newWS(token string, handle func(context.Context, dctl.Interaction), onMessage func(context.Context, messageCreate)) *ws {
	return &ws{token: token, handle: handle, onMessage: onMessage, sem: make(chan struct{}, maxInFlight)}
}
```

Replace the `case opDispatch:` arm in `session` with:

```go
		case opDispatch:
			w.dispatch(ctx, p.T, p.D)
```

Rename `onDispatch` to `dispatchInteraction` (body unchanged) and add:

```go
// dispatch fans one gateway dispatch out to the right decoder. Unknown event
// types are dropped: the two intents we identify with deliver a handful of
// events we do not act on, and they must cost nothing.
func (w *ws) dispatch(ctx context.Context, t string, d json.RawMessage) {
	switch t {
	case "INTERACTION_CREATE":
		if w.handle != nil {
			w.dispatchInteraction(ctx, d)
		}
	case "MESSAGE_CREATE":
		if w.onMessage == nil {
			return
		}
		var m messageCreate
		if err := json.Unmarshal(d, &m); err != nil {
			fmt.Fprintf(os.Stderr, "discord gateway: bad message: %v\n", err)
			return
		}
		w.bounded(ctx, func() { w.onMessage(ctx, m) })
	}
}

// bounded runs fn on its own goroutine under the in-flight semaphore, so a burst
// of dispatches cannot spawn arbitrarily many handlers.
func (w *ws) bounded(ctx context.Context, fn func()) {
	select {
	case w.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	go func() {
		defer func() { <-w.sem }()
		fn()
	}()
}
```

Refactor `dispatchInteraction` to call `w.bounded` too, so both paths share the one bound.

- [ ] **Step 6: Use the constant in `identify`**

In `/tmp/wt-gateway/ws.go`, change `Intents: 0` to `Intents: wsIntents`.

- [ ] **Step 7: Update the existing call site**

In `/tmp/wt-gateway/slash.go`, `start()` becomes `newWS(s.token, s.onInteraction, s.onMessage).run(s.ctx)`. Add a temporary stub so this task compiles on its own; Task 7 fills it in:

```go
// onMessage receives every MESSAGE_CREATE the intents deliver. Task 7 replaces
// this stub with the trigger filter.
func (s *slash) onMessage(context.Context, messageCreate) {}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `cd /tmp/wt-gateway && go test ./... -v`
Expected: PASS, including the pre-existing `ws_permanent_test.go`.

- [ ] **Step 9: Commit**

```bash
cd /tmp/wt-gateway
git add ws.go ws_test.go message.go slash.go
git commit -m "feat(gateway): dispatch MESSAGE_CREATE under non-privileged intents"
```

---

## Task 7: Gateway — the owner + mention trigger filter

**Files:**
- Create: `/tmp/wt-gateway/mention.go`
- Test: `/tmp/wt-gateway/mention_test.go`

**Interfaces:**
- Consumes: `messageCreate` (Task 6).
- Produces: `type trigger struct { owner, appID string }` and `func (t trigger) fires(m messageCreate) bool`. Task 10 gates the router on it.

- [ ] **Step 1: Write the failing test**

Create `/tmp/wt-gateway/mention_test.go`:

```go
package discord

import "testing"

func TestTriggerFires(t *testing.T) {
	tr := trigger{owner: "owner1", appID: "app1"}
	owner := dctl.Author{ID: "owner1", Username: "leo"}
	someoneElse := dctl.Author{ID: "other", Username: "sam"}
	botMsg := &struct {
		Author dctl.Author `json:"author"`
	}{Author: dctl.Author{ID: "app1", Bot: true}}

	cases := []struct {
		name string
		msg  messageCreate
		want bool
	}{
		{"owner mentions the bot", messageCreate{Author: owner, Mentions: []dctl.Author{{ID: "app1"}}}, true},
		{"owner replies to the bot", messageCreate{Author: owner, Referenced: botMsg}, true},
		{"owner says nothing to the bot", messageCreate{Author: owner, Content: "unrelated chatter"}, false},
		{"someone else mentions the bot", messageCreate{Author: someoneElse, Mentions: []dctl.Author{{ID: "app1"}}}, false},
		{"owner mentions someone else", messageCreate{Author: owner, Mentions: []dctl.Author{{ID: "other"}}}, false},
		{"the bot's own message", messageCreate{Author: dctl.Author{ID: "app1", Bot: true}, Mentions: []dctl.Author{{ID: "app1"}}}, false},
		{"a bot impersonating the owner id", messageCreate{Author: dctl.Author{ID: "owner1", Bot: true}, Mentions: []dctl.Author{{ID: "app1"}}}, false},
		{"owner replies to a human", messageCreate{Author: owner, Referenced: &struct {
			Author dctl.Author `json:"author"`
		}{Author: someoneElse}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tr.fires(c.msg); got != c.want {
				t.Fatalf("fires() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTriggerNeverFiresWithoutAnOwner(t *testing.T) {
	tr := trigger{appID: "app1"}
	m := messageCreate{Author: dctl.Author{ID: "anyone"}, Mentions: []dctl.Author{{ID: "app1"}}}
	if tr.fires(m) {
		t.Fatal("an unconfigured owner must obey nobody, not everybody")
	}
}
```

Add `"github.com/Herrscherd/dctl"` to the test file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /tmp/wt-gateway && go test ./... -run TestTrigger -v`
Expected: FAIL to compile — `undefined: trigger`.

- [ ] **Step 3: Write the filter**

Create `/tmp/wt-gateway/mention.go`:

```go
package discord

// trigger decides which incoming messages the bot acts on. The rule is
// deliberately narrow and lives entirely here: a turn starts only when the
// configured owner addresses the bot, by @mention or by replying to one of its
// messages. Every other message — including everybody else's — is still read as
// context later, but never makes the bot act.
type trigger struct {
	owner string // Discord user id of the operator the bot obeys
	appID string // the bot's own application (user) id
}

// fires reports whether m should open a turn. An unset owner fires for nobody:
// a misconfigured bot must be inert, never obedient to everyone.
func (t trigger) fires(m messageCreate) bool {
	if t.owner == "" || m.Author.ID != t.owner || m.Author.Bot {
		return false
	}
	for _, u := range m.Mentions {
		if u.ID == t.appID {
			return true
		}
	}
	return m.Referenced != nil && m.Referenced.Author.ID == t.appID
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /tmp/wt-gateway && go test ./... -run TestTrigger -v`
Expected: PASS — nine subtests.

- [ ] **Step 5: Commit**

```bash
cd /tmp/wt-gateway
git add mention.go mention_test.go
git commit -m "feat(gateway): owner + mention/reply trigger filter"
```

---

## Task 8: Gateway — the binding store

**Files:**
- Create: `/tmp/wt-gateway/routerstore.go`
- Test: `/tmp/wt-gateway/routerstore_test.go`

**Interfaces:**
- Consumes: `writeAtomic` from `allow.go:58`.
- Produces: `func newBindStore(path string) *bindStore`, `(*bindStore).Session(channel string) string`, `(*bindStore).Bind(channel, session string) error`, `(*bindStore).Unbind(channel string) error`. Task 9 uses all four.

- [ ] **Step 1: Write the failing test**

Create `/tmp/wt-gateway/routerstore_test.go`:

```go
package discord

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBindStoreRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.json")
	s := newBindStore(path)
	if got := s.Session("c1"); got != "" {
		t.Fatalf("empty store returned %q", got)
	}
	if err := s.Bind("c1", "ch-c1"); err != nil {
		t.Fatal(err)
	}
	if got := newBindStore(path).Session("c1"); got != "ch-c1" {
		t.Fatalf("reloaded store returned %q, want ch-c1", got)
	}
	if err := s.Unbind("c1"); err != nil {
		t.Fatal(err)
	}
	if got := newBindStore(path).Session("c1"); got != "" {
		t.Fatalf("unbound channel still resolves to %q", got)
	}
}

func TestBindStoreIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.json")
	if err := newBindStore(path).Bind("c1", "s1"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestCorruptBindStoreStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := newBindStore(path).Session("c1"); got != "" {
		t.Fatalf("corrupt store resolved %q — it must ask again, never guess a repo", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /tmp/wt-gateway && go test ./... -run TestBindStore -v`
Expected: FAIL to compile — `undefined: newBindStore`.

- [ ] **Step 3: Write the store**

Create `/tmp/wt-gateway/routerstore.go`:

```go
package discord

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// bindStore remembers which session drives which channel, persisted as JSON
// beside the allow store. It lives entirely in the plugin: the core knows
// sessions, never channels-to-sessions. Losing it is safe — an unbound channel
// simply asks the operator again.
type bindStore struct {
	mu   sync.Mutex
	path string

	Channels map[string]string `json:"channels"` // channel id -> session name
}

func newBindStore(path string) *bindStore {
	s := &bindStore{path: path, Channels: map[string]string{}}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, s); err != nil {
			// A corrupt store must not silently resolve to a wrong session: report
			// it and start empty, which only costs one extra question.
			fmt.Fprintf(os.Stderr, "discord gateway: bind store %s is corrupt, ignoring: %v\n", path, err)
			s.Channels = map[string]string{}
		}
		if s.Channels == nil {
			s.Channels = map[string]string{}
		}
	}
	return s
}

// persist mirrors allowStore.persist: mutate and serialize under the lock, write
// outside it so a slow disk cannot block the dispatch hot path.
func (s *bindStore) persist(mutate func()) error {
	s.mu.Lock()
	mutate()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return writeAtomic(s.path, data)
}

// Session returns the session bound to a channel, or "" when none is.
func (s *bindStore) Session(channel string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Channels[channel]
}

func (s *bindStore) Bind(channel, session string) error {
	return s.persist(func() { s.Channels[channel] = session })
}

func (s *bindStore) Unbind(channel string) error {
	return s.persist(func() { delete(s.Channels, channel) })
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /tmp/wt-gateway && go test ./... -run TestBindStore -v && cd /tmp/wt-gateway && go test ./... -run TestCorruptBind -v`
Expected: PASS — three tests.

- [ ] **Step 5: Commit**

```bash
cd /tmp/wt-gateway
git add routerstore.go routerstore_test.go
git commit -m "feat(gateway): persisted channel to session binding store"
```

---

## Task 9: Gateway — the router

**Files:**
- Create: `/tmp/wt-gateway/router.go`
- Modify: `/tmp/wt-gateway/choice.go`
- Test: `/tmp/wt-gateway/router_test.go`, `/tmp/wt-gateway/choice_test.go`

**Interfaces:**
- Consumes: `trigger.fires` (Task 7), `bindStore` (Task 8), `sinks.at` (Task 5), `contracts.SessionControl` (Task 1).
- Produces: `func newRouter(ctrl func() contracts.SessionControl, c client, binds *bindStore, sinks *sinks, cfg routerConfig) *router`, `(*router).onMessage(ctx, messageCreate)`, `(*router).onBindPick(ctx, channel, value string) string`, `BindCustomID(channel string) string`, `ParseBindCustomID(id string) (string, bool)`.

- [ ] **Step 1: Write the failing custom-id test**

Append to `/tmp/wt-gateway/choice_test.go`:

```go
func TestBindCustomIDRoundTrips(t *testing.T) {
	id := BindCustomID("chan1")
	got, ok := ParseBindCustomID(id)
	if !ok || got != "chan1" {
		t.Fatalf("ParseBindCustomID(%q) = %q,%v", id, got, ok)
	}
	if _, ok := ParseBindCustomID(ChoiceCustomID("sess1")); ok {
		t.Fatal("a choice id must not parse as a bind id — the two routes would cross")
	}
	if _, ok := ParseChoiceCustomID(BindCustomID("chan1")); ok {
		t.Fatal("a bind id must not parse as a choice id")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /tmp/wt-gateway && go test ./... -run TestBindCustomID -v`
Expected: FAIL to compile — `undefined: BindCustomID`.

- [ ] **Step 3: Add the second custom-id namespace**

Replace `/tmp/wt-gateway/choice.go` with:

```go
package discord

import "strings"

// The gateway routes two kinds of select-menu click, told apart by the prefix of
// their custom_id. Neither prefix is a prefix of the other, so a click can never
// take the wrong route.
const (
	choiceCustomIDPrefix = "dctlchoice:"
	bindCustomIDPrefix   = "dctlbind:"
)

// ChoiceCustomID builds the custom_id carried by a session's choice select menu.
func ChoiceCustomID(session string) string { return choiceCustomIDPrefix + session }

// ParseChoiceCustomID extracts the session name from a choice-menu custom_id and
// reports whether the id is a choice menu at all.
func ParseChoiceCustomID(id string) (string, bool) {
	return strings.CutPrefix(id, choiceCustomIDPrefix)
}

// BindCustomID builds the custom_id carried by the menu that asks which repo a
// channel should work on.
func BindCustomID(channel string) string { return bindCustomIDPrefix + channel }

// ParseBindCustomID extracts the channel id from a repo-binding custom_id and
// reports whether the id is a binding menu at all.
func ParseBindCustomID(id string) (string, bool) {
	return strings.CutPrefix(id, bindCustomIDPrefix)
}
```

- [ ] **Step 4: Write the failing router test**

Create `/tmp/wt-gateway/router_test.go`:

```go
package discord

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/dctl"
	contracts "github.com/Herrscherd/herrscher-contracts"
)

// fakeCtrl records what the router asked the core to do.
type fakeCtrl struct {
	repos     []contracts.RepoRef
	created   []contracts.CreateSession
	submitted map[string][]contracts.Inbound
	live      map[string]bool
	picked    map[string][]string
}

func newFakeCtrl() *fakeCtrl {
	return &fakeCtrl{
		submitted: map[string][]contracts.Inbound{},
		live:      map[string]bool{},
		picked:    map[string][]string{},
	}
}

func (f *fakeCtrl) Dispatch(context.Context, []string) (string, error) { return "", nil }
func (f *fakeCtrl) Create(_ context.Context, s contracts.CreateSession) (string, error) {
	f.created = append(f.created, s)
	f.live[s.Name] = true
	return "created", nil
}
func (f *fakeCtrl) Close(context.Context, string, bool) (string, error) { return "", nil }
func (f *fakeCtrl) Sessions() []contracts.SessionInfo                   { return nil }
func (f *fakeCtrl) Scrollback(string) []contracts.ScrollbackLine        { return nil }
func (f *fakeCtrl) Resume(string) error                                 { return nil }
func (f *fakeCtrl) Interrupt(string) bool                               { return false }
func (f *fakeCtrl) Submit(name string, in contracts.Inbound) bool {
	if !f.live[name] {
		return false
	}
	f.submitted[name] = append(f.submitted[name], in)
	return true
}
func (f *fakeCtrl) Pick(name, value string) bool {
	if !f.live[name] {
		return false
	}
	f.picked[name] = append(f.picked[name], value)
	return true
}
func (f *fakeCtrl) Repos(context.Context) ([]contracts.RepoRef, error) { return f.repos, nil }

// fakeClient records outbound Discord calls (the client seam from gateway.go).
type fakeClient struct {
	menus []struct{ channel, content, customID string }
	sent  []struct{ channel, content string }
	read  []dctl.Message
}

func (f *fakeClient) Send(_ context.Context, ch, content string) (*dctl.Message, error) {
	f.sent = append(f.sent, struct{ channel, content string }{ch, content})
	return &dctl.Message{ID: "posted"}, nil
}
func (f *fakeClient) Reply(_ context.Context, ch, _, content string) (*dctl.Message, error) {
	return f.Send(context.Background(), ch, content)
}
func (f *fakeClient) React(context.Context, string, string, string) error { return nil }
func (f *fakeClient) SendSelectMenu(_ context.Context, ch, _, content, customID string, _ []dctl.SelectOption) (*dctl.Message, error) {
	f.menus = append(f.menus, struct{ channel, content, customID string }{ch, content, customID})
	return &dctl.Message{ID: "menu"}, nil
}
func (f *fakeClient) ReadMessages(context.Context, string, int, string) ([]dctl.Message, error) {
	return f.read, nil
}

func newTestRouter(t *testing.T) (*router, *fakeCtrl, *fakeClient) {
	t.Helper()
	ctrl, c := newFakeCtrl(), &fakeClient{}
	binds := newBindStore(filepath.Join(t.TempDir(), "router.json"))
	r := newRouter(func() contracts.SessionControl { return ctrl }, c, binds,
		newSinks(context.Background(), &fakeRenderClient{}, "full"),
		routerConfig{owner: "owner1", appID: "app1", contextMessages: 5, playbook: "pr-job"})
	return r, ctrl, c
}

func ownerPing(text string) messageCreate {
	return messageCreate{
		ID: "m1", ChannelID: "c1", Content: text,
		Author:   dctl.Author{ID: "owner1", Username: "leo"},
		Mentions: []dctl.Author{{ID: "app1"}},
	}
}

func TestUnknownChannelAsksWhichRepo(t *testing.T) {
	r, ctrl, c := newTestRouter(t)
	ctrl.repos = []contracts.RepoRef{{Name: "herrscher", Local: true}, {Name: "Herrscherd/dctl"}}

	r.onMessage(context.Background(), ownerPing("fix the login bug"))

	if len(c.menus) != 1 || c.menus[0].customID != BindCustomID("c1") {
		t.Fatalf("menus = %+v, want one bind menu for c1", c.menus)
	}
	if len(ctrl.created) != 0 {
		t.Fatal("a session was created before the operator picked a repo")
	}
}

func TestPickCreatesBindsAndReplaysTheMessage(t *testing.T) {
	r, ctrl, _ := newTestRouter(t)
	ctrl.repos = []contracts.RepoRef{{Name: "herrscher", Local: true}}
	r.onMessage(context.Background(), ownerPing("fix the login bug"))

	r.onBindPick(context.Background(), "c1", "local:herrscher")

	if len(ctrl.created) != 1 {
		t.Fatalf("created = %+v, want one session", ctrl.created)
	}
	spec := ctrl.created[0]
	if spec.ChannelID != "c1" || spec.Project != "herrscher" || spec.Clone != "" {
		t.Fatalf("spec = %+v, want the channel adopted and a local project", spec)
	}
	got := ctrl.submitted[spec.Name]
	if len(got) != 1 || !strings.Contains(got[0].Text, "fix the login bug") {
		t.Fatalf("buffered message not replayed: %+v", got)
	}
	if !strings.Contains(got[0].Text, "pr-job") {
		t.Fatalf("playbook not named in the opening turn: %q", got[0].Text)
	}
}

func TestRemotePickClonesInsteadOfProject(t *testing.T) {
	r, ctrl, _ := newTestRouter(t)
	ctrl.repos = []contracts.RepoRef{{Name: "Herrscherd/dctl"}}
	r.onMessage(context.Background(), ownerPing("bug"))
	r.onBindPick(context.Background(), "c1", "remote:Herrscherd/dctl")

	if spec := ctrl.created[0]; spec.Clone != "Herrscherd/dctl" || spec.Project != "" {
		t.Fatalf("spec = %+v, want a clone", spec)
	}
}

func TestKnownChannelSubmitsDirectly(t *testing.T) {
	r, ctrl, c := newTestRouter(t)
	ctrl.live["ch-c1"] = true
	if err := r.binds.Bind("c1", "ch-c1"); err != nil {
		t.Fatal(err)
	}
	r.onMessage(context.Background(), ownerPing("another one"))

	if len(c.menus) != 0 {
		t.Fatalf("a bound channel still asked: %+v", c.menus)
	}
	if got := ctrl.submitted["ch-c1"]; len(got) != 1 || !strings.Contains(got[0].Text, "another one") {
		t.Fatalf("submitted = %+v", got)
	}
}

func TestNonTriggerMessagesAreIgnored(t *testing.T) {
	r, ctrl, c := newTestRouter(t)
	r.onMessage(context.Background(), messageCreate{
		ID: "m9", ChannelID: "c1", Content: "just chatting",
		Author: dctl.Author{ID: "someone-else"},
	})
	if len(c.menus) != 0 || len(ctrl.created) != 0 || len(ctrl.submitted) != 0 {
		t.Fatal("a non-trigger message reached the core")
	}
}

func TestStaleBindingIsDroppedAndReAsked(t *testing.T) {
	r, ctrl, c := newTestRouter(t)
	ctrl.repos = []contracts.RepoRef{{Name: "herrscher", Local: true}}
	if err := r.binds.Bind("c1", "ch-gone"); err != nil { // never made live
		t.Fatal(err)
	}
	r.onMessage(context.Background(), ownerPing("hello?"))

	if r.binds.Session("c1") != "" {
		t.Fatal("stale binding survived a failed Submit")
	}
	if len(c.menus) != 1 {
		t.Fatalf("menus = %+v, want the repo question re-asked", c.menus)
	}
}

func TestSecondPingReplacesTheBufferedMessage(t *testing.T) {
	r, ctrl, _ := newTestRouter(t)
	ctrl.repos = []contracts.RepoRef{{Name: "herrscher", Local: true}}
	r.onMessage(context.Background(), ownerPing("first"))
	r.onMessage(context.Background(), ownerPing("second"))
	r.onBindPick(context.Background(), "c1", "local:herrscher")

	got := ctrl.submitted[ctrl.created[0].Name]
	if len(got) != 1 || !strings.Contains(got[0].Text, "second") || strings.Contains(got[0].Text, "first") {
		t.Fatalf("replayed %+v, want only the newest ping", got)
	}
}

func TestContextCarriesOtherPeoplesMessages(t *testing.T) {
	r, ctrl, c := newTestRouter(t)
	ctrl.live["ch-c1"] = true
	if err := r.binds.Bind("c1", "ch-c1"); err != nil {
		t.Fatal(err)
	}
	c.read = []dctl.Message{
		{ID: "a", Content: "the button is broken", Author: dctl.Author{Username: "sam"}},
		{ID: "b", Content: "on mobile too", Author: dctl.Author{Username: "kim"}},
	}
	r.onMessage(context.Background(), ownerPing("fix that"))

	text := ctrl.submitted["ch-c1"][0].Text
	for _, want := range []string{"sam", "the button is broken", "kim", "fix that"} {
		if !strings.Contains(text, want) {
			t.Fatalf("context missing %q in:\n%s", want, text)
		}
	}
}

func TestPickOnADeadSessionReportsIt(t *testing.T) {
	r, _, _ := newTestRouter(t)
	if err := r.binds.Bind("c1", "ch-dead"); err != nil {
		t.Fatal(err)
	}
	if msg := r.onChoicePick(context.Background(), "ch-dead", "yes"); msg == "" {
		t.Fatal("a pick on a dead session must tell the operator, not fail silently")
	}
}
```

`fakeRenderClient` is the fake extended in Task 5 Step 2. The `client` interface in `gateway.go` gains `ReadMessages` in Step 6 below.

- [ ] **Step 5: Run it to verify it fails**

Run: `cd /tmp/wt-gateway && go test ./... -run 'TestUnknownChannel|TestPick|TestKnownChannel|TestNonTrigger|TestStale|TestSecondPing|TestContextCarries' -v`
Expected: FAIL to compile — `undefined: newRouter`, `undefined: routerConfig`.

- [ ] **Step 6: Widen the client seam**

In `/tmp/wt-gateway/gateway.go`, add to `type client interface`:

```go
	ReadMessages(ctx context.Context, channelID string, limit int, after string) ([]dctl.Message, error)
```

and to `discordClient`:

```go
func (d discordClient) ReadMessages(ctx context.Context, channelID string, limit int, after string) ([]dctl.Message, error) {
	return d.c.Messages().Read(ctx, channelID, limit, after)
}
```

- [ ] **Step 7: Write the router**

Create `/tmp/wt-gateway/router.go`:

```go
package discord

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Herrscherd/dctl"
	contracts "github.com/Herrscherd/herrscher-contracts"
)

// routerConfig is the plugin's own policy, read once from plugin config.
type routerConfig struct {
	owner           string // Discord user id the bot obeys
	appID           string // the bot's own application id
	contextMessages int    // how many prior channel messages to carry as context
	playbook        string // skill name the opening turn is told to follow
}

// selectMenuMax is Discord's hard cap on options in one select menu.
const selectMenuMax = 25

// router turns a Discord message into a core turn. It owns every Discord-shaped
// decision the flow needs — who may trigger, which session a channel belongs to,
// what context to carry, how a repo question is asked and answered — so the core
// only ever receives a neutral Inbound for a named session.
type router struct {
	ctrl   func() contracts.SessionControl
	c      client
	binds  *bindStore
	sinks  *sinks
	cfg    routerConfig
	trig   trigger

	mu      sync.Mutex
	pending map[string]messageCreate // channel id -> the ping awaiting a repo answer
}

func newRouter(ctrl func() contracts.SessionControl, c client, binds *bindStore, sinks *sinks, cfg routerConfig) *router {
	return &router{
		ctrl:    ctrl,
		c:       c,
		binds:   binds,
		sinks:   sinks,
		cfg:     cfg,
		trig:    trigger{owner: cfg.owner, appID: cfg.appID},
		pending: map[string]messageCreate{},
	}
}

// onMessage is the single entry point from the websocket. Everything that is not
// the owner addressing the bot stops here, before any core call.
func (r *router) onMessage(ctx context.Context, m messageCreate) {
	if !r.trig.fires(m) {
		return
	}
	ctrl := r.ctrl()
	if ctrl == nil {
		return
	}
	if session := r.binds.Session(m.ChannelID); session != "" {
		if r.submit(ctx, ctrl, session, m, false) {
			return
		}
		// The session is gone (daemon restarted, session closed out of band).
		// Drop the stale binding and fall through to ask again, so a restart
		// mid-conversation costs one question rather than silence.
		_ = r.binds.Unbind(m.ChannelID)
	}
	r.ask(ctx, ctrl, m)
}

// ask buffers the ping and posts the repo question. Only the newest ping is kept:
// answering the menu replays one message, and replaying a stale one would be
// worse than dropping it.
func (r *router) ask(ctx context.Context, ctrl contracts.SessionControl, m messageCreate) {
	repos, err := ctrl.Repos(ctx)
	if err != nil || len(repos) == 0 {
		r.post(ctx, m.ChannelID, "je ne trouve aucun repo sur lequel travailler — vérifie le workspace ou l'auth de la forge")
		return
	}
	r.mu.Lock()
	r.pending[m.ChannelID] = m
	r.mu.Unlock()

	opts := make([]dctl.SelectOption, 0, len(repos))
	for _, repo := range repos {
		if len(opts) == selectMenuMax {
			break
		}
		opts = append(opts, dctl.SelectOption{Label: repo.Name, Value: repoValue(repo), Description: repo.Description})
	}
	prompt := "sur quel repo je travaille dans ce salon ? (je ne poserai la question qu'une fois)"
	if len(repos) > selectMenuMax {
		prompt = fmt.Sprintf("%s — %d repos, les %d premiers sont listés", prompt, len(repos), selectMenuMax)
	}
	if _, err := r.c.SendSelectMenu(ctx, m.ChannelID, m.ID, prompt, BindCustomID(m.ChannelID), opts); err != nil {
		fmt.Fprintf(os.Stderr, "discord gateway: repo menu: %v\n", err)
	}
}

// repoValue encodes a RepoRef into a menu value that survives the round trip and
// says which CreateSession field it belongs in.
func repoValue(r contracts.RepoRef) string {
	if r.Local {
		return "local:" + r.Name
	}
	return "remote:" + r.Name
}

// onBindPick answers the repo question: create the session on the picked target,
// adopting this channel, remember the binding, then replay the buffered ping. It
// returns the text the click is acknowledged with.
func (r *router) onBindPick(ctx context.Context, channel, value string) string {
	ctrl := r.ctrl()
	if ctrl == nil {
		return "le contrôleur de sessions n'est pas encore prêt"
	}
	r.mu.Lock()
	m, buffered := r.pending[channel]
	delete(r.pending, channel)
	r.mu.Unlock()

	spec := contracts.CreateSession{
		Name:      sessionNameFor(channel),
		ChannelID: channel,
		Gateways:  []string{"discord"},
	}
	if target, ok := strings.CutPrefix(value, "local:"); ok {
		spec.Project = target
	} else if target, ok := strings.CutPrefix(value, "remote:"); ok {
		spec.Clone = target
	} else {
		return "choix illisible"
	}
	if _, err := ctrl.Create(ctx, spec); err != nil {
		return "création de session impossible : " + err.Error()
	}
	if err := r.binds.Bind(channel, spec.Name); err != nil {
		fmt.Fprintf(os.Stderr, "discord gateway: bind store save failed: %v\n", err)
	}
	if buffered {
		r.submit(ctx, ctrl, spec.Name, m, true)
	}
	return "c'est parti sur " + strings.TrimPrefix(strings.TrimPrefix(value, "local:"), "remote:")
}

// onChoicePick routes an agent's pending-choice answer back to its session. It
// returns the acknowledgement text, empty when the pick landed.
func (r *router) onChoicePick(ctx context.Context, session, value string) string {
	ctrl := r.ctrl()
	if ctrl == nil || !ctrl.Pick(session, value) {
		return "cette session n'est plus active"
	}
	return ""
}

// submit assembles the neutral Inbound and hands it to the core. opening marks
// the first turn of a freshly created session, which is where the playbook is
// named. It reports whether a live session accepted it.
func (r *router) submit(ctx context.Context, ctrl contracts.SessionControl, session string, m messageCreate, opening bool) bool {
	in := contracts.Inbound{
		Conversation: contracts.Conversation{Gateway: "discord", ID: m.ChannelID},
		Author:       m.Author.Username,
		AuthorID:     m.Author.ID,
		Text:         r.compose(ctx, m, opening),
		Attachments:  attachmentsOf(m),
		MessageID:    contracts.MessageID(m.ID),
	}
	if !ctrl.Submit(session, in) {
		return false
	}
	// The ⏳ ack belongs on the message that triggered this turn, in its own
	// channel — the poller used to do this from Read.
	r.sinks.at(m.ChannelID).noteUser(m.ID)
	return true
}

// compose builds the turn text: what everyone else has been saying in this
// channel, then the owner's actual instruction. Assembling context here is what
// keeps the core agnostic — it receives one opaque string.
func (r *router) compose(ctx context.Context, m messageCreate, opening bool) string {
	var b strings.Builder
	if lines := r.context(ctx, m); lines != "" {
		b.WriteString("Contexte du salon (messages précédents, tous auteurs) :\n")
		b.WriteString(lines)
		b.WriteString("\n---\n\n")
	}
	if opening && r.cfg.playbook != "" {
		fmt.Fprintf(&b, "Pour finir ce travail, suis la skill %q.\n\n", r.cfg.playbook)
	}
	b.WriteString(m.Content)
	return b.String()
}

// context reads the channel's recent history over REST. This is not gated by the
// message-content intent — only gateway dispatches are — so the bot sees what
// everyone said without asking Discord for a privileged intent.
func (r *router) context(ctx context.Context, m messageCreate) string {
	if r.cfg.contextMessages <= 0 {
		return ""
	}
	msgs, err := r.c.ReadMessages(ctx, m.ChannelID, r.cfg.contextMessages, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "discord gateway: context read: %v\n", err)
		return ""
	}
	var b strings.Builder
	for _, prev := range msgs {
		if prev.ID == m.ID || strings.TrimSpace(prev.Content) == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", prev.Author.Username, prev.Content)
	}
	return b.String()
}

// attachmentsOf maps the message's uploads to the neutral shape. The host, not
// the gateway, downloads them — against the session's own allowlist.
func attachmentsOf(m messageCreate) []contracts.Attachment {
	if len(m.Attachments) == 0 {
		return nil
	}
	out := make([]contracts.Attachment, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		out = append(out, contracts.Attachment{
			Filename:    a.Filename,
			URL:         a.URL,
			ContentType: a.ContentType,
			Size:        a.Size,
		})
	}
	return out
}

// sessionNameFor derives a stable session name from a channel id, so a restart
// re-binds the same name and it can never collide with an operator-named session.
func sessionNameFor(channel string) string { return "ch-" + channel }

// post is a best-effort operator-facing message in a channel.
func (r *router) post(ctx context.Context, channel, text string) {
	if _, err := r.c.Send(ctx, channel, text); err != nil {
		fmt.Fprintf(os.Stderr, "discord gateway: post: %v\n", err)
	}
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `cd /tmp/wt-gateway && go test ./... -v`
Expected: PASS — all router tests plus everything that came before.

- [ ] **Step 9: Commit**

```bash
cd /tmp/wt-gateway
git add router.go router_test.go choice.go choice_test.go gateway.go
git commit -m "feat(gateway): route owner mentions to sessions, asking which repo once"
```

---

## Task 10: Gateway — wire the routes and the config

**Files:**
- Modify: `/tmp/wt-gateway/slash.go:32-74`
- Modify: `/tmp/wt-gateway/register.go:16-69`
- Modify: `/tmp/wt-gateway/adapters.go:157-170`
- Test: `/tmp/wt-gateway/slash_test.go`

**Interfaces:**
- Consumes: everything from Tasks 5-9.
- Produces: the assembled plugin. Nothing depends on this.

- [ ] **Step 1: Write the failing test**

Append to `/tmp/wt-gateway/slash_test.go`:

```go
func TestComponentInteractionRoutesToPickNotTheRegistry(t *testing.T) {
	r, ctrl, _ := newTestRouter(t)
	ctrl.live["ch-c1"] = true
	s := &slash{ctx: context.Background(), router: r}

	s.onInteraction(context.Background(), dctl.Interaction{
		Type:      dctl.InteractionComponent,
		ChannelID: "c1",
		Data:      dctl.InteractionData{CustomID: ChoiceCustomID("ch-c1"), Values: []string{"yes"}},
	})

	if got := ctrl.picked["ch-c1"]; len(got) != 1 || got[0] != "yes" {
		t.Fatalf("picked = %v, want [yes]", got)
	}
}

func TestBindComponentInteractionCreatesTheSession(t *testing.T) {
	r, ctrl, _ := newTestRouter(t)
	ctrl.repos = []contracts.RepoRef{{Name: "herrscher", Local: true}}
	r.onMessage(context.Background(), ownerPing("fix it"))
	s := &slash{ctx: context.Background(), router: r}

	s.onInteraction(context.Background(), dctl.Interaction{
		Type:      dctl.InteractionComponent,
		ChannelID: "c1",
		Data:      dctl.InteractionData{CustomID: BindCustomID("c1"), Values: []string{"local:herrscher"}},
	})

	if len(ctrl.created) != 1 {
		t.Fatalf("created = %+v, want the session created from the click", ctrl.created)
	}
}

func TestReadIsSuppressedForPushDrivenChannels(t *testing.T) {
	binds := newBindStore(filepath.Join(t.TempDir(), "router.json"))
	if err := binds.Bind("c1", "ch-c1"); err != nil {
		t.Fatal(err)
	}
	p := &Platform{binds: binds}
	p.readImpl = func(context.Context, string, int, string) ([]rawMsg, error) {
		t.Fatal("Read hit the network for a push-driven channel")
		return nil, nil
	}
	got, err := p.Read(context.Background(), "c1", 100, "")
	if err != nil || len(got) != 0 {
		t.Fatalf("Read = %v, %v; want empty and no error", got, err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /tmp/wt-gateway && go test ./... -run 'TestComponent|TestBindComponent|TestReadIsSuppressed' -v`
Expected: FAIL to compile — `slash` has no `router` field, `Platform` has no `binds` field.

- [ ] **Step 3: Route component interactions**

In `/tmp/wt-gateway/slash.go`, add `router *router` to the `slash` struct, take it in `newSlash`, and insert the component branch at the top of `onInteraction`:

```go
func (s *slash) onInteraction(ctx context.Context, ix dctl.Interaction) {
	// A component click is not a command: it answers a menu this gateway posted,
	// and the custom_id says which one. Route it before the command registry,
	// which has no handler for it.
	if ix.Type == dctl.InteractionComponent {
		s.onComponent(ctx, ix)
		return
	}
	// … existing autocomplete + registry dispatch unchanged
}

// onComponent answers a select-menu click: a repo-binding menu creates the
// session, a session-choice menu answers the agent's pending question. The click
// is always acknowledged so the dropdown collapses instead of spinning.
func (s *slash) onComponent(ctx context.Context, ix dctl.Interaction) {
	if s.router == nil {
		return
	}
	value := ""
	if len(ix.Data.Values) > 0 {
		value = ix.Data.Values[0]
	}
	msg := "ok"
	if channel, ok := ParseBindCustomID(ix.Data.CustomID); ok {
		msg = s.router.onBindPick(ctx, channel, value)
	} else if session, ok := ParseChoiceCustomID(ix.Data.CustomID); ok {
		if out := s.router.onChoicePick(ctx, session, value); out != "" {
			msg = out
		} else {
			msg = "choix enregistré : " + value
		}
	} else {
		return
	}
	if s.ix != nil {
		_ = s.ix.Components().Ack(ctx, ix.ID, ix.Token.Reveal(), msg)
	}
}
```

`Ack` lives on `*dctl.Components`; reach it from the client the plugin already holds rather than through `s.ix` if `Interactions` does not expose it — check `dctl.Client.Components()` and pass that sub-client into `slash` the same way `ix` is passed.

Replace the `onMessage` stub with the real delegation:

```go
// onMessage hands every MESSAGE_CREATE to the router, which decides whether it
// is the owner addressing the bot.
func (s *slash) onMessage(ctx context.Context, m messageCreate) {
	if s.router != nil {
		s.router.onMessage(ctx, m)
	}
}
```

- [ ] **Step 4: Suppress `Read` for push-driven channels**

In `/tmp/wt-gateway/adapters.go`, add `binds *bindStore` to `Platform` and guard `Read`:

```go
// Read returns recent channel messages and records the id of the last non-bot
// message so the next turn's ACK reaction lands on it. A channel the router
// drives by push returns nothing: that channel already has an inbound path, and
// two would deliver every message twice.
func (p *Platform) Read(ctx context.Context, channelID string, limit int, after string) ([]contracts.Message, error) {
	if p.binds != nil && p.binds.Session(channelID) != "" {
		return nil, nil
	}
	// … existing body unchanged
}
```

- [ ] **Step 5: Swap the config and assemble**

Replace the `Config` slice in `/tmp/wt-gateway/register.go`'s `init`:

```go
				Config: []contracts.Setting{
					{Key: "token", Env: "DISCORD_BOT_TOKEN", Help: "Discord bot token", Required: true},
					{Key: "owner", Env: "DISCORD_USER_ID", Help: "Discord user id the bot obeys (it reads everyone, acts only for this user)", Required: true},
					{Key: "context_messages", Env: "DISCORD_CONTEXT_MESSAGES", Help: "how many prior channel messages to carry as context (default 30)"},
					{Key: "playbook", Env: "DISCORD_PLAYBOOK", Help: "skill name a new session is told to follow (default pr-job)"},
				},
```

and rewrite `NewGatewaySet`:

```go
func NewGatewaySet(ctx context.Context, cfg contracts.PluginConfig) (contracts.GatewaySet, error) {
	token := cfg.Get("token")
	owner := cfg.Get("owner")
	if owner == "" {
		return contracts.GatewaySet{}, fmt.Errorf("discord gateway: owner (DISCORD_USER_ID) is required — without it the bot would obey everyone")
	}
	// No default channel: the bot is bound to an operator, not to a room, and it
	// always answers in the conversation it was addressed in.
	c := dctl.New(token, "")
	gw := NewGateway(discordClient{c})
	plat := NewPlatform(c)
	binds := newBindStore(bindStorePath())
	plat.binds = binds

	s := newSinks(ctx, renderAdapter{plat}, "full")
	gw.sinks = s
	plat.sinks = s

	appID, err := c.Interactions().AppID(ctx)
	if err != nil {
		return contracts.GatewaySet{}, fmt.Errorf("discord gateway: resolve application id: %w", err)
	}
	gw.slash = newSlash(ctx, c.Interactions(), token, newAllowStore(allowStorePath()))
	gw.slash.router = newRouter(
		func() contracts.SessionControl { return gw.slash.ctrl },
		discordClient{c}, binds, s,
		routerConfig{
			owner:           owner,
			appID:           appID,
			contextMessages: intSetting(cfg.Get("context_messages"), 30),
			playbook:        strSetting(cfg.Get("playbook"), "pr-job"),
		},
	)
	return contracts.GatewaySet{
		Gateway: gw,
		Reader:  plat,
		Admin:   NewChannelAdmin(c),
		Prober:  NewProber(c),
	}, nil
}

// bindStorePath sits beside the allow store, under the same state dir.
func bindStorePath() string { return filepath.Join(stateDir(), "discord-router.json") }

// intSetting parses a numeric setting, falling back to def for empty or invalid
// values — a typo must not silently disable channel context.
func intSetting(v string, def int) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func strSetting(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
```

Factor the existing `allowStorePath` body into a `stateDir()` helper both paths use, and add `fmt`, `strconv` to the imports.

- [ ] **Step 6: Run the whole suite**

Run: `cd /tmp/wt-gateway && go vet ./... && go test ./... -v`
Expected: PASS, vet clean.

- [ ] **Step 7: Update the README**

In `/tmp/wt-gateway/README.md`, replace the config row and the mono-channel paragraph:

```markdown
| **Config & env** | `token` / `DISCORD_BOT_TOKEN` (**required**), `owner` / `DISCORD_USER_ID` (**required**, the user the bot obeys), `context_messages` / `DISCORD_CONTEXT_MESSAGES` (default 30), `playbook` / `DISCORD_PLAYBOOK` (default `pr-job`), `DCTL_STATE_DIR` (default: `~/.config/dctl`) |
```

and, replacing the "Mono-channel by design" sentence:

```markdown
**Owner-bound, not channel-bound.** The gateway identifies with `GUILD_MESSAGES`
and `DIRECT_MESSAGES` — both non-privileged — and acts on a message only when the
configured owner @mentions the bot or replies to it. Everyone else's messages are
never triggers, but the last `context_messages` messages of the channel are read
over REST and carried into the turn, so the agent sees the whole conversation.
The first ping in an unknown channel asks which repo to work on with a select
menu; the answer creates a session that **adopts that channel** and is remembered
in `discord-router.json`. Rendering is per conversation: each channel gets its own
progress message, ⏳ ack and reply.
```

- [ ] **Step 8: Commit**

```bash
cd /tmp/wt-gateway
git add slash.go slash_test.go register.go adapters.go README.md
git commit -m "feat(gateway): bind to an owner id, route clicks, drop the channel setting"
```

---

## Task 11: The editable playbook

**Files:**
- Create: `skills/pr-job/SKILL.md` (in the `herrscher` worktree)
- Test: `core/skills/discover_test.go`

**Interfaces:**
- Consumes: the `playbook` setting name (Task 10).
- Produces: a skill named `pr-job`.

- [ ] **Step 1: Write the failing test**

Append to `core/skills/discover_test.go`:

```go
func TestPRJobSkillIsDiscoverable(t *testing.T) {
	got := Discover([]string{"../../skills"})
	for _, s := range got {
		if s.Name == "pr-job" {
			if s.Description == "" {
				t.Fatal("pr-job has no description — the menu would show a blank line")
			}
			return
		}
	}
	t.Fatalf("pr-job not discovered in %+v", got)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./core/skills/ -run TestPRJob -v`
Expected: FAIL — `pr-job not discovered in []`.

- [ ] **Step 3: Write the skill**

Create `skills/pr-job/SKILL.md`:

```markdown
---
name: pr-job
description: Use when a task arrives from chat and must end as a pull request — fix in the session worktree, review it against every axis, then open the PR.
---

# Finishing a chat-triggered job as a PR

You are working in this session's own git worktree. The job ends with a pull
request, not with a message saying the work is done.

## 1. Understand before touching anything

Read the request and every screenshot attached to it. The screenshots are the
report: reproduce what they show before you decide what is broken. If the request
is ambiguous in a way that changes what you would build, end your turn with a
question and concrete options — the gateway renders them as a menu and your next
turn receives the answer. Ask once, with real options; do not ask what you can
determine by reading the code.

## 2. Fix

Make the smallest change that actually fixes the reported problem. Follow the
surrounding code's patterns, naming and comment density.

## 3. Review your own diff, on every axis

Go through these in order and fix what you find. Report only what you verified —
a finding you cannot demonstrate is a false positive, and false positives cost
more than silence.

- **CI compliance** — run the project's own build, tests, vet and lint. Paste the
  real output. Never claim green without it.
- **Architecture** — does the change respect the project's boundaries? A layer
  that suddenly knows about another layer's concrete types is a defect, not a
  shortcut.
- **Performance** — new allocations in hot paths, N+1 calls, unbounded goroutines,
  work done per-item that could be done once.
- **Code quality** — dead code, duplicated logic, names that do not say what the
  thing is, error paths that swallow the cause.
- **Security** — untrusted input reaching the filesystem, a shell, or a network
  call; secrets in logs; permissions on files you create.
- **Bug review** — read the diff as an adversary. What input makes this wrong?
  What happens on the empty, the concurrent, and the restart case?
- **Useless comments** — delete comments that restate the code. Keep the ones
  that explain *why*.
- **Docs** — update the README and any doc the change contradicts, so the docs
  describe the code as it is now.

## 4. Open the PR

Commit with a conventional-commit message, push the session branch, and open the
PR with `gh pr create`, whose body states what changed, why, and how you verified
it. Post the PR URL as your reply so it lands in the channel.
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./core/skills/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add skills/pr-job/SKILL.md core/skills/discover_test.go
git commit -m "feat(skills): pr-job playbook for chat-triggered jobs"
```

---

## Task 12: Release and integrate

**Files:**
- Modify: `go.mod`, `go.sum` (this worktree)
- Modify: `README.md` (this worktree)

**Interfaces:**
- Consumes: the tagged versions of both plugin repos.
- Produces: a host that builds with no `replace` directive.

- [ ] **Step 1: Merge and tag contracts**

```bash
cd /home/shan/dev/herrscher-contracts
git fetch origin && git push origin feat/mention-seam
gh pr create --fill --base master --head feat/mention-seam
```

Merge the PR, then from a clean master checkout:

```bash
git -C /home/shan/dev/herrscher-contracts fetch origin
git tag v0.2.14 origin/master && git push origin v0.2.14
```

Expected: `v0.2.14` visible in `git ls-remote --tags origin`.

- [ ] **Step 2: Point the gateway at the tag and release it**

```bash
cd /tmp/wt-gateway
go mod edit -dropreplace github.com/Herrscherd/herrscher-contracts
go get github.com/Herrscherd/herrscher-contracts@v0.2.14
go mod tidy && go test ./...
git add go.mod go.sum && git commit -m "chore(deps): contracts v0.2.14"
git push origin feat/mention-flow
gh pr create --fill --base master --head feat/mention-flow
```

Merge, then tag `v0.3.0` (a breaking config change: `channel` is gone, `owner` is required) and push the tag.

- [ ] **Step 3: Point the host at both tags**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/discord-ai-pr-automation
go mod edit -dropreplace github.com/Herrscherd/herrscher-contracts
go get github.com/Herrscherd/herrscher-contracts@v0.2.14
go get github.com/Herrscherd/herrscher-discord-gateway@v0.3.0
go mod tidy
```

- [ ] **Step 4: Verify the whole thing builds and passes**

Run:

```bash
go build ./... && go vet ./... && go test ./...
grep -rn 'replace ' go.mod
```

Expected: build and vet clean, every package `ok`, and **no output** from the grep — a `replace` left behind would break CI, which fetches with no local checkouts.

- [ ] **Step 5: Update the host README**

In `README.md`, the plugin table's Gateway row and the durable-sessions bullet both describe a session as "a channel + an agent + a worktree". Amend the bullet to note the Discord edge can also adopt an existing channel from a mention, and leave the terminal path's description alone.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum README.md
git commit -m "chore(deps): contracts v0.2.14, discord gateway v0.3.0"
```

- [ ] **Step 7: Manual smoke test**

With `DISCORD_BOT_TOKEN` and `DISCORD_USER_ID` set, run `./herrscher` and, in a
Discord channel the bot can see:

1. Have someone else post a message. Nothing happens — no reaction, no reply.
2. @mention the bot with a screenshot attached and a one-line bug report. Expect a
   select menu asking which repo.
3. Pick a repo. Expect the menu to collapse, a session banner in the channel, and
   a ⏳ on your message followed by a live progress message.
4. Reply to the bot's message without mentioning it. Expect a second turn, no repo
   question.
5. Restart the daemon and reply again. Expect the binding to survive; if the
   session did not, expect exactly one repo question, not silence.

---

## Self-review notes

- **Spec coverage:** owner binding → Tasks 7, 10; sees others' messages → Task 9
  (`context`); mention-or-reply trigger → Task 7; stays in channel → Task 9
  (`ChannelID` adoption) + Task 3; screenshots → Task 9 (`attachmentsOf`) + Task 2
  (`submit` resolves them); repo menu, persisted → Tasks 8, 9, 10; agent-driven
  worktree→PR → Task 11; editable `.md` → Task 11; select-menu questions → Task 10
  (`onComponent`) + Task 4 (`Pick`); per-conversation rendering → Task 5; core
  stays agnostic → enforced by the purity tests run in Tasks 2, 4, 12.
- **Known follow-up not in this plan:** `Platform.RouteMenu` and
  `contracts.MenuRouter` remain without a core consumer. This plan wires the
  *inbound* half (clicks now reach `Pick`); the host still has no code path that
  *posts* a routed menu when a backend declares a `PendingChoice`. Landing that is
  a separate, small slice in `core/host` — until it lands, choice menus appear only
  if a gateway posts them itself.
