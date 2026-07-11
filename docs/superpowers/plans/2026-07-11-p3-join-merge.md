# P3 Join + Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give a lead session a `⟢ merge: <worker>` trailer that aggregates a worker's `session/<worker>` branch into the lead's worktree via a real `git merge`, with clean abort + diagnostic on conflict.

**Architecture:** Model O (host-driven). A new `worktree.MergeInto` runs the git merge; a new `coordinator.Merge` guards then calls it and seeds the outcome back to the lead; a new `parseMerge` trailer feeds the existing `maybeCoordinate` dispatch. The `Coordinator` port in `herrscher-contracts` gains a `Merge` method + `MergeRequest` type (this tranche touches contracts — the merge is an *exposed* capability, unlike host-internal `forget`).

**Tech Stack:** Go, two modules — `github.com/Herrscherd/herrscher-contracts` (local `/home/shan/dev/herrscher-contracts`) and `github.com/Herrscherd/herrscher` (local `/home/shan/dev/herrscher`). Real `git` for worktree tests, injected fakes for coordinator tests.

## Global Constraints

- **Générique OSS**: zero Roblox/Neublox domain knowledge in code or messages — vocabulary is "worker"/"lead" only.
- **herrscher-contracts touched (deliberate)**: add `Merge` method + `MergeRequest`; every other `Coordinator` signature stays byte-for-byte unchanged.
- **Worktree lead always clean**: success (merge commit) or failure (abort) both leave the lead worktree with no uncommitted changes — shared invariant with all other primitives.
- **No effect on join state**: `Merge` never touches `reported`/`mu`. Delivery-tracking (`Report`) and aggregation (`Merge`) are orthogonal.
- **W stays alive**: no teardown on merge. One worker per trailer.
- **CI green both modules**: `gofmt -l` clean, `go vet ./...` clean, `go build ./...`, `go test -race ./...` green, `go mod tidy` leaves `go.mod` unchanged.
- **Commit identity**: `git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit`. Footer on every commit:
  ```
  Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
  Co-Authored-By: Claude <noreply@anthropic.com>
  ```
- **1 logical commit per task.**

---

## File Structure

**herrscher-contracts** (`/home/shan/dev/herrscher-contracts`):
- `coordinator.go` — add `MergeRequest` struct + `Merge` method on `Coordinator`.
- `coordinator_test.go` (create if absent) — compile-time assertion the port shape holds.

**herrscher** (`/home/shan/dev/herrscher`):
- `core/internal/worktree/worktree.go` — add `MergeOutcome` enum + `MergeInto` method.
- `core/internal/worktree/worktree_test.go` — real-git tests for `MergeInto`.
- `core/host/handoff.go` — add `mergeMarker` const + `parseMerge` parser (parsers already live here).
- `core/host/handoff_test.go` (or wherever parser tests live) — `TestParseMerge`.
- `core/host/coordinator.go` — extend `cleanBrancher` interface with `MergeInto`; add `Merge` method.
- `core/host/coordinator_test.go` — extend `fakeWTC` with merge behavior; add `Merge` tests.
- `core/host/turnloop.go` — insert `parseMerge` branch into `maybeCoordinate`.

---

## Task 1: Contracts — `MergeRequest` + `Merge` port method

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/coordinator.go`
- Test: `/home/shan/dev/herrscher-contracts/coordinator_test.go`

**Interfaces:**
- Produces: `contracts.MergeRequest{FromSession, Worker string}` and the interface method `Merge(ctx context.Context, req MergeRequest) (lead string, err error)` on `contracts.Coordinator`. Task 4 (coordinator) implements it; Task 4 (dispatch) constructs `MergeRequest`.

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-contracts/coordinator_test.go`:

```go
package contracts

import (
	"context"
	"testing"
)

// mergeOnly is the smallest type carrying just the new method, to assert the
// port shape at compile time without a full Coordinator implementation.
type mergeStub struct{}

func (mergeStub) Handoff(context.Context, HandoffRequest) (string, error)   { return "", nil }
func (mergeStub) Delegate(context.Context, DelegateRequest) (string, error) { return "", nil }
func (mergeStub) Report(context.Context, ReportRequest) (string, error)     { return "", nil }
func (mergeStub) Merge(context.Context, MergeRequest) (string, error)       { return "", nil }

func TestCoordinatorPortIncludesMerge(t *testing.T) {
	var _ Coordinator = mergeStub{}
	req := MergeRequest{FromSession: "lead", Worker: "w"}
	if req.FromSession != "lead" || req.Worker != "w" {
		t.Fatalf("MergeRequest fields not wired: %+v", req)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestCoordinatorPortIncludesMerge`
