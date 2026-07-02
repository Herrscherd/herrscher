# Memory A — Recall Ranking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Return the most relevant memory nodes for a turn, ranked (lexical tf + title + recency + kind + graph-proximity), so primed context is small, ordered, and on-topic.

**Architecture:** Scoring is centralized in `herrscher-contracts` (a passive, dependency-free `scoring.go`) so both the obsidian `Search` impl and the contracts-level `RecallRelevant` free function share one ranking. `Query.Ranked` is an additive flag defaulting to today's behaviour. Recency is fed by `Meta["capturedAt"]`, which `obsidian.Record` now auto-stamps (RFC3339 UTC, preserved on upsert). No embeddings, no new deps, no markdown-format change beyond the one auto-stamped field.

**Tech Stack:** Go 1.25, two modules — `github.com/Herrscherd/herrscher-contracts` (currently v0.1.9) and `github.com/Herrscherd/herrscher-obsidian-memory` (currently v0.2.0). Both siblings resolve via the local `go.work`, so obsidian can build/test against the un-released contracts changes during implementation. Table-driven tests, white-box (`package contracts` / `package obsidian`).

**Decisions locked (from brainstorming):**
- Scoring lives in `contracts` (RecallRelevant is in contracts and can't import obsidian — cycle).
- Recency: `obsidian.Record` stamps `Meta["capturedAt"]` when absent, preserved on upsert; hand-authored files without it score neutral recency.
- Versions: contracts **v0.1.10**, obsidian **v0.2.1** (additive/patch, no breaking change).

**Release order (all user-gated):** contracts v0.1.10 → obsidian v0.2.1 (bump its contracts require first) → host dependency bump via branch+PR. Implement and test ALL code in the workspace first; release only after the full gate is green in both repos.

---

## File Structure

**herrscher-contracts** (`/home/shan/dev/herrscher-contracts`):
- Modify `memory.go` — add `Ranked bool` to `Query`.
- Create `scoring.go` — `tokenize`, weight consts, `ranker`, `score`, `proximityBoost`, `kindBoost`. The passive ranking core. No I/O, no new imports beyond `math`, `strings`, `time`, `sort`.
- Modify `memory_scope.go` — add `RecallRelevant` + unexported `rankNodes`/BFS-proximity helper beside `RecallScoped`.
- Create `scoring_test.go` — tokenizer + `score` ordering tests.
- Modify `memory_scope_test.go` (or create if absent) — `RecallRelevant` tests.

**herrscher-obsidian-memory** (`/home/shan/dev/herrscher-obsidian-memory`):
- Modify `memory.go` — add `now func() time.Time` field, set in `New`; stamp `capturedAt` in `recordUnlocked`; score+sort in `Search` when `q.Ranked`.
- Modify `memory_test.go` — capturedAt stamping/upsert tests, ranked-Search tests, `Ranked:false` unchanged test.

**herrscher** (host): `go.mod`/`go.sum` bump only (Task 8).

---

### Task 1: `Query.Ranked` field (contracts)

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/memory.go:51-56`
- Test: `/home/shan/dev/herrscher-contracts/scoring_test.go` (created in Task 2 covers the field indirectly; no dedicated test needed — it's a plain field)

- [ ] **Step 1: Add the field**

In `memory.go`, extend `Query`:

```go
type Query struct {
	Text  string
	Kinds []NodeKind
	Tags  []string
	Limit int // 0 = no limit
	// Ranked, when true, asks the Memory to return results score-sorted by
	// relevance to Text (highest first) instead of storage/walk order. Zero
	// value (false) preserves the historical unranked behaviour, so existing
	// callers are unaffected.
	Ranked bool
}
```

- [ ] **Step 2: Build**

Run: `cd /home/shan/dev/herrscher-contracts && go build ./...`
Expected: success (additive field).

- [ ] **Step 3: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add memory.go && git commit -m "feat(query): add optional Ranked flag (default false = walk order)"
```

---

### Task 2: Scoring core — `scoring.go` (contracts)

**Files:**
- Create: `/home/shan/dev/herrscher-contracts/scoring.go`
- Test: `/home/shan/dev/herrscher-contracts/scoring_test.go`

- [ ] **Step 1: Write the failing tests**

Create `scoring_test.go`:

```go
package contracts

import (
	"testing"
	"time"
)

func TestTokenize_LowercasesAndSplitsOnNonAlnum(t *testing.T) {
	got := tokenize("NATS, transport-layer!")
	want := []string{"nats", "transport", "layer"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestScore_TwoTermBeatsOneTerm(t *testing.T) {
	r := newRanker("nats transport", time.Time{})
	both := Node{Title: "x", Body: "we use nats for transport"}
	one := Node{Title: "x", Body: "we use nats only"}
	sBoth, hitBoth := r.score(both)
	sOne, _ := r.score(one)
	if !hitBoth || sBoth <= sOne {
		t.Fatalf("two-term (%.3f) must outrank one-term (%.3f)", sBoth, sOne)
	}
}

func TestScore_TitleHitBeatsBodyOnly(t *testing.T) {
	r := newRanker("nats", time.Time{})
	inTitle := Node{Title: "nats decision", Body: "z"}
	inBody := Node{Title: "z", Body: "nats decision"}
	sT, _ := r.score(inTitle)
	sB, _ := r.score(inBody)
	if sT <= sB {
		t.Fatalf("title hit (%.3f) must outrank body-only (%.3f)", sT, sB)
	}
}

func TestScore_RecentBeatsStaleAtEqualText(t *testing.T) {
	now := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	r := newRanker("nats", now)
	recent := Node{Title: "nats", Meta: map[string]string{"capturedAt": now.Add(-24 * time.Hour).Format(time.RFC3339)}}
	stale := Node{Title: "nats", Meta: map[string]string{"capturedAt": now.Add(-365 * 24 * time.Hour).Format(time.RFC3339)}}
	sR, _ := r.score(recent)
	sS, _ := r.score(stale)
	if sR <= sS {
		t.Fatalf("recent (%.3f) must outrank stale (%.3f)", sR, sS)
	}
}

func TestScore_MissingDateIsNeutralNotZero(t *testing.T) {
	now := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	r := newRanker("nats", now)
	noDate := Node{Title: "nats"}
	veryStale := Node{Title: "nats", Meta: map[string]string{"capturedAt": now.Add(-3650 * 24 * time.Hour).Format(time.RFC3339)}}
	sNo, _ := r.score(noDate)
	sStale, _ := r.score(veryStale)
	if sNo <= sStale {
		t.Fatalf("missing date (%.3f) should be neutral, above very stale (%.3f)", sNo, sStale)
	}
}

func TestScore_NoTextMatchHasNoTextHit(t *testing.T) {
	r := newRanker("nats", time.Time{})
	_, hit := r.score(Node{Title: "redis", Body: "cache"})
	if hit {
		t.Fatal("node with no query term must report textHit=false")
	}
}

func TestScore_KindBoostOrdersDurableAboveSession(t *testing.T) {
	r := newRanker("nats", time.Time{})
	dec := Node{Title: "nats", Kind: KindDecision}
	sess := Node{Title: "nats", Kind: KindSession}
	sD, _ := r.score(dec)
	sS, _ := r.score(sess)
	if sD <= sS {
		t.Fatalf("decision (%.3f) must outrank session (%.3f) at equal text", sD, sS)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestScore -v 2>&1 | head`
Expected: FAIL — `tokenize`/`newRanker`/`ranker.score` undefined.

- [ ] **Step 3: Write `scoring.go`**

```go
package contracts

import (
	"math"
	"strings"
	"time"
)

// Ranking weights. Constants for now; a later PR may make them configurable.
const (
	weightTF       = 1.0 // per query-term occurrence in Title+Body
	weightTitleHit = 3.0 // per distinct query term found in Title
	weightRecency  = 2.0 // scaled by recencyScore in [0,1]
	weightKind     = 1.0 // scaled by kindBoost in [0,1]
	weightProx     = 1.5 // scaled by proximityBoost in (0,1]; applied by RecallRelevant

	// recencyHalfLifeDays sets how fast recency decays: a node captured this
	// long ago scores 0.5 recency; older decays toward 0, newer toward 1.
	recencyHalfLifeDays = 30.0
	// recencyNeutral is the recency subscore for a node with no capturedAt —
	// neutral, so an undated node is never penalized below a moderately old one.
	recencyNeutral = 0.5
)

// tokenize lowercases s and splits on any non-alphanumeric run, dropping empties.
// Shared by query parsing and node scanning so both sides tokenize identically.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
}

// ranker scores nodes against a fixed query. now is the reference time for
// recency decay; a zero now disables recency (subscore stays neutral for all),
// which keeps text-only tests deterministic.
type ranker struct {
	terms map[string]bool
	now   time.Time
}

func newRanker(text string, now time.Time) ranker {
	terms := map[string]bool{}
	for _, t := range tokenize(text) {
		terms[t] = true
	}
	return ranker{terms: terms, now: now}
}

// score returns a node's relevance score and whether it matched any query term.
// It combines term frequency, a title-hit boost, recency decay, and a per-kind
// boost. Proximity is NOT included here — RecallRelevant adds it, since only the
// graph walk knows a node's distance from a root. textHit is false when no query
// term appears in Title or Body, letting callers exclude non-matches.
func (r ranker) score(n Node) (total float64, textHit bool) {
	if len(r.terms) == 0 {
		return 0, false
	}
	var tf float64
	for _, tok := range tokenize(n.Title + "\n" + n.Body) {
		if r.terms[tok] {
			tf++
		}
	}
	var titleHits float64
	seen := map[string]bool{}
	for _, tok := range tokenize(n.Title) {
		if r.terms[tok] && !seen[tok] {
			seen[tok] = true
			titleHits++
		}
	}
	textHit = tf > 0
	if !textHit {
		return 0, false
	}
	total = weightTF*tf + weightTitleHit*titleHits
	total += weightRecency * r.recencyScore(n)
	total += weightKind * kindBoost(n.Kind)
	return total, true
}

// recencyScore maps a node's Meta["capturedAt"] to [0,1] via exponential decay.
// Missing/unparseable date, or a zero reference now, yields the neutral score.
func (r ranker) recencyScore(n Node) float64 {
	if r.now.IsZero() {
		return recencyNeutral
	}
	raw := n.Meta["capturedAt"]
	if raw == "" {
		return recencyNeutral
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return recencyNeutral
	}
	ageDays := r.now.Sub(at).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	return math.Exp(-math.Ln2 * ageDays / recencyHalfLifeDays)
}

// kindBoost weights durable knowledge above transient session logs, in [0,1].
func kindBoost(k NodeKind) float64 {
	switch k {
	case KindDecision, KindUser, KindArchitecture, KindProduction:
		return 1.0
	case KindProject, KindRepo, KindServer, KindOrganization, KindDomain, KindAgent:
		return 0.6
	case KindSession:
		return 0.2
	default:
		return 0.4
	}
}

// proximityBoost decays with graph distance from a scope root: depth 0 → 1.
func proximityBoost(depth int) float64 {
	if depth < 0 {
		return 0
	}
	return 1.0 / float64(1+depth)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run 'TestScore|TestTokenize' -v 2>&1 | tail -20`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
gofmt -w scoring.go scoring_test.go
git add scoring.go scoring_test.go
git commit -m "feat(scoring): dependency-free lexical ranker (tf+title+recency+kind)"
```

---

### Task 3: `RecallRelevant` + proximity (contracts)

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/memory_scope.go`
- Test: `/home/shan/dev/herrscher-contracts/memory_scope_test.go`

- [ ] **Step 1: Write the failing tests**

Append to (or create) `memory_scope_test.go`. Use the existing fake Memory in that test file if present; otherwise add this minimal one:

```go
package contracts

import (
	"context"
	"testing"
)

// fakeMem is an in-memory Memory for scope tests: Recall returns a subgraph
// rooted at key following one level of Links against the stored node set.
type fakeMem struct{ nodes map[string]Node }

func (f *fakeMem) Record(ctx context.Context, n Node) error { f.nodes[n.Key] = n; return nil }
func (f *fakeMem) Links(ctx context.Context, from, to, rel string) error { return nil }
func (f *fakeMem) Close() error                                          { return nil }
func (f *fakeMem) Search(ctx context.Context, q Query) ([]Node, error)   { return nil, nil }
func (f *fakeMem) Recall(ctx context.Context, key string, depth int) (Subgraph, error) {
	root, ok := f.nodes[key]
	if !ok {
		return Subgraph{}, context.Canceled // any non-nil err; RecallScoped treats project-miss as fatal
	}
	sg := Subgraph{Root: root}
	seen := map[string]bool{key: true}
	frontier := []Node{root}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []Node
		for _, n := range frontier {
			for _, l := range n.Links {
				sg.Edges = append(sg.Edges, l)
				if seen[l.To] {
					continue
				}
				seen[l.To] = true
				if c, ok := f.nodes[l.To]; ok {
					sg.Nodes = append(sg.Nodes, c)
					next = append(next, c)
				}
			}
		}
		frontier = next
	}
	return sg, nil
}

func TestRecallRelevant_TopKByScoreAcrossScopes(t *testing.T) {
	f := &fakeMem{nodes: map[string]Node{}}
	ctx := context.Background()
	f.Record(ctx, Node{Key: "proj", Kind: KindProject, Title: "root", Links: []Link{{To: "a", Rel: RelContains}, {To: "b", Rel: RelContains}}})
	f.Record(ctx, Node{Key: "agent", Kind: KindAgent, Title: "me", Links: []Link{{To: "c", Rel: RelContains}}})
	f.Record(ctx, Node{Key: "a", Kind: KindDecision, Title: "use nats transport"})
	f.Record(ctx, Node{Key: "b", Kind: KindSession, Title: "nats note"})
	f.Record(ctx, Node{Key: "c", Kind: KindSession, Title: "redis cache"}) // no match
	s := MemoryScope{Project: "proj", Agent: "agent"}

	got, err := RecallRelevant(ctx, f, s, "nats transport", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want top-2, got %d", len(got))
	}
	if got[0].Key != "a" {
		t.Fatalf("highest-score node should be 'a', got %q", got[0].Key)
	}
	for _, n := range got {
		if n.Key == "c" {
			t.Fatal("zero-text-match node 'c' must never appear")
		}
	}
}

func TestRecallRelevant_ProximityBreaksTies(t *testing.T) {
	f := &fakeMem{nodes: map[string]Node{}}
	ctx := context.Background()
	// near and far have identical text/kind; near is a direct child of the root.
	f.Record(ctx, Node{Key: "proj", Kind: KindProject, Title: "root", Links: []Link{{To: "near", Rel: RelContains}, {To: "mid", Rel: RelContains}}})
	f.Record(ctx, Node{Key: "near", Kind: KindDecision, Title: "nats"})
	f.Record(ctx, Node{Key: "mid", Kind: KindDecision, Title: "hop", Links: []Link{{To: "far", Rel: RelContains}}})
	f.Record(ctx, Node{Key: "far", Kind: KindDecision, Title: "nats"})
	s := MemoryScope{Project: "proj"}

	got, err := RecallRelevant(ctx, f, s, "nats", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "near" {
		t.Fatalf("nearer node should rank first on tie; got %+v", keysOf(got))
	}
}

func keysOf(ns []Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Key
	}
	return out
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestRecallRelevant -v 2>&1 | head`
Expected: FAIL — `RecallRelevant` undefined.

- [ ] **Step 3: Implement `RecallRelevant` in `memory_scope.go`**

Add (beside `RecallScoped`), including the `time`/`sort` imports:

```go
// relevantDepth bounds how far RecallRelevant walks from each scope root before
// ranking. Deep enough to reach a project's facts and an agent's skills, shallow
// enough to keep the candidate set (and cost) small.
const relevantDepth = 3

// RecallRelevant returns the top-k nodes from the scoped subgraph ranked by
// relevance to text, instead of the full merged subgraph — bounding how much is
// primed into a prompt. Nodes with no textual match are excluded; among matches,
// term frequency, title hits, recency, kind, and graph proximity to a scope root
// order the result (highest first). Fewer than k are returned when fewer match.
func RecallRelevant(ctx context.Context, m Memory, s MemoryScope, text string, k int) ([]Node, error) {
	sg, err := RecallScoped(ctx, m, s, relevantDepth)
	if err != nil {
		return nil, err
	}
	depth := scopeDepths(sg, s)
	r := newRanker(text, time.Now().UTC())

	type scored struct {
		n     Node
		score float64
	}
	var hits []scored
	for _, n := range append([]Node{sg.Root}, sg.Nodes...) {
		base, ok := r.score(n)
		if !ok {
			continue // no textual match → excluded
		}
		d, seen := depth[n.Key]
		if !seen {
			d = relevantDepth // unreached (e.g. private root not linked from project)
		}
		hits = append(hits, scored{n: n, score: base + weightProx*proximityBoost(d)})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	out := make([]Node, len(hits))
	for i, h := range hits {
		out[i] = h.n
	}
	return out, nil
}

// scopeDepths BFS-labels every node in sg with its shortest distance (in edges)
// from a scope root (Project or Agent), reusing the subgraph's own edges. Roots
// are depth 0. Nodes unreachable from either root are absent from the map.
func scopeDepths(sg Subgraph, s MemoryScope) map[string]int {
	adj := map[string][]string{}
	for _, e := range sg.Edges {
		adj[keyFor(sg, e.To)] = adj[keyFor(sg, e.To)] // no-op to keep keys; see below
	}
	// Build adjacency from edges (from is implicit in BFS via node Links is not
	// available here, so use Edges' To reachability layered from roots).
	depth := map[string]int{}
	var frontier []string
	for _, root := range []string{s.Project, s.Agent} {
		if root != "" {
			if _, dup := depth[root]; !dup {
				depth[root] = 0
				frontier = append(frontier, root)
			}
		}
	}
	// Edge list is flat (from→to); index it.
	fanout := map[string][]string{}
	for _, e := range sg.Edges {
		fanout[e.From()] = append(fanout[e.From()], e.To)
	}
	for len(frontier) > 0 {
		var next []string
		for _, cur := range frontier {
			for _, to := range fanout[cur] {
				if _, ok := depth[to]; !ok {
					depth[to] = depth[cur] + 1
					next = append(next, to)
				}
			}
		}
		frontier = next
	}
	return depth
}
```

> **Implementer note:** `Link` has fields `To` and `Rel` only — there is **no `From`**. So the `fanout` above cannot be built from `Edges` alone (an edge doesn't record its source). Resolve this by BFS over **node Links** instead of `Edges`: build `map[key]Node` from `{Root}+Nodes`, then BFS from the roots following each node's `Links[].To`. Replace `scopeDepths` with the version below and delete the broken `keyFor`/`e.From()` references:

```go
func scopeDepths(sg Subgraph, s MemoryScope) map[string]int {
	byKey := map[string]Node{sg.Root.Key: sg.Root}
	for _, n := range sg.Nodes {
		byKey[n.Key] = n
	}
	depth := map[string]int{}
	var frontier []string
	for _, root := range []string{s.Project, s.Agent} {
		if root == "" {
			continue
		}
		if _, ok := byKey[root]; ok {
			if _, dup := depth[root]; !dup {
				depth[root] = 0
				frontier = append(frontier, root)
			}
		}
	}
	for len(frontier) > 0 {
		var next []string
		for _, cur := range frontier {
			for _, l := range byKey[cur].Links {
				if _, ok := depth[l.To]; !ok {
					if _, exists := byKey[l.To]; exists {
						depth[l.To] = depth[cur] + 1
						next = append(next, l.To)
					}
				}
			}
		}
		frontier = next
	}
	return depth
}
```

Use ONLY this second `scopeDepths`; do not include the first sketch or `keyFor`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... 2>&1 | tail -20`
Expected: all PASS (including the new RecallRelevant tests).

- [ ] **Step 5: Gate + commit**

```bash
cd /home/shan/dev/herrscher-contracts
gofmt -w memory_scope.go memory_scope_test.go && go vet ./... && go test -race ./...
git add memory_scope.go memory_scope_test.go
git commit -m "feat(recall): RecallRelevant top-K ranked recall with graph proximity"
```

---

### Task 4: Stamp `capturedAt` on Record (obsidian)

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory.go` (struct + `New` + `recordUnlocked`)
- Test: `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `memory_test.go`:

```go
func TestRecord_StampsCapturedAtWhenAbsent(t *testing.T) {
	m := newTestMem(t)
	fixed := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "facts/x", Kind: contracts.KindDecision, Title: "x"}); err != nil {
		t.Fatal(err)
	}
	sg, err := m.Recall(ctx, "facts/x", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := sg.Root.Meta["capturedAt"]; got != fixed.Format(time.RFC3339) {
		t.Fatalf("capturedAt: want %q, got %q", fixed.Format(time.RFC3339), got)
	}
}

func TestRecord_PreservesCapturedAtOnUpsert(t *testing.T) {
	m := newTestMem(t)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	m.now = func() time.Time { return t0 }
	m.Record(ctx, contracts.Node{Key: "facts/x", Kind: contracts.KindDecision, Title: "v1"})
	m.now = func() time.Time { return t1 } // later re-record (upsert)
	m.Record(ctx, contracts.Node{Key: "facts/x", Kind: contracts.KindDecision, Title: "v2"})
	sg, _ := m.Recall(ctx, "facts/x", 0)
	if got := sg.Root.Meta["capturedAt"]; got != t0.Format(time.RFC3339) {
		t.Fatalf("capturedAt must be preserved from first write %q, got %q", t0.Format(time.RFC3339), got)
	}
}

func TestRecord_KeepsCallerSuppliedCapturedAt(t *testing.T) {
	m := newTestMem(t)
	m.now = func() time.Time { return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	supplied := "2020-05-05T00:00:00Z"
	m.Record(ctx, contracts.Node{Key: "facts/x", Kind: contracts.KindDecision, Title: "x", Meta: map[string]string{"capturedAt": supplied}})
	sg, _ := m.Recall(ctx, "facts/x", 0)
	if got := sg.Root.Meta["capturedAt"]; got != supplied {
		t.Fatalf("caller-supplied capturedAt must be kept: want %q, got %q", supplied, got)
	}
}
```

Ensure `memory_test.go` imports `"time"`.

- [ ] **Step 2: Run to verify they fail**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestRecord_ 2>&1 | head`
Expected: FAIL — `m.now` undefined (field doesn't exist yet).

- [ ] **Step 3: Add the clock field and stamping**

In `memory.go`, extend the struct and `New`:

```go
type ObsidianMemory struct {
	mu       sync.Mutex
	root     *os.Root
	lockFile *os.File
	now      func() time.Time // injectable clock for capturedAt (tests override)
}
```

In `New`, after constructing the value (before returning it), set:

```go
	m.now = time.Now
```

(Adapt to however `New` currently builds the struct — set `now: time.Now` in the literal if it uses one.)

Add `"time"` to the imports.

In `recordUnlocked`, at the very top (after the `validKey` check), stamp capturedAt:

```go
	// Stamp capturedAt (RFC3339 UTC) so recall can rank by recency. Only when
	// absent: a caller-supplied value is kept, and on upsert an existing stored
	// value is preserved so re-recording the same fact does not reset its age.
	if n.Meta["capturedAt"] == "" {
		at := m.now().UTC().Format(time.RFC3339)
		if existing, err := m.loadUnlocked(n.Key); err == nil {
			if prior := existing.Meta["capturedAt"]; prior != "" {
				at = prior
			}
		}
		if n.Meta == nil {
			n.Meta = map[string]string{}
		}
		n.Meta["capturedAt"] = at
	}
```

> Reading `n.Meta["capturedAt"]` on a nil map is safe in Go (returns ""). `loadUnlocked` is already safe to call here — `recordUnlocked` runs under the mutex+flock.

- [ ] **Step 4: Run to verify they pass**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestRecord_ -v 2>&1 | tail -15`
Expected: all three PASS.

- [ ] **Step 5: Confirm existing tests still pass, then commit**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && gofmt -w memory.go memory_test.go && go vet ./... && go test -race ./... 2>&1 | tail -5`
Expected: all PASS (existing Record/Recall/Search tests unaffected — capturedAt is additive Meta).

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go memory_test.go
git commit -m "feat(record): stamp capturedAt (UTC) on write, preserved on upsert"
```

---

### Task 5: Ranked `Search` (obsidian)

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory.go` (`Search`, lines ~208-244)
- Test: `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `memory_test.go`:

```go
func TestSearch_RankedOrdersByRelevance(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	m.Record(ctx, contracts.Node{Key: "facts/both", Kind: contracts.KindDecision, Title: "nats transport choice", Body: "use nats for transport"})
	m.Record(ctx, contracts.Node{Key: "facts/one", Kind: contracts.KindDecision, Title: "logging", Body: "mentions nats once"})
	got, err := m.Search(ctx, contracts.Query{Text: "nats transport", Ranked: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "facts/both" {
		t.Fatalf("ranked: two-term node should lead; got %v", keysOfNodes(got))
	}
}

func TestSearch_RankedRespectsLimitAsTopK(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	m.Record(ctx, contracts.Node{Key: "facts/both", Kind: contracts.KindDecision, Title: "nats transport", Body: "nats transport"})
	m.Record(ctx, contracts.Node{Key: "facts/one", Kind: contracts.KindSession, Title: "note", Body: "nats"})
	got, err := m.Search(ctx, contracts.Query{Text: "nats transport", Ranked: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "facts/both" {
		t.Fatalf("ranked+limit should return the single best; got %v", keysOfNodes(got))
	}
}

func TestSearch_UnrankedIsUnchanged(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindDecision, Title: "nats", Body: "x"})
	m.Record(ctx, contracts.Node{Key: "b", Kind: contracts.KindDecision, Title: "nats deep", Body: "nats nats"})
	// Ranked:false must preserve today's semantics: all matches, walk order, no sort.
	got, err := m.Search(ctx, contracts.Query{Text: "nats"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("unranked should return all matches, got %d", len(got))
	}
}

func keysOfNodes(ns []contracts.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Key
	}
	return out
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestSearch_Ranked 2>&1 | head`
Expected: FAIL — ranked ordering not implemented (two-term node not guaranteed first).

- [ ] **Step 3: Implement ranked sort in `Search`**

In `Search`, keep the walk + `matchesQuery` gate exactly as-is (it collects `out []contracts.Node`). Replace the final truncation block (currently `if q.Limit > 0 && len(out) > q.Limit { out = out[:q.Limit] }`) with rank-then-cut:

```go
	if q.Ranked {
		r := newRanker(q.Text, m.now().UTC())
		// Stable sort by descending score; matchesQuery already gated membership,
		// so every node here is a legitimate match — ranking only orders them.
		scores := make(map[string]float64, len(out))
		for _, n := range out {
			s, _ := r.score(n)
			scores[n.Key] = s
		}
		sort.SliceStable(out, func(i, j int) bool { return scores[out[i].Key] > scores[out[j].Key] })
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
```

> `newRanker`/`score` are exported-to-package? They live in `package contracts`, so obsidian must call them as `contracts.newRanker` — but they are **unexported**. Resolve: add a thin exported entrypoint in contracts, OR compute the score via an exported function. Add to `contracts/scoring.go`:
>
> ```go
> // Score exposes the lexical relevance score of n against text for out-of-package
> // rankers (e.g. the obsidian Search impl). now drives recency decay; pass a zero
> // Time to disable it. The bool reports whether any query term matched.
> func Score(text string, now time.Time, n Node) (float64, bool) {
> 	return newRanker(text, now).score(n)
> }
> ```
>
> Then in obsidian `Search`, use `contracts.Score(q.Text, m.now().UTC(), n)` per node instead of constructing a ranker. Update Task 2's commit or add `Score` here in Task 5 (add it in contracts, re-run contracts gate, commit contracts, then implement obsidian). Add `"sort"` to obsidian imports.

Add `Score` to `contracts/scoring.go`, commit it in contracts:

```bash
cd /home/shan/dev/herrscher-contracts
gofmt -w scoring.go && go test ./... && git add scoring.go && git commit -m "feat(scoring): export Score for out-of-package rankers"
```

Then implement the obsidian `Search` change using `contracts.Score`.

- [ ] **Step 4: Run to verify they pass**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... 2>&1 | tail -15`
Expected: all PASS (ranked tests + unchanged unranked + all prior).

- [ ] **Step 5: Full gate + commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
gofmt -w memory.go memory_test.go && go vet ./... && go test -race ./... 2>&1 | tail -5
git add memory.go memory_test.go
git commit -m "feat(search): score-sort results when Query.Ranked (top-K by relevance)"
```

---

### Task 6: Release `herrscher-contracts` v0.1.10 (USER-GATED)

**Files:** none (tag/push only).

- [ ] **Step 1: Verify clean gate**

Run: `cd /home/shan/dev/herrscher-contracts && go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test -race ./... 2>&1 | tail -3`
Expected: green.

- [ ] **Step 2: CONFIRM version v0.1.10 with the user, then tag + push**

```bash
cd /home/shan/dev/herrscher-contracts
git push origin master
git tag -a v0.1.10 -m "v0.1.10 — Query.Ranked + Score + RecallRelevant (chantier A)"
git push origin v0.1.10
```

---

### Task 7: Release `herrscher-obsidian-memory` v0.2.1 (USER-GATED)

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/go.mod` (require contracts v0.1.10)

- [ ] **Step 1: Bump contracts require and tidy**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
GOPRIVATE=github.com/Herrscherd/* GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-contracts@v0.1.10
go mod tidy
```

- [ ] **Step 2: Full gate**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test -race ./... 2>&1 | tail -3`
Expected: green.

- [ ] **Step 3: Commit, CONFIRM v0.2.1, tag + push**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add go.mod go.sum && git commit -m "chore: require herrscher-contracts v0.1.10 (ranked recall)"
git push origin master
git tag -a v0.2.1 -m "v0.2.1 — ranked Search + capturedAt stamping (chantier A)"
git push origin v0.2.1
```

---

### Task 8: Host dependency bump via branch + PR (USER-GATED)

**Files:**
- Modify: `/home/shan/dev/herrscher/go.mod`, `go.sum`

- [ ] **Step 1: Branch, bump both deps, tidy**

```bash
cd /home/shan/dev/herrscher
git checkout -b chore/memory-a-recall-ranking
GOPRIVATE=github.com/Herrscherd/* GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-contracts@v0.1.10 github.com/Herrscherd/herrscher-obsidian-memory@v0.2.1
go mod tidy
```

- [ ] **Step 2: Verify tidy gate + build**

Run: `cd /home/shan/dev/herrscher && go mod tidy && git diff --exit-code go.mod go.sum && go build ./... && go vet ./...`
Expected: tidy clean, build+vet green.

- [ ] **Step 3: Commit, push, PR, merge**

```bash
cd /home/shan/dev/herrscher
git add go.mod go.sum && git commit -m "chore(host): bump contracts v0.1.10 + obsidian-memory v0.2.1 (chantier A recall ranking)"
git push -u origin chore/memory-a-recall-ranking
gh pr create --base master --title "chore(host): chantier A — ranked recall deps" --body "Bumps contracts→v0.1.10 and obsidian-memory→v0.2.1 to pick up ranked Search, RecallRelevant, and capturedAt stamping."
```

Merge after review (see final review step).

---

## Notes for the executor

- **Workspace-first:** contracts + obsidian resolve locally via `/home/shan/dev/go.work`. Implement and test Tasks 1-5 fully before ANY release (Tasks 6-8). The obsidian tests in Tasks 4-5 exercise the un-released contracts `Score`/scoring directly through the workspace.
- **Ordering caveat:** `contracts.Score` (Task 5) must exist before obsidian's ranked `Search` compiles. If executing strictly task-by-task, add `Score` to contracts as soon as Task 5 Step 3 says, re-running the contracts gate.
- **Do not** wire `RecallRelevant` into the orchestrator's turn-priming in this PR — that's a separate concern (spec lists only contracts + obsidian). This PR ships the capability.
- **No silent caps:** ranked `Limit` truncation is top-K by design and fine; there is nothing to log here since Search returns the ranked slice.
- Releases (Tasks 6-8) are USER-GATED: confirm each version before pushing/tagging.
