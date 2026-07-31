# Memory `Unlink` Primitive Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `Unlink(from, to)` primitive to the memory port so force-restore can drop the residual `merged-into` edge left by G4.

**Architecture:** Extend `contracts.Memory` with an `Unlink` verb mirroring `Links` (identity by `(from, to)` pair). The obsidian impl excises the `[[to|rel]]` wikilink from the node body (edges are literal body text there, so removing the `Links` entry alone is a no-op round-trip). `Restore`'s Force branch calls `Unlink` best-effort; a host `memory unlink` verb exposes it manually.

**Tech Stack:** Go; three PUBLIC moat repos (`herrscher-contracts`, `herrscher-obsidian-memory`, `herrscher-orchestrator`) + host `github.com/Herrscherd/herrscher`. `go.work` overlay for local cross-dev; `GOWORK=off` for release parity.

## Global Constraints

- **Ports-only:** behaviour lives in the impl; the port gains exactly one neutral verb `Unlink(ctx, from, to)`. No `rel` parameter — identity by pair, mirroring `Links`' `to`-only dedup.
- **Learning never breaks the turn:** the force-restore `Unlink` call is best-effort — its error is discarded, never propagated.
- **Surgical:** only wikilinks targeting `to` are touched; other edges, the `## Liens` header, and human prose are byte-preserved.
- **Idempotent:** `Unlink` of an absent edge is a no-op, not an error.
- **Version floors (release, dependency order):** contracts **v0.2.12** → obsidian **v0.2.10** (go.mod → contracts v0.2.12) → orchestrator **v0.1.20** (go.mod → contracts v0.2.12) → host go.mod → all three tags.
- **SHARED-WORKTREE:** host branch `herrscher-memory-review` is harness-owned — commit on branch, never checkout/reset master. **All git tag/push/network ops are the MAIN AGENT's**; subagents commit LOCALLY only.
- **Existing serialization facts (obsidian `vault.go`):** `wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)` — group 1 is the target key, group 2 the optional rel. Managed edges render as bullet lines `- [[to|rel]]` under a `## Liens` header (`liensHeader = "## Liens"`). `marshalNode` only *appends* missing wikilinks; `unmarshalNode` re-derives `Links` by scanning the body.

---

### Task 1: contracts — add `Unlink` to the `Memory` port (v0.2.12)

**Files:**
- Modify: `memory.go` (the `Memory` interface, ~line 91; the `Links` doc-comment, ~line 100)

**Interfaces:**
- Produces: `Unlink(ctx context.Context, from, to string) error` on `contracts.Memory` — every implementation and every test fake satisfying the port must now provide it.

- [ ] **Step 1: Add the method to the interface**

In `memory.go`, inside the `Memory` interface, immediately after the `Links` method, add:

```go
	// Unlink removes the typed edge from→to — the inverse of Links. Every
	// relation targeting `to` is removed (identity is the (from, to) pair, no
	// rel, mirroring Links' to-only dedup). Idempotent: an absent edge is not
	// an error.
	Unlink(ctx context.Context, from, to string) error
```

Update the `Links` doc-comment's first line to name its inverse, e.g. append: `// Its inverse is Unlink.`

- [ ] **Step 2: Verify it compiles (no impl in this repo)**

Run: `cd /home/shan/dev/herrscher-contracts && GOWORK=off go build ./... && GOWORK=off go test ./...`
Expected: PASS (contracts is interfaces + neutral types; no fake to update here).