Expected: FAIL — `undefined: MergeRequest` and `mergeStub does not implement Coordinator (missing Merge)`.

- [ ] **Step 3: Write minimal implementation**

In `/home/shan/dev/herrscher-contracts/coordinator.go`, add the request type after `ReportRequest` (before the `Coordinator` interface):

```go
// MergeRequest is the typed intent for a merge L←W: the lead L aggregates
// worker W's committed branch (session/<W>) into L's own worktree via a real
// git merge. Lead-initiated: L decides when and which worker to pull. W stays
// alive; the merge has no effect on the delivery-tracking join state.
type MergeRequest struct {
	FromSession string // the lead triggering the merge (the merge target)
	Worker      string // the worker whose session/<Worker> branch is aggregated
}
```

Add the method inside the `Coordinator` interface, after `Report`:

```go
	// Merge aggregates worker W's committed branch into the lead's worktree and
	// returns the lead's name. It errors on an unknown lead or worker, a worker
	// that is not a child of this lead, or a dirty lead/worker worktree. A merge
	// conflict is not an error: the merge is aborted (lead left clean) and the
	// lead is seeded a diagnostic. W stays alive; join state is untouched.
	Merge(ctx context.Context, req MergeRequest) (lead string, err error)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-contracts && go test -race ./...`
Expected: PASS.

- [ ] **Step 5: Verify CI gates**

Run: `cd /home/shan/dev/herrscher-contracts && gofmt -l . && go vet ./... && cp go.mod /tmp/gm.bak && go mod tidy && diff go.mod /tmp/gm.bak && echo CLEAN`
Expected: `gofmt -l` prints nothing; `go vet` silent; `diff` shows no change; `CLEAN` printed.

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher-contracts && git add coordinator.go coordinator_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(coordinator): Merge port method + MergeRequest (P3 join+merge)

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 2: `worktree.MergeInto` (real git)

**Files:**
- Modify: `/home/shan/dev/herrscher/core/internal/worktree/worktree.go`
- Test: `/home/shan/dev/herrscher/core/internal/worktree/worktree_test.go`

**Interfaces:**
- Produces: `worktree.MergeOutcome` enum (`MergeApplied`, `MergeUpToDate`, `MergeConflict`) and method `func (w *Worktreer) MergeInto(leadPath, branch string) (MergeOutcome, []string, error)`. Task 4's `cleanBrancher` interface consumes this exact signature; the second return (conflict file list) is populated only when outcome is `MergeConflict`.

**Note on existing patterns:** `worktree.go` already runs git via `exec.CommandContext(w.ctx, "git", ...)`. Match that style. The package uses `--git-dir` probing (`isGitRepo`) but `MergeInto` runs inside a known worktree path, so it can assume a git dir and surface any failure.

- [ ] **Step 1: Write the failing test**

Add to `/home/shan/dev/herrscher/core/internal/worktree/worktree_test.go`. **Reuse the existing `initRepo(t)` helper** (already in this file: makes a temp git repo with `git config user.email/name` set + one empty commit — so later commits in that repo or its worktrees inherit the identity, no env vars needed). Add one small commit helper and the two behavior tests. The base repo has no tracked file, so each test first commits a base `f.txt` on the main worktree.

```go
// gitAt runs a git command in dir, failing the test on error. Identity is
// inherited from the repo config that initRepo already set.
func gitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s", args, out)
	}
}

// commitFile writes name=content in dir and commits it.
func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", msg)
}
```

Then the two behavior tests:

