# G7 — Raw-session archival tier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a full-text-searchable raw per-turn transcript tier to herrscher's memory, invisible to every existing recall/sweep/merge/promote path and surfaced only via an explicit `memory search --raw`.

**Architecture:** Extend `contracts.Query` with one opt-in `IncludeRaw` bool + a new `KindTranscript` node kind (shaped exactly on the existing `IncludeArchived`/`StateArchived` precedent). The obsidian vault hides `KindTranscript` nodes unless `IncludeRaw`, and stores them untruncated (per-node budget bypass). The orchestrator's `Learner.Observe` writes one verbatim raw node per turn, best-effort, behind an opt-in `raw-archive` toggle. The host exposes a `memory search` CLI verb.

**Tech Stack:** Go, three public moat modules (`herrscher-contracts`, `herrscher-obsidian-memory`, `herrscher-orchestrator`) + the host repo.

## Global Constraints

- **Minimal contracts surface:** add exactly one `NodeKind` (`KindTranscript = "transcript"`) and one `Query` field (`IncludeRaw bool`). No new port, no new method. `Search` still returns `[]contracts.Node`.
- **Invisible by default:** every existing caller uses a zero-value `Query` (`IncludeRaw == false`), so raw nodes are hidden from ordinary Search/Recall AND from the curator sweep/merge/promote (which query with `IncludeArchived: true` but never set `IncludeRaw`). No regression to distilled recall is permitted.
- **Learning never breaks a turn:** the raw `Record` in `Observe` is best-effort — its error is discarded (`_ =`) — and gated behind the opt-in `raw-archive` toggle (default OFF).
- **Append-only & reversible:** raw nodes are never truncated (budget bypass), never mutated in place (monotonic `raw/<session>/<seq>` keys), never swept/merged/promoted. Nothing in G7 deletes or overwrites.
- **Versions:** contracts v0.2.9 → **v0.2.10**; obsidian v0.2.7 → **v0.2.8**; orchestrator v0.1.16 → **v0.1.17**; host dep bumps to match.
- **Repo rules:** moat repos (contracts/obsidian/orchestrator) commit on `master`; host commits on branch `herrscher-memory-review`. **Main agent owns ALL git tag/push/network ops** — task implementers commit LOCALLY only. `go.work` overlay (gitignored) is active for cross-module dev; release parity is verified with `GOWORK=off` against published tags.
- **Config triple idiom:** a new setting is `{Key, Env, Help}` in `register.go`'s manifest + a `cfg.Get` read + a setter call in the Learner branch, mirroring the existing `idle-days`/`promote-min-age-days` entries.

---

### Task 1: contracts — `KindTranscript` + `Query.IncludeRaw`

**Repo:** `/home/shan/dev/herrscher-contracts` (module `github.com/Herrscherd/herrscher-contracts`), on `master`.

**Files:**
- Modify: `memory.go` (NodeKind const block ~L10-28; Query struct ~L51-65)
- Test: `memory_test.go` (extend `TestNodeKindConstants` ~L39)

**Interfaces:**
- Produces: `contracts.KindTranscript NodeKind = "transcript"`; `contracts.Query` field `IncludeRaw bool` (zero value false).

- [ ] **Step 1: Write the failing test.** In `memory_test.go`, add a dedicated test asserting the new constant value and that `IncludeRaw` defaults false:

```go
func TestKindTranscriptAndIncludeRaw(t *testing.T) {
	if KindTranscript != "transcript" {
		t.Fatalf("KindTranscript = %q, want %q", KindTranscript, "transcript")
	}
	var q Query // zero value
	if q.IncludeRaw {
		t.Fatal("Query.IncludeRaw must default to false so the raw tier is hidden from existing callers")
	}
}
```

- [ ] **Step 2: Run it, verify it fails to compile.**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestKindTranscriptAndIncludeRaw`
Expected: FAIL — `undefined: KindTranscript` and `q.IncludeRaw undefined`.

- [ ] **Step 3: Add the constant.** In `memory.go`, inside the `const (...)` block, after the `KindDomain` block (currently ends ~L27), add:

```go
	// KindTranscript is one raw per-turn transcript chunk — the append-only,
	// full-text-searchable archival tier (G7). It is HIDDEN from ordinary
	// Search/Recall (and from the curator sweep/merge/promote) unless a caller
	// sets Query.IncludeRaw, mirroring how StateArchived nodes hide behind
	// IncludeArchived. Raw chunks are never distilled, swept, merged, or promoted.
	KindTranscript NodeKind = "transcript"