- [ ] **Step 3: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add memory.go
git commit -m "feat(memory): add Unlink to Memory port — inverse of Links (Unlink primitive)"
```

---

### Task 2: obsidian — implement `Unlink` with body excision (v0.2.10)

**Files:**
- Modify: `go.mod` (bump `github.com/Herrscherd/herrscher-contracts` to v0.2.12 — needed to see the new port method)
- Modify: `memory.go` (add `Unlink` method next to `Links`, ~line 288)
- Modify: `vault.go` (add an unexported body-excision helper next to `marshalNode`)
- Test: `memory_test.go` (add `TestUnlinkRemovesEdgeAndPreservesProse`)

**Interfaces:**
- Consumes: `contracts.Memory.Unlink` (Task 1); existing `m.loadUnlocked(from)`, `m.recordUnlockedNoReload(n)`, `wikilinkRe`.
- Produces: `func (m *ObsidianMemory) Unlink(ctx context.Context, from, to string) error`; `func exciseWikilinks(body, to string) string` in vault.go.

- [ ] **Step 1: Write the failing test**

In `memory_test.go`, add (adapt the constructor call to the package's existing test helper — the G7 tests use `newTestMem(t)`; match whatever the file already uses):

```go
func TestUnlinkRemovesEdgeAndPreservesProse(t *testing.T) {
	ctx := context.Background()
	m := newTestMem(t)

	// A node whose body mixes a human inline wikilink, surrounding prose, and a
	// managed bullet edge — Unlink must remove only the edges to "proj/b".
	body := "See [[proj/b|decided-in]] for context and keep [[proj/c|see-also]] too.\n\n## Liens\n- [[proj/b|merged-into]]\n"
	if err := m.Record(ctx, contracts.Node{Key: "proj/a", Kind: contracts.KindProject, Title: "A", Body: body}); err != nil {
		t.Fatal(err)
	}

	if err := m.Unlink(ctx, "proj/a", "proj/b"); err != nil {
		t.Fatal(err)
	}

	sg, err := m.Recall(ctx, "proj/a", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range sg.Root.Links {
		if l.To == "proj/b" {
			t.Fatalf("edge to proj/b still present after Unlink: %+v", sg.Root.Links)
		}
	}
	// The other edge and its surrounding prose survive.
	foundC := false
	for _, l := range sg.Root.Links {
		if l.To == "proj/c" {
			foundC = true
		}
	}
	if !foundC {
		t.Fatalf("Unlink dropped the unrelated edge to proj/c: %+v", sg.Root.Links)
	}
	if !strings.Contains(sg.Root.Body, "for context and keep") {
		t.Fatalf("Unlink mangled surrounding prose: %q", sg.Root.Body)
	}
	if strings.Contains(sg.Root.Body, "[[proj/b") {
		t.Fatalf("Unlink left a [[proj/b...]] token in the body: %q", sg.Root.Body)
	}

	// Idempotent: unlinking an absent edge is a no-op, not an error.
	if err := m.Unlink(ctx, "proj/a", "proj/b"); err != nil {
		t.Fatalf("second Unlink should be a no-op, got %v", err)
	}
}
```

Ensure `strings` is imported in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestUnlinkRemovesEdgeAndPreservesProse -v`
Expected: FAIL — `m.Unlink undefined`.

- [ ] **Step 3: Add the excision helper in vault.go**

After `marshalNode`, add:

```go
// exciseWikilinks removes every wikilink whose target key is `to` from body:
// a managed bullet line ("- [[to|rel]]" optionally with trailing spaces) is
// dropped whole; an inline "[[to|rel]]" / "[[to]]" token is removed in place,
// then a double space or space-before-punctuation left behind is collapsed.
// Only edges to `to` are touched — other wikilinks and prose are preserved.
func exciseWikilinks(body, to string) string {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		// Drop a managed bullet line that is *only* an edge to `to`.
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			if m := wikilinkRe.FindStringSubmatch(strings.TrimSpace(trimmed[2:])); m != nil && m[1] == to && wikilinkRe.ReplaceAllString(trimmed[2:], "") == "" {
				continue // pure "- [[to|rel]]" bullet — remove the whole line
			}
		}
		// Inline: remove only tokens targeting `to`, keep the rest of the line.
		line = wikilinkRe.ReplaceAllStringFunc(line, func(tok string) string {
			m := wikilinkRe.FindStringSubmatch(tok)
			if m != nil && m[1] == to {
				return ""
			}
			return tok
		})
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	// Collapse whitespace left by an inline removal (" ,"/"  " → ", "/" ").
	out = strings.ReplaceAll(out, "  ", " ")
	out = strings.ReplaceAll(out, " ,", ",")
	out = strings.ReplaceAll(out, " .", ".")
	return out
}
```

- [ ] **Step 4: Add the `Unlink` method in memory.go**

Immediately after the `Links` method, add:

```go
// Unlink removes the edge from→to (the inverse of Links): it drops every
// contracts.Link with To == to from the source node AND excises the matching
// [[to|rel]] wikilink text from its body, since the obsidian body is the source
// of truth for edges (marshalNode only appends, never removes, so dropping the
// Link alone would round-trip straight back). Idempotent: an absent edge is a
// no-op. Under the same lock as Links; a human co-owns the note.
func (m *ObsidianMemory) Unlink(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.flock(ctx)()
	n, err := m.loadUnlocked(from)
	if err != nil {
		return err
	}
	kept := n.Links[:0:0]
	found := false
	for _, l := range n.Links {
		if l.To == to {
			found = true
			continue
		}
		kept = append(kept, l)
	}
	if !found {
		return nil // no edge to `to` — no-op, no needless mtime bump
	}
	n.Links = kept
	n.Body = exciseWikilinks(n.Body, to)
	return m.recordUnlockedNoReload(n)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestUnlinkRemovesEdgeAndPreservesProse -v`
Expected: PASS.

- [ ] **Step 6: Bump contracts and run the full suite under release parity**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
GOWORK=off GOPROXY=direct GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-contracts@v0.2.12
GOWORK=off go build ./... && GOWORK=off go test ./...
```
Expected: build OK; all tests pass (64 + the new one).

- [ ] **Step 7: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go vault.go memory_test.go go.mod go.sum
git commit -m "feat(memory): implement Unlink — excise [[to|rel]] wikilink from body (contracts v0.2.12)"
```

---

### Task 3: orchestrator — wire Force-restore to `Unlink` + update fakes (v0.1.20)

**Files:**
- Modify: `go.mod` (bump contracts to v0.2.12)
- Modify: `restore.go` (Force branch of the `Restore` free function, ~line 89)
- Modify test fakes to satisfy the extended port — add an `Unlink` method to: `learner_test.go` (`recMem`, real removal), `merge_test.go` (`mergeMem`), `orchestrator_test.go` (`fakeMem`), `sweep_test.go` (`sweepFakeMem`), `learner_budget_test.go` (`budgetMem`), `restore_test.go` (`restoreMem`, real removal).
- Test: `restore_test.go` (add `TestForceRestoreDropsMergedIntoEdge`)

**Interfaces:**
- Consumes: `contracts.Memory.Unlink` (Task 1); `MetaMergedInto`; existing `recMem.links`/`hasLink`.
- Produces: Force-restore now emits `mem.Unlink(ctx, key, umbrella)` best-effort.

- [ ] **Step 1: Add `Unlink` to every test fake so the package still compiles**

`recMem` (learner_test.go) — real removal so `hasLink` reflects it:

```go
func (m *recMem) Unlink(_ context.Context, from, to string) error {
	kept := m.links[:0:0]
	for _, l := range m.links {
		if l[0] == from && l[1] == to {
			continue
		}
		kept = append(kept, l)
	}
	m.links = kept
	return nil
}
```

`restoreMem` (restore_test.go) tracks links too if used by the new test — if it currently only stubs `Links`, give it a real link store OR reuse `recMem` in the new test. Simplest: use `recMem` in the new test (Step 3) and give the remaining stub fakes a no-op `Unlink`:

```go
// add to mergeMem, fakeMem, sweepFakeMem, budgetMem, restoreMem:
func (m *mergeMem) Unlink(context.Context, string, string) error      { return nil }
func (f *fakeMem) Unlink(context.Context, string, string) error       { return nil }
func (f *sweepFakeMem) Unlink(context.Context, string, string) error  { return nil }
func (m *budgetMem) Unlink(context.Context, string, string) error     { return nil }
func (m *restoreMem) Unlink(context.Context, string, string) error    { return nil }
```

(Match each receiver name/pointer-ness to that file's existing `Links` receiver.)

- [ ] **Step 2: Verify the package compiles (tests may still fail on the new test)**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off GOPROXY=direct GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-contracts@v0.2.12 && go vet ./...`
Expected: vet passes (all fakes now satisfy the port).

- [ ] **Step 3: Write the failing regression test**

In `restore_test.go`, using `recMem` (real Unlink from Step 1):

```go
func TestForceRestoreDropsMergedIntoEdge(t *testing.T) {
	ctx := context.Background()
	mem := newRec()
	// original folded into umbrella: state archived + mergedInto meta + the edge.
	mem.nodes["facts/x"] = contracts.Node{Key: "facts/x", Meta: map[string]string{
		contracts.MetaState: contracts.StateArchived,
		MetaMergedInto:      "facts/umbrella",
	}}
	mem.links = append(mem.links, [3]string{"facts/x", "facts/umbrella", "merged-into"})

	if _, err := Restore(ctx, mem, "facts/x", Force(true)); err != nil {
		t.Fatal(err)
	}

	if mem.hasLink("facts/x", "facts/umbrella") {
		t.Fatalf("force-restore left the merged-into edge: %v", mem.links)
	}
	if mem.nodes["facts/x"].Meta[MetaMergedInto] != "" {
		t.Fatalf("force-restore left MetaMergedInto set")
	}
}
```

- [ ] **Step 4: Run to verify it fails**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestForceRestoreDropsMergedIntoEdge -v`
Expected: FAIL — the edge is still present (Restore does not yet call Unlink).

- [ ] **Step 5: Wire Unlink into the Force branch of `Restore`**

In `restore.go`, replace the Force block (currently `if cfg.force { delete(n.Meta, MetaMergedInto) }`, ~line 89) so it captures the umbrella and, after a successful `Record`, drops the edge best-effort:

```go
	umbrella := ""
	if cfg.force {
		umbrella = n.Meta[MetaMergedInto]
		delete(n.Meta, MetaMergedInto)
	}
	if err := mem.Record(ctx, n); err != nil {
		return "", err
	}
	if umbrella != "" {
		// Best-effort: the reactivation already succeeded; a failure to drop the
		// residual merged-into edge must not fail the restore (learning never
		// breaks the turn). The umbrella is additive, so a stale edge is cosmetic.
		_ = mem.Unlink(ctx, key, umbrella)
	}
	return prior, nil
```

(Remove the old standalone `delete`/`Record` lines this replaces.)

- [ ] **Step 6: Run the new test + full suite (with -race)**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestForceRestoreDropsMergedIntoEdge -v && GOWORK=off go build ./... && GOWORK=off go test -race ./...`
Expected: new test PASS; build OK; full suite green.

- [ ] **Step 7: Commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
git add restore.go restore_test.go learner_test.go merge_test.go orchestrator_test.go sweep_test.go learner_budget_test.go go.mod go.sum
git commit -m "feat(memory): force-restore drops residual merged-into edge via Unlink (contracts v0.2.12)"
```

---

### Task 4: host — `memory unlink` verb + fake + README

**Files:**
- Modify: `core/host/cli.go` (add the `memory unlink` verb after the `restore` verb, ~line 195)
- Modify: `core/host/memory_restore_verb_test.go` (add `Unlink` to `restoreVerbMem`)
- Modify: `README.md` (memory verbs section — add the `unlink` line)
- Test: `core/host/memory_restore_verb_test.go` (add a verb test if the file already tests verbs; otherwise assert via the shared fake)

**Interfaces:**
- Consumes: `contracts.Memory.Unlink` (Task 1); `BuildFirstMemory`; the `contracts.New(...).Param(...).Do(...)` verb builder pattern.

- [ ] **Step 1: Add `Unlink` to the host test fake so the package compiles**

In `core/host/memory_restore_verb_test.go`, next to `restoreVerbMem`'s `Links`:

```go
func (m *restoreVerbMem) Unlink(context.Context, string, string) error { return nil }
```

- [ ] **Step 2: Add the verb in cli.go**

After the `memory restore` verb registration block (before `memory search`), add:

```go
	if err := reg.Add(contracts.New("memory", "unlink").
		Help("remove the edge from→to between two memory nodes").
		Param("from", "source node key", true).
		Param("to", "target node key", true).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			if err := mem.Unlink(cmdCtx, in.Get("from"), in.Get("to")); err != nil {
				return "", err
			}
			return "unlinked " + in.Get("from") + " -> " + in.Get("to"), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
```

- [ ] **Step 3: Add the README line**

In `README.md`, in the memory verbs list (near `memory restore` / `memory search`), add:

```markdown
- `memory unlink --from K1 --to K2` — remove the edge K1→K2 (inverse of an auto-created link; used to detach a force-restored node from its old umbrella).
```

- [ ] **Step 4: Bump go.mod to the three tags and run the full host suite (release parity)**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review
GOWORK=off GOPROXY=direct GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-contracts@v0.2.12
GOWORK=off GOPROXY=direct GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-obsidian-memory@v0.2.10
GOWORK=off GOPROXY=direct GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-orchestrator@v0.1.20
GOWORK=off go build ./... && GOWORK=off go test ./...
```
Expected: build OK; full host suite green. **NOTE: this step depends on the Task 5 tags existing — the MAIN AGENT publishes them first (see Task 5). Until then, run against the go.work overlay: `go build ./... && go test ./core/host/...`.**

- [ ] **Step 5: Commit on the branch**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review
git add core/host/cli.go core/host/memory_restore_verb_test.go README.md go.mod go.sum
git commit -m "feat(memory): memory unlink verb + README (Unlink primitive)"
```

---

### Task 5: Release (MAIN AGENT ONLY)

**Files:** none (tags + pushes only). Subagents MUST NOT run this task.

- [ ] **Step 1: Publish tags in dependency order** (each moat go.mod already bumped + committed in its task):
  - contracts `v0.2.12` @ its Task 1 commit; push branch + tag.
  - obsidian `v0.2.10` @ its Task 2 commit; push branch + tag.
  - orchestrator `v0.1.20` @ its Task 3 commit; push branch + tag.
- [ ] **Step 2:** With tags live, re-run host Task 4 Step 4 (`GOWORK=off` `go get` of all three + build + full suite). Confirm green.
- [ ] **Step 3:** Push the host branch `herrscher-memory-review`; PR #46 CI re-triggers.
- [ ] **Step 4:** Update the memory note `herrscher-memory-vs-hermes.md` — mark the G4 deferred "force-restore leaves stale merged-into EDGE" debt as RESOLVED (contracts v0.2.12 / obsidian v0.2.10 / orchestrator v0.1.20). Update the local `.superpowers/sdd/progress.md` ledger.

---

## Self-Review

- **Spec coverage:** Volet 1 → Task 1; Volet 2 → Task 2; Volet 3 → Task 3; Volet 4 → Task 4; versions/release → Task 5. All four invariants are covered (ports-only T1; best-effort T3 Step 5; surgical T2 test; idempotent T2 Step 1 + T3 no-op fakes). ✔
- **Placeholder scan:** every code step shows real code; test bodies are complete; commands have expected output. ✔
- **Type consistency:** `Unlink(ctx, from, to string) error` is identical across T1 (port), T2 (obsidian receiver), T3 (fakes + `mem.Unlink(ctx, key, umbrella)`), T4 (`mem.Unlink(cmdCtx, from, to)`). `exciseWikilinks(body, to string) string` used only in T2. ✔
- **Open risk noted:** the whitespace collapse in `exciseWikilinks` is heuristic; the test asserts prose survival for the representative inline case. If a reviewer finds an edge case (e.g. link at line start), tighten the collapse rules rather than widening scope.