```go
func TestMergeIntoCleanMerge(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "")
	// Base file on the lead's main worktree.
	commitFile(t, repo, "f.txt", "base\n", "base file")

	// A worker branch (created off the base) that adds a new, non-conflicting file.
	if _, err := w.Create(repo, "worker", ""); err != nil {
		t.Fatal(err)
	}
	wp := w.Path(repo, "worker")
	commitFile(t, wp, "g.txt", "worker\n", "worker work")

	// Merge the worker branch into the repo's main worktree (the lead here).
	outcome, conflicts, err := w.MergeInto(repo, w.Branch("worker"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if outcome != MergeApplied {
		t.Fatalf("outcome = %v, want MergeApplied", outcome)
	}
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %v, want none", conflicts)
	}
	// Lead worktree must be clean and now contain the worker's file.
	if clean, _ := w.IsCleanAt(repo); !clean {
		t.Fatal("lead worktree not clean after merge")
	}
	if _, err := os.Stat(filepath.Join(repo, "g.txt")); err != nil {
		t.Fatalf("merged file missing: %v", err)
	}
}

func TestMergeIntoConflictAborts(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "")
	// Base file both sides will edit on the same line.
	commitFile(t, repo, "f.txt", "base\n", "base file")

	// Worker (branched off base) edits f.txt.
	if _, err := w.Create(repo, "worker", ""); err != nil {
		t.Fatal(err)
	}
	wp := w.Path(repo, "worker")
	commitFile(t, wp, "f.txt", "worker-change\n", "worker edit")

	// Lead diverges on the same line, committed (so the lead worktree is clean).
	commitFile(t, repo, "f.txt", "lead-change\n", "lead edit")

	outcome, conflicts, err := w.MergeInto(repo, w.Branch("worker"))
	if err != nil {
		t.Fatalf("conflict must not be an error, got: %v", err)
	}
	if outcome != MergeConflict {
		t.Fatalf("outcome = %v, want MergeConflict", outcome)
	}
	if len(conflicts) == 0 || conflicts[0] != "f.txt" {
		t.Fatalf("conflicts = %v, want [f.txt]", conflicts)
	}
	// Abort must have restored a clean worktree.
	if clean, _ := w.IsCleanAt(repo); !clean {
		t.Fatal("lead worktree not clean after conflict abort")
	}
}
```

The test file already imports `context`, `os`, `os/exec`, `path/filepath`, `testing` — no import change. `initRepo`, `gitAt`, and `commitFile` live in the same package/file.

**Watch-out (worker base):** `w.Create(repo, "worker", "")` with an empty base branches off the lead worktree's current `HEAD`. Since `commitFile(repo, "f.txt", ...)` runs *before* `Create`, the worker starts from the base commit that has `f.txt` — required for the conflict test to actually conflict. Keep that ordering.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/worktree/ -run TestMergeInto`
Expected: FAIL — `undefined: MergeApplied` / `w.MergeInto undefined`.

- [ ] **Step 3: Write minimal implementation**

In `/home/shan/dev/herrscher/core/internal/worktree/worktree.go`, add after `Remove`:

```go
// MergeOutcome classifies a MergeInto result.
type MergeOutcome int

const (
	MergeApplied  MergeOutcome = iota // a merge commit was created
	MergeUpToDate                     // nothing to merge — branch already in lead
	MergeConflict                     // conflict — the merge was aborted
)

// MergeInto runs `git -C leadPath merge --no-edit branch`. On success it reports
// MergeApplied (a commit was created) or MergeUpToDate (HEAD unchanged). On a
// conflict it returns MergeConflict plus the conflicted file list and runs
// `git merge --abort`, so leadPath is left clean either way. Any other git
// failure is returned as an error (with a best-effort abort first, so a
// half-started merge never lingers).
func (w *Worktreer) MergeInto(leadPath, branch string) (MergeOutcome, []string, error) {
	pre, err := w.headAt(leadPath)
	if err != nil {
		return MergeConflict, nil, fmt.Errorf("merge: cannot read HEAD of %q: %w", leadPath, err)
	}
	out, mergeErr := exec.CommandContext(w.ctx, "git", "-C", leadPath, "merge", "--no-edit", branch).CombinedOutput()
	if mergeErr == nil {
		post, err := w.headAt(leadPath)
		if err != nil {
			return MergeApplied, nil, fmt.Errorf("merge: cannot read HEAD of %q after merge: %w", leadPath, err)
		}
		if post == pre {
			return MergeUpToDate, nil, nil
		}
		return MergeApplied, nil, nil
	}
	// Merge failed. If files are in conflict, abort and report them cleanly.
	conflicts := w.conflictFiles(leadPath)
	_ = exec.CommandContext(w.ctx, "git", "-C", leadPath, "merge", "--abort").Run()
	if len(conflicts) > 0 {
		return MergeConflict, conflicts, nil
	}
	return MergeConflict, nil, fmt.Errorf("merge %s into %q: %s", branch, leadPath, strings.TrimSpace(string(out)))
}

// headAt returns the commit HEAD points to in the given worktree.
func (w *Worktreer) headAt(path string) (string, error) {
	out, err := exec.CommandContext(w.ctx, "git", "-C", path, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// conflictFiles lists paths currently in an unmerged (conflicted) state.
func (w *Worktreer) conflictFiles(path string) []string {
	out, err := exec.CommandContext(w.ctx, "git", "-C", path, "diff", "--name-only", "--diff-filter=U").Output()
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
```