```

- [ ] **Step 4: Add the Query field.** In the `Query` struct, after the `IncludeArchived bool` field (currently ends ~L64), add:

```go
	// IncludeRaw includes KindTranscript nodes (the G7 raw archival tier) in the
	// result. Default false: raw chunks are hidden from ordinary Search/Recall and
	// from the curator's sweep/merge/promote passes, so the distilled memory is
	// unaffected. Only an explicit raw search (memory search --raw) sets it true.
	IncludeRaw bool
```

- [ ] **Step 5: Run the test, verify it passes.**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: PASS (all packages).

- [ ] **Step 6: Commit.**

```bash
cd /home/shan/dev/herrscher-contracts
git add memory.go memory_test.go
git commit -m "feat(memory): KindTranscript node kind + Query.IncludeRaw (G7 raw tier surface)"
```

---

### Task 2: obsidian — hide `KindTranscript` unless `IncludeRaw`

**Repo:** `/home/shan/dev/herrscher-obsidian-memory` (module `github.com/Herrscherd/herrscher-obsidian-memory`), on `master`. Requires contracts v0.2.10 locally (go.work overlay resolves it; if not overlaid, `GOWORK=off GOPROXY=direct GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-contracts@<local>` is a release-time concern — for dev the overlay makes the new symbols visible).

**Files:**
- Modify: `memory.go` — `matchesQuery` (currently starts L352)
- Test: `memory_test.go` (or the existing search test file — place beside other `matchesQuery`/`Search` tests)

**Interfaces:**
- Consumes: `contracts.KindTranscript`, `contracts.Query.IncludeRaw` (Task 1).
- Produces: `Search`/`matchesQuery` semantics — `KindTranscript` nodes are excluded unless `q.IncludeRaw`.

- [ ] **Step 1: Write the failing test.** Record a transcript node and one ordinary node, then assert visibility. Use the package's existing test-harness pattern for constructing an `*ObsidianMemory` over a temp vault (copy the setup from a neighbouring `Search` test in the same file — do NOT invent a new constructor).

```go
func TestSearchHidesTranscriptUnlessIncludeRaw(t *testing.T) {
	m := newTestMemory(t) // reuse the existing test helper; adapt name to the real one
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "projects/p/fact", Kind: contracts.KindProject, Title: "widget throughput", Body: "the widget runs at 5rps"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Record(ctx, contracts.Node{Key: "raw/s/1", Kind: contracts.KindTranscript, Title: "turn 1", Body: "user: how fast is the widget → assistant: 5rps"}); err != nil {
		t.Fatal(err)
	}

	// Default query: raw chunk hidden.
	got, err := m.Search(ctx, contracts.Query{Text: "widget"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if n.Kind == contracts.KindTranscript {
			t.Fatalf("default Search returned a raw transcript node %q; must be hidden", n.Key)
		}
	}

	// Sweep-shaped query (IncludeArchived true, IncludeRaw unset): still hidden.
	got, _ = m.Search(ctx, contracts.Query{Text: "widget", IncludeArchived: true})
	for _, n := range got {
		if n.Kind == contracts.KindTranscript {
			t.Fatalf("IncludeArchived Search leaked a raw node %q; sweep/merge/promote must not see raw", n.Key)
		}
	}

	// Opt-in: raw chunk visible.
	got, err = m.Search(ctx, contracts.Query{Text: "widget", IncludeRaw: true})
	if err != nil {
		t.Fatal(err)
	}
	var sawRaw bool
	for _, n := range got {
		if n.Key == "raw/s/1" {
			sawRaw = true
		}
	}
	if !sawRaw {
		t.Fatal("IncludeRaw Search did not return the raw transcript node")
	}
}
```

- [ ] **Step 2: Run it, verify it fails.**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestSearchHidesTranscriptUnlessIncludeRaw`
Expected: FAIL — the default/IncludeArchived searches return the raw node (rule not yet added).

- [ ] **Step 3: Add the hide rule.** In `memory.go`, at the top of `matchesQuery` (currently L352), add the raw gate immediately after the archived gate (L353-355), so both live together:

```go
func matchesQuery(n contracts.Node, q contracts.Query) bool {
	if n.Meta[contracts.MetaState] == contracts.StateArchived && !q.IncludeArchived {
		return false
	}
	if n.Kind == contracts.KindTranscript && !q.IncludeRaw {
		return false // G7 raw archival tier: hidden unless the caller opts in
	}
	// ... rest unchanged (Kinds/Text/Tags filters)
```

- [ ] **Step 4: Run the test, verify it passes.**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go memory_test.go
git commit -m "feat(memory): hide KindTranscript from Search unless Query.IncludeRaw (G7)"
```

---

### Task 3: obsidian — bypass the per-node budget for `KindTranscript`

**Repo:** `/home/shan/dev/herrscher-obsidian-memory`, on `master`.

**Files:**
- Modify: `memory.go` — `recordUnlocked` (currently L128-135)
- Test: `memory_test.go` (beside the existing budget test — search for `BudgetError`/`SetNodeBudget` and place adjacent)

**Interfaces:**
- Consumes: `contracts.KindTranscript` (Task 1).
- Produces: `Record` never returns `*contracts.BudgetError` for a `KindTranscript` node, regardless of `SetNodeBudget`.

- [ ] **Step 1: Write the failing test.**

```go
func TestRecordTranscriptBypassesBudget(t *testing.T) {
	m := newTestMemory(t) // reuse the existing helper
	m.SetNodeBudget(10)   // 10-rune cap
	ctx := context.Background()
	big := strings.Repeat("x", 500)

	// A distilled node over budget is rejected (existing behaviour, regression-lock).
	err := m.Record(ctx, contracts.Node{Key: "projects/p/fact", Kind: contracts.KindProject, Body: big})
	if _, ok := err.(*contracts.BudgetError); !ok {
		t.Fatalf("distilled over-budget Record: got %v, want *BudgetError", err)
	}

	// A raw transcript node over budget is stored untruncated.
	if err := m.Record(ctx, contracts.Node{Key: "raw/s/1", Kind: contracts.KindTranscript, Body: big}); err != nil {
		t.Fatalf("raw over-budget Record must succeed, got %v", err)
	}
	got, err := m.Search(ctx, contracts.Query{Text: "xxx", IncludeRaw: true})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range got {
		if n.Key == "raw/s/1" {
			found = true
			if utf8.RuneCountInString(n.Body) != 500 {
				t.Fatalf("raw body truncated to %d runes; archival must preserve all 500", utf8.RuneCountInString(n.Body))
			}
		}
	}
	if !found {
		t.Fatal("raw node not found after Record")
	}
}
```

(Ensure the test file imports `unicode/utf8` and `strings`.)

- [ ] **Step 2: Run it, verify it fails.**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestRecordTranscriptBypassesBudget`
Expected: FAIL — the raw Record returns `*BudgetError`.

- [ ] **Step 3: Add the bypass.** In `recordUnlocked` (L128), guard the budget check on kind:

```go
func (m *ObsidianMemory) recordUnlocked(n contracts.Node) error {
	// KindTranscript is the append-only raw archival tier (G7): it must be stored
	// verbatim, so the per-node Body budget never applies to it. Every distilled
	// kind is still subject to the budget.
	if m.budget > 0 && n.Kind != contracts.KindTranscript {
		if r := utf8.RuneCountInString(n.Body); r > m.budget {
			return &contracts.BudgetError{Key: n.Key, Runes: r, Limit: m.budget}
		}
	}
	return m.writeNode(n, true)
}
```

- [ ] **Step 4: Run the test, verify it passes.**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go memory_test.go
git commit -m "feat(memory): bypass per-node budget for KindTranscript (G7 untruncated archival)"
```

---

### Task 4: orchestrator — write a raw per-turn node in `Learner.Observe` + `raw-archive` config

**Repo:** `/home/shan/dev/herrscher-orchestrator` (module `github.com/Herrscherd/herrscher-orchestrator`), on `master`. Requires contracts v0.2.10 locally (overlay).

**Files:**
- Modify: `learner.go` — struct fields (~L106-125), add `SetRawArchive`, extend `Observe` (L232-246)
- Modify: `register.go` — manifest Setting (after L26) + wiring in the Learner branch (after L63)
- Test: `idle_test.go` or a new `raw_test.go` (orchestrator convention: focused `_test.go` per feature — a new `raw_test.go` is cleanest); `register_test.go` for the manifest test.

**Interfaces:**
- Consumes: `contracts.KindTranscript` (Task 1); the embedded `*Curator` fields `session` (`"sessions/"+name`) and `now`; the package helper — NONE reused for the body (raw body is built verbatim, deliberately NOT via `turnLine`, which truncates).
- Produces: `(*Learner).SetRawArchive(bool)`; when enabled, each `Observe` best-effort records a `KindTranscript` node keyed `raw/<sessionTail>/<seq>` with `seq` a per-Learner monotonic counter.

- [ ] **Step 1: Write the failing tests.** Create `raw_test.go`. Use the existing test-Extractor/`mergeMem` helpers already in the package (inspect `idle_test.go`/`learner_test.go` for the real fake-memory type — adapt names). The fake memory must record nodes so the test can read them back.

```go
func TestObserveRecordsRawTurnWhenEnabled(t *testing.T) {
	mem := &mergeMem{nodes: map[string]contracts.Node{}} // adapt to the real fake
	l := NewLearner(mem, "sess-1", contracts.MemoryScope{}, nil, "", 0)
	l.SetRawArchive(true)
	ctx := context.Background()

	if err := l.Observe(ctx, contracts.Prompt{Author: "alice", Content: "how do I deploy?"}, "run make ship"); err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(ctx, contracts.Prompt{Author: "alice", Content: "and rollback?"}, "make unship"); err != nil {
		t.Fatal(err)
	}

	first, ok := mem.nodes["raw/sess-1/1"]
	if !ok {
		t.Fatalf("first raw node raw/sess-1/1 not recorded; have %v", keysOf(mem.nodes))
	}
	if first.Kind != contracts.KindTranscript {
		t.Fatalf("raw node kind = %q, want KindTranscript", first.Kind)
	}
	if !strings.Contains(first.Body, "how do I deploy?") || !strings.Contains(first.Body, "run make ship") {
		t.Fatalf("raw body must contain the verbatim prompt and reply, got %q", first.Body)
	}
	if _, ok := mem.nodes["raw/sess-1/2"]; !ok {
		t.Fatal("second raw node raw/sess-1/2 not recorded; seq must advance")
	}
}

func TestObserveSkipsRawWhenDisabled(t *testing.T) {
	mem := &mergeMem{nodes: map[string]contracts.Node{}}
	l := NewLearner(mem, "sess-1", contracts.MemoryScope{}, nil, "", 0)
	// SetRawArchive not called → default off.
	ctx := context.Background()
	if err := l.Observe(ctx, contracts.Prompt{Author: "a", Content: "x"}, "y"); err != nil {
		t.Fatal(err)
	}
	for k, n := range mem.nodes {
		if n.Kind == contracts.KindTranscript {
			t.Fatalf("raw node %q written with raw-archive off; must be no-op", k)
		}
	}
}
```

(If the package has no `keysOf` helper, drop that arg — it is only for the failure message. Match the real fake-memory field names; if the fake stores via a slice, adapt the lookups.)

- [ ] **Step 2: Run them, verify they fail.**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run 'TestObserveRecordsRawTurnWhenEnabled|TestObserveSkipsRawWhenDisabled'`
Expected: FAIL — `l.SetRawArchive undefined`.

- [ ] **Step 3: Add the fields + setter.** In `learner.go`, add to the `Learner` struct (near the idle fields ~L106-124, keep the stampMu-guarded counters together):

```go
	// rawArchive enables the G7 raw-session archival tier: when true, every
	// Observe best-effort records one verbatim KindTranscript node per turn. Off
	// by default (append-heavy); set via SetRawArchive. rawSeq is the per-Learner
	// monotonic turn counter for the raw node key; it advances under stampMu
	// alongside n, never on the consolidation mu.
	rawArchive bool
	rawSeq     int
```

Add the setter (near `SetIdle` ~L138):

```go
// SetRawArchive toggles the G7 raw-session archival tier. When true, Observe
// records one untruncated KindTranscript node per turn (best-effort, never
// blocking the turn). Off by default. The host wires this from MEMORY_RAW_ARCHIVE.
func (l *Learner) SetRawArchive(on bool) {
	l.rawArchive = on
}
```

- [ ] **Step 4: Extend `Observe`.** Replace the body of `Observe` (L232-246) so it captures the raw sequence under stampMu and records off-lock:

```go
func (l *Learner) Observe(ctx context.Context, p contracts.Prompt, reply string) error {
	err := l.Curator.Observe(ctx, p, reply)
	l.stampMu.Lock()
	l.lastActivity = l.now()
	var due bool
	if l.every > 0 {
		l.n++
		due = l.n%l.every == 0
	}
	var rawSeq int
	if l.rawArchive {
		l.rawSeq++
		rawSeq = l.rawSeq
	}
	l.stampMu.Unlock()
	if rawSeq > 0 {
		l.recordRaw(ctx, p, reply, rawSeq) // best-effort; never breaks the turn
	}
	if due {
		_ = l.Consolidate(ctx)
	}
	return err
}

// recordRaw writes one verbatim raw-transcript node for the turn (G7). It is
// best-effort: any error is discarded so archival never breaks the turn loop
// (invariant 2). The Body is the full untruncated prompt+reply — the obsidian
// per-node budget is bypassed for KindTranscript, so nothing is lost. Keyed
// raw/<sessionTail>/<seq> (append-only, monotonic; never re-recorded).
func (l *Learner) recordRaw(ctx context.Context, p contracts.Prompt, reply string, seq int) {
	if l.mem == nil {
		return
	}
	tail := strings.TrimPrefix(l.session, "sessions/")
	_ = l.mem.Record(ctx, contracts.Node{
		Key:   fmt.Sprintf("raw/%s/%d", tail, seq),
		Kind:  contracts.KindTranscript,
		Title: fmt.Sprintf("turn %d", seq),
		Body:  fmt.Sprintf("%s: %s\n\nassistant: %s", p.Author, p.Content, reply),
	})
}
```

Ensure `learner.go` imports `fmt` and `strings` (add if absent — check the import block).

- [ ] **Step 5: Run the tests, verify they pass.**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run 'TestObserveRecordsRawTurnWhenEnabled|TestObserveSkipsRawWhenDisabled'`
Expected: PASS.

- [ ] **Step 6: Wire the config triple.** In `register.go`, add the manifest Setting after the `idle-hours` entry (L26):

```go
					{Key: "raw-archive", Env: "MEMORY_RAW_ARCHIVE", Help: "when true (1/on), the learner archives one untruncated raw transcript node per turn (G7 full-text tier), surfaced only by memory search --raw; default off (append-heavy)", Required: false},
```

And in the Learner branch, after the `l.SetIdle(idleDays, idleHours)` line (L63):

```go
				rawArchive := isTruthy(cfg.Get("raw-archive"))
				l.SetRawArchive(rawArchive)
```

Add a small helper near the bottom of `register.go` (or reuse an existing truthy parser if one exists — grep for how `report-enabled` parses; it inlines `!= "false" && != "0" && != "off"`. For an opt-IN flag, define the positive form):

```go
// isTruthy reports whether an opt-in config value is enabled. Empty/unset is off.
func isTruthy(v string) bool {
	return v == "true" || v == "1" || v == "on"
}
```

- [ ] **Step 7: Write the manifest test.** In `register_test.go`, mirroring the existing `TestManifestHasIdleSettings`:

```go
func TestManifestHasRawArchiveSetting(t *testing.T) {
	var found *contracts.Setting
	for _, o := range contracts.Default.Orchestrators() {
		for i := range o.Manifest.Config {
			if o.Manifest.Config[i].Key == "raw-archive" {
				found = &o.Manifest.Config[i]
			}
		}
	}
	if found == nil {
		t.Fatal("raw-archive setting missing from orchestrator manifest")
	}
	if found.Env != "MEMORY_RAW_ARCHIVE" {
		t.Fatalf("raw-archive Env = %q, want MEMORY_RAW_ARCHIVE", found.Env)
	}
}
```

(Match the exact accessor the existing idle test uses — if it iterates a different registry accessor, copy that shape verbatim.)

- [ ] **Step 8: Run the full suite with -race.**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test -race ./...`
Expected: PASS, no data race (the raw counter advances under stampMu, same discipline as `n`).

- [ ] **Step 9: Commit.**

```bash
cd /home/shan/dev/herrscher-orchestrator
git add learner.go register.go raw_test.go register_test.go
git commit -m "feat(memory): raw-session archival tier — best-effort per-turn KindTranscript record + raw-archive config (G7)"
```

---

### Task 5: host — `memory search` verb + README

**Repo:** host worktree `/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review`, on branch `herrscher-memory-review`. Builds against the local moat via the go.work overlay.

**Files:**
- Modify: `core/host/cli.go` — add a verb after the `memory restore` block (L176-193)
- Test: the host test file covering the memory verbs (grep for `"restore"` in `core/host/*_test.go` and place the new test beside it; if none exists, create `core/host/memory_search_test.go`)
- Modify: `README.md` — add a G7 paragraph in the Learning/Staleness section

**Interfaces:**
- Consumes: `BuildFirstMemory(cmdCtx) (contracts.Memory, error)`; `contracts.Query{Text, IncludeRaw, Ranked, Limit}`; `mem.Search(ctx, q) ([]contracts.Node, error)`; `contracts.Input.Get`/`Bool`.
- Produces: CLI verb `memory search --text <t> [--raw] [--limit N]`.

- [ ] **Step 1: Write the failing test.** Register the CLI (reuse whatever constructor the existing `memory restore` test uses to build the registry over a temp vault) and drive `memory search`. If the host memory tests seed a memory first, seed a raw node via the orchestrator/`BuildFirstMemory` path the same way. Minimal shape:

```go
func TestMemorySearchVerbRegistered(t *testing.T) {
	reg := buildTestRegistry(t) // reuse the existing helper used by the restore test
	cmd, ok := reg.Lookup("memory", "search")
	if !ok {
		t.Fatal("memory search verb not registered")
	}
	// The verb must accept text (required), raw, limit params.
	if !cmd.HasParam("text") || !cmd.HasParam("raw") || !cmd.HasParam("limit") {
		t.Fatal("memory search missing expected params")
	}
}
```

(Adapt `reg.Lookup`/`cmd.HasParam` to the real registry API — inspect how existing verb tests introspect the registry; if they instead execute the verb, follow that pattern and assert on output text instead.)

- [ ] **Step 2: Run it, verify it fails.**

Run: `cd <host> && go test ./core/host/ -run TestMemorySearchVerbRegistered`
Expected: FAIL — verb not registered.

- [ ] **Step 3: Add the verb.** In `core/host/cli.go`, after the `memory restore` block (closes L193), add:

```go
	if err := reg.Add(contracts.New("memory", "search").
		Help("full-text search the memory vault; --raw also searches the raw per-turn transcript tier (G7)").
		Param("text", "query text", true).
		Param("raw", "include raw per-turn transcript chunks (G7 archival tier)", false).
		Param("limit", "max hits (default 10)", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			mem, err := BuildFirstMemory(cmdCtx)
			if err != nil {
				return "", err
			}
			defer mem.Close()
			limit := 10
			if v, err := strconv.Atoi(in.Get("limit")); err == nil && v > 0 {
				limit = v
			}
			hits, err := mem.Search(cmdCtx, contracts.Query{
				Text:       in.Get("text"),
				Ranked:     true,
				Limit:      limit,
				IncludeRaw: in.Bool("raw"),
			})
			if err != nil {
				return "", err
			}
			if len(hits) == 0 {
				return "no matches", nil
			}
			var b strings.Builder
			for _, n := range hits {
				fmt.Fprintf(&b, "%s\t[%s]\t%s\n", n.Key, n.Kind, n.Title)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})); err != nil {
		return nil, hostDeps{}, err
	}
```

Ensure `core/host/cli.go` imports `strconv`, `strings`, `fmt` (check the import block — `fmt` is already used at L161; add `strconv`/`strings` if absent).

- [ ] **Step 4: Run the test, verify it passes.**

Run: `cd <host> && go test ./core/host/`
Expected: PASS.

- [ ] **Step 5: Update the README.** In `README.md`, in the Learning/Staleness section (after the "Inactivity-triggered curator" bullet added by G5), add:

```markdown
- **Raw-session archival tier (G7).** With `MEMORY_RAW_ARCHIVE` enabled, the
  learner archives every turn as an untruncated raw node (`KindTranscript`,
  keyed `raw/<session>/<seq>`) — a full-text-searchable transcript store. Raw
  nodes are hidden from ordinary recall and from the curator's sweep/merge/
  promote passes (they surface only via `Query.IncludeRaw`), so the distilled
  memory is unaffected. Retrieve them with `memory search --text "…" --raw`.
  The write is best-effort (never breaks a turn) and off by default.
```

- [ ] **Step 6: Run the full host suite.**

Run: `cd <host> && go test ./...`
Expected: PASS (all packages green against the overlay).

- [ ] **Step 7: Commit (local; main agent pushes at release).**

```bash
cd <host>
git add core/host/cli.go core/host/*_test.go README.md
git commit -m "feat(memory): memory search verb (--raw for G7 transcript tier) + README"
```

---

### Task 6: Release (MAIN AGENT ONLY — tags, pushes, PR)

**This task is executed by the main agent, not a subagent** — it owns all git tag/push/network ops.

- [ ] **Step 1: Tag + push the moat modules in dependency order.**

```bash
cd /home/shan/dev/herrscher-contracts && git push origin master && git tag v0.2.10 && git push origin v0.2.10
cd /home/shan/dev/herrscher-obsidian-memory && git push origin master && git tag v0.2.8 && git push origin v0.2.8
cd /home/shan/dev/herrscher-orchestrator && git push origin master && git tag v0.1.17 && git push origin v0.1.17
```

- [ ] **Step 2: Bump host go.mod against the published tags** (GOWORK=off for release parity).

```bash
cd <host>
GOWORK=off GOPROXY=direct GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-contracts@v0.2.10
GOWORK=off GOPROXY=direct GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-obsidian-memory@v0.2.8
GOWORK=off GOPROXY=direct GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-orchestrator@v0.1.17
GOWORK=off go mod tidy
```

- [ ] **Step 3: Verify the host builds + tests against published tags (no overlay).**

Run: `cd <host> && GOWORK=off go build ./... && GOWORK=off go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit the go.mod bump and push the branch.**

```bash
cd <host>
git add go.mod go.sum
git commit -m "chore(deps): contracts v0.2.10 + obsidian v0.2.8 + orchestrator v0.1.17 (G7 raw tier)"
git push origin herrscher-memory-review
```

- [ ] **Step 5: Confirm PR #46 CI is green** (poll `gh run list`/`gh run view` until completed; the whole-branch review + finishing-a-development-branch follow).

---

## Self-Review

**1. Spec coverage:**
- Engine = obsidian vault → Tasks 2/3/5 (Search reuse, budget bypass, verb). ✅
- Contracts surface = extend Query → Task 1 (`IncludeRaw` + `KindTranscript`). ✅
- Granularity = per-turn chunks → Task 4 (`recordRaw` one node per Observe). ✅
- Invariant "invisible by default" → Task 2 (matchesQuery gate, incl. the IncludeArchived-shaped sweep query test). ✅
- Invariant "never truncated" → Task 3 (budget bypass + 500-rune assertion). ✅
- Invariant "learning never breaks a turn" → Task 4 (`_ =` discard, off-lock, opt-in). ✅
- Invariant "append-only, not swept/merged/promoted" → structural (default Query) + Task 2's IncludeArchived test proves sweep-shaped queries hide raw. ✅
- `memory search --raw` verb + acceptance → Task 5. ✅
- Config toggle default off → Task 4 (Step 7 test `TestObserveSkipsRawWhenDisabled` + manifest test). ✅

**2. Placeholder scan:** No TBD/TODO/"handle edge cases". Every code step carries full code. The only adaptive instructions are "reuse the existing test helper / match the real registry API" — deliberate, because the fake-memory type and registry-introspection API are pre-existing and must not be reinvented; the implementer is told exactly which existing test to copy from.

**3. Type consistency:** `KindTranscript`/`IncludeRaw` defined in Task 1, consumed identically in 2/3/4/5. `recordRaw` key scheme `raw/<sessionTail>/<seq>` matches the Task 2/3 test keys (`raw/s/1`, `raw/sess-1/1`) in shape. `SetRawArchive(bool)` defined and called consistently. Body format `"<author>: <content>\n\nassistant: <reply>"` matches the Task 4 test assertions (`Contains "how do I deploy?"` and `"run make ship"`).