`fmt`, `os/exec`, `strings` are already imported in this file — no import change.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test -race ./core/internal/worktree/ -run TestMergeInto -v`
Expected: PASS (both `TestMergeIntoCleanMerge` and `TestMergeIntoConflictAborts`).

- [ ] **Step 5: Verify package + CI gates**

Run: `cd /home/shan/dev/herrscher && go test -race ./core/internal/worktree/ && gofmt -l core/internal/worktree/ && go vet ./core/internal/worktree/`
Expected: all worktree tests pass; `gofmt -l` prints nothing; `go vet` silent.

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/internal/worktree/worktree.go core/internal/worktree/worktree_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(worktree): MergeInto — real git merge with clean abort on conflict

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 3: `parseMerge` trailer parser

**Files:**
- Modify: `/home/shan/dev/herrscher/core/host/handoff.go`
- Test: `/home/shan/dev/herrscher/core/host/handoff_test.go`

**Interfaces:**
- Consumes: existing `parseTrailer(reply, marker string) (string, bool)` in the same file.
- Produces: `mergeMarker = "⟢ merge:"` const and `parseMerge(reply string) (worker string, ok bool)`. Task 4's dispatch consumes `parseMerge`. Unlike delegate/handoff there is no em-dash split — the whole body is a single worker name (mirrors `parseDone`).

**Note:** find the file that holds the existing parser tests (`TestParseDone`/`TestParseHandoff`). If it is not `handoff_test.go`, put `TestParseMerge` beside them.

- [ ] **Step 1: Write the failing test**

Add beside the existing parser tests:

```go
func TestParseMerge(t *testing.T) {
	cases := []struct {
		name       string
		reply      string
		wantWorker string
		wantOK     bool
	}{
		{"valid", "doing the thing\n⟢ merge: worker-x", "worker-x", true},
		{"trims spaces", "x\n⟢ merge:   worker-2  ", "worker-2", true},
		{"empty body", "x\n⟢ merge:", "", false},
		{"not last line", "⟢ merge: worker-x\nmore text", "", false},
		{"different marker", "x\n⟢ done: summary", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, ok := parseMerge(tc.reply)
			if ok != tc.wantOK || w != tc.wantWorker {
				t.Fatalf("parseMerge(%q) = (%q, %v), want (%q, %v)", tc.reply, w, ok, tc.wantWorker, tc.wantOK)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestParseMerge`
Expected: FAIL — `undefined: parseMerge`.

- [ ] **Step 3: Write minimal implementation**

In `/home/shan/dev/herrscher/core/host/handoff.go`, add `mergeMarker` to the const block:

```go
const (
	handoffMarker  = "⟢ handoff:"
	delegateMarker = "⟢ delegate:"
	doneMarker     = "⟢ done:"
	mergeMarker    = "⟢ merge:"
)
```

And add the parser after `parseDone`:

```go
// parseMerge extracts a merge intent: "⟢ merge: <worker>". Like done and unlike
// handoff/delegate, the whole body is a single token (the worker name) — no
// em-dash split. An empty body is not a merge intent.
func parseMerge(reply string) (worker string, ok bool) {
	body, ok := parseTrailer(reply, mergeMarker)
	if !ok || body == "" {
		return "", false
	}
	return body, true
}
```

Also update the const-block comment above it (currently "done over delegate over handoff") to reflect the new priority — change it to read:
`// done has priority over delegate over merge over handoff when dispatched (see maybeCoordinate).`

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestParseMerge -v`
Expected: PASS (all sub-cases).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/host/handoff.go core/host/handoff_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): parseMerge trailer parser (P3 join+merge)

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 4: `coordinator.Merge` + dispatch wiring

**Files:**
- Modify: `/home/shan/dev/herrscher/core/host/coordinator.go`
- Modify: `/home/shan/dev/herrscher/core/host/turnloop.go`
- Test: `/home/shan/dev/herrscher/core/host/coordinator_test.go`

**Interfaces:**
- Consumes: `contracts.MergeRequest` + `Coordinator.Merge` (Task 1); `worktree.MergeOutcome`/`MergeApplied`/`MergeUpToDate`/`MergeConflict` + `MergeInto` (Task 2); `parseMerge` (Task 3); existing `findByName`, `seedWithRetry`, `c.wt.Branch`, `c.wt.IsCleanAt`.
- Produces: `coordinator.Merge` implementing the port; the `maybeCoordinate` dispatch branch.

**Design recap (from spec):** guards on a single atomic snapshot, in order — lead known → worker known → worker is a child of this lead (`Parent == FromSession`) → worker clean → lead clean. Then `MergeInto(leadPath, session/<W>)`; seed the lead the outcome message; return `(lead, nil)`. Conflict is seeded, not an error. `reported`/`mu` untouched.

- [ ] **Step 1: Write the failing tests**

First extend the test fake so per-path cleanliness and a scripted merge outcome can be injected. In `/home/shan/dev/herrscher/core/host/coordinator_test.go`, replace the `fakeWTC` block (lines ~29-39) with:

```go
type fakeWTC struct {
	clean    bool
	err      error
	branches map[string]bool
	// cleanBy overrides `clean` per worktree path when non-nil (path → clean?).
	cleanBy map[string]bool
	// merge scripts MergeInto's return; mergedBranch records the branch merged.
	merge        worktree.MergeOutcome
	mergeConf    []string
	mergeErr     error
	mergedBranch string
}

func (f *fakeWTC) IsCleanAt(path string) (bool, error) {
	if f.cleanBy != nil {
		return f.cleanBy[path], f.err
	}
	return f.clean, f.err
}
func (f *fakeWTC) Branch(name string) string { return "session/" + name }
func (f *fakeWTC) BranchExistsAt(_, branch string) (bool, error) {
	return f.branches[branch], nil
}
func (f *fakeWTC) MergeInto(_, branch string) (worktree.MergeOutcome, []string, error) {
	f.mergedBranch = branch
	return f.merge, f.mergeConf, f.mergeErr
}
```

Because `fakeWTC` is now used by pointer (it records `mergedBranch`), update `newTestCoordinatorFull` (line ~62) to pass a pointer:

```go
	return newCoordinator(cr, fakeAgents{known: km}, &fakeWTC{clean: clean, branches: branches}, fakeSessions{list: sessions}, closer, seed)
```

Add `"github.com/Herrscherd/herrscher/core/internal/worktree"` to the test file imports.

Now add the `Merge` tests (place after the existing `Report` tests):

```go
func leadWorkerSessions() []state.Session {
	return []state.Session{
		{Name: "lead", Worktree: "/wt/lead"},
		{Name: "w", Parent: "lead", Worktree: "/wt/w"},
	}
}

func TestMergeDeliversWorkerBranchToLead(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	c.wt.(*fakeWTC).merge = worktree.MergeApplied

	lead, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if lead != "lead" {
		t.Fatalf("lead = %q, want lead", lead)
	}
	if got := c.wt.(*fakeWTC).mergedBranch; got != "session/w" {
		t.Fatalf("merged branch = %q, want session/w", got)
	}
	if len(seeded) != 1 || !strings.Contains(seeded[0], "lead|") || !strings.Contains(seeded[0], "w") {
		t.Fatalf("seeded = %v, want one message to lead mentioning w", seeded)
	}
}

func TestMergeConflictAbortsAndReports(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	f := c.wt.(*fakeWTC)
	f.merge = worktree.MergeConflict
	f.mergeConf = []string{"a.go", "b.go"}

	lead, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"})
	if err != nil {
		t.Fatalf("conflict must not be an error: %v", err)
	}
	if lead != "lead" {
		t.Fatalf("lead = %q, want lead", lead)
	}
	if len(seeded) != 1 || !strings.Contains(seeded[0], "conflit") || !strings.Contains(seeded[0], "a.go") {
		t.Fatalf("seeded = %v, want conflict diagnostic listing files", seeded)
	}
}

func TestMergeUpToDateIsNeutral(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	c.wt.(*fakeWTC).merge = worktree.MergeUpToDate

	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(seeded) != 1 || !strings.Contains(seeded[0], "déjà à jour") {
		t.Fatalf("seeded = %v, want an up-to-date message", seeded)
	}
}

func TestMergeRefusesNonChildWorker(t *testing.T) {
	var seeded []string
	sessions := []state.Session{
		{Name: "lead", Worktree: "/wt/lead"},
		{Name: "w", Parent: "other", Worktree: "/wt/w"}, // not a child of lead
	}
	c := newTestCoordinator(&fakeCreator{}, nil, true, sessions, &seeded)

	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err == nil {
		t.Fatal("expected refusal for non-child worker")
	}
	if c.wt.(*fakeWTC).mergedBranch != "" {
		t.Fatal("MergeInto must not be called on a non-child worker")
	}
}

func TestMergeRefusesDirtyWorker(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	// Lead clean, worker dirty.
	c.wt.(*fakeWTC).cleanBy = map[string]bool{"/wt/lead": true, "/wt/w": false}

	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err == nil {
		t.Fatal("expected refusal for dirty worker")
	}
	if c.wt.(*fakeWTC).mergedBranch != "" {
		t.Fatal("MergeInto must not be called when worker is dirty")
	}
}

func TestMergeRefusesDirtyLead(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	// Worker clean, lead dirty.
	c.wt.(*fakeWTC).cleanBy = map[string]bool{"/wt/lead": false, "/wt/w": true}

	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err == nil {
		t.Fatal("expected refusal for dirty lead")
	}
	if c.wt.(*fakeWTC).mergedBranch != "" {
		t.Fatal("MergeInto must not be called when lead is dirty")
	}
}

func TestMergeUnknownWorker(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)
	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "ghost"}); err == nil {
		t.Fatal("expected refusal for unknown worker")
	}
}

func TestMergeUnknownLead(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{{Name: "w", Parent: "lead", Worktree: "/wt/w"}}, &seeded)
	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err == nil {
		t.Fatal("expected refusal for unknown lead")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestMerge`
Expected: FAIL — `c.Merge undefined` (and the fake edits won't compile until Step 3's interface method exists — that is expected; the compile failure IS the red state).

- [ ] **Step 3: Write the implementation**

In `/home/shan/dev/herrscher/core/host/coordinator.go`:

(a) Add `strings` (currently NOT imported — verified) and `worktree` to the import block. After the edit the imports read:

```go
import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)
```

(b) Extend the `cleanBrancher` interface (currently `IsCleanAt`/`Branch`/`BranchExistsAt`) with:

```go
	MergeInto(leadPath, branch string) (worktree.MergeOutcome, []string, error)
```

(c) Add the `Merge` method after `Report` (and before `forget`):

```go
// Merge aggregates worker W's committed branch (session/<W>) into the lead's
// worktree via a real git merge, then seeds the lead the outcome. Lead-initiated
// (⟢ merge). Every guard runs before the side effect, on one atomic snapshot:
// lead known → worker known → worker is a child of THIS lead → worker committed →
// lead committed. Lead-clean is required so a conflict abort restores the lead's
// worktree without discarding uncommitted lead work. A conflict is not an error:
// MergeInto aborts (lead left clean) and the lead is seeded a diagnostic. W stays
// alive; the join state (reported/mu) is deliberately untouched — delivery
// tracking and aggregation are orthogonal.
func (c *coordinator) Merge(ctx context.Context, req contracts.MergeRequest) (string, error) {
	sessions := c.sessions.SnapshotSessions()
	lead, ok := findByName(sessions, req.FromSession)
	if !ok {
		return "", fmt.Errorf("merge: lead %q not found", req.FromSession)
	}
	worker, ok := findByName(sessions, req.Worker)
	if !ok {
		return "", fmt.Errorf("merge: worker %q not found", req.Worker)
	}
	if worker.Parent != req.FromSession {
		return "", fmt.Errorf("merge refused: %q is not a worker of %q", req.Worker, req.FromSession)
	}
	// Worker must be committed: session/<W>'s tip is what gets merged, so
	// uncommitted worker work would not be aggregated. Mirror the Report guard.
	wClean, err := c.wt.IsCleanAt(worker.Worktree)
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	if !wClean {
		return "", fmt.Errorf("merge refused: worker %q has uncommitted changes — commit first", req.Worker)
	}
	// Lead must be committed so a conflict abort restores it cleanly without
	// clobbering uncommitted lead work.
	lClean, err := c.wt.IsCleanAt(lead.Worktree)
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	if !lClean {
		return "", fmt.Errorf("merge refused: lead %q has uncommitted changes — commit first", req.FromSession)
	}

	outcome, conflicts, err := c.wt.MergeInto(lead.Worktree, c.wt.Branch(req.Worker))
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	var msg string
	switch outcome {
	case worktree.MergeApplied:
		msg = fmt.Sprintf("branche de %s mergée dans %s", req.Worker, req.FromSession)
	case worktree.MergeUpToDate:
		msg = fmt.Sprintf("%s déjà à jour dans %s", req.Worker, req.FromSession)
	case worktree.MergeConflict:
		msg = fmt.Sprintf("merge de %s refusé : conflit sur %s — résous manuellement",
			req.Worker, strings.Join(conflicts, ", "))
	}
	if !c.seedWithRetry(ctx, req.FromSession, msg) {
		return "", fmt.Errorf("merge: delivery to lead %q timed out", req.FromSession)
	}
	return req.FromSession, nil
}
```

(`strings` is used by the conflict message join; `worktree` by the outcome switch — both added in step (a).)

- [ ] **Step 4: Wire the dispatch**

In `/home/shan/dev/herrscher/core/host/turnloop.go`, inside `maybeCoordinate`, add a branch AFTER the `parseDelegate` block and BEFORE the `parseHandoff` block:

```go
	if worker, ok := parseMerge(reply); ok {
		if lead, err := d.coordinator.Merge(ctx, contracts.MergeRequest{
			FromSession: d.name, Worker: worker,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "merge refusé: " + err.Error()})
		} else {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "merge traité pour " + lead})
		}
		return
	}
```

Also update the `maybeCoordinate` doc comment ("done wins over delegate over handoff") to "done wins over delegate over merge over handoff".

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/shan/dev/herrscher && go test -race ./core/host/ -run TestMerge -v && go test -race ./core/host/`
Expected: all `TestMerge*` pass; the full `core/host` package stays green (existing handoff/delegate/report/join tests unaffected).

- [ ] **Step 6: Verify CI gates (whole workspace)**

Run: `cd /home/shan/dev/herrscher && gofmt -l core/ && go vet ./... && go build ./... && cp go.mod /tmp/gm2.bak && go mod tidy && diff go.mod /tmp/gm2.bak && echo CLEAN`
Expected: `gofmt -l` prints nothing; `go vet` silent; build succeeds; `diff` no change; `CLEAN`.

- [ ] **Step 7: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/host/coordinator.go core/host/turnloop.go core/host/coordinator_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): coordinator.Merge + maybeCoordinate dispatch (P3 join+merge)

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Cross-module note (go.work)

The local `/home/shan/dev/go.work` makes herrscher resolve `herrscher-contracts` from the local checkout, so Task 4 sees Task 1's new `MergeRequest`/`Merge` immediately — no publish/pin step between tasks. Do NOT commit go.work edits or the Neublox-extractor blank import (per [[neublox-dist-homing]] / memory: those local composition edits must never land in public herrscher). Verify with `GOWORK=off go build ./...` only if a resolution surprise appears; otherwise the default `go.work` path is correct for this workspace.

## Self-Review

- **Spec coverage:** trailer + `parseMerge` (T3); port `Merge`+`MergeRequest` (T1); guards lead/worker/child/worker-clean/lead-clean (T4); `MergeInto` real git + abort + conflict files (T2); outcome messages applied/up-to-date/conflict (T4); no join effect (T4 asserts via untouched `reported`, implicitly — method never references it); W stays alive (no Close call in Merge). All spec sections map to a task.
- **Placeholder scan:** none — every code step carries full code.
- **Type consistency:** `MergeOutcome`/`MergeApplied`/`MergeUpToDate`/`MergeConflict`, `MergeInto(leadPath, branch) (MergeOutcome, []string, error)`, `MergeRequest{FromSession, Worker}`, `Merge(ctx, MergeRequest) (string, error)` used identically across T1/T2/T4. The fake and the real `MergeInto` share the exact signature. `cleanBrancher` gains `MergeInto` so `*worktree.Worktreer` and `*fakeWTC` both satisfy it.
