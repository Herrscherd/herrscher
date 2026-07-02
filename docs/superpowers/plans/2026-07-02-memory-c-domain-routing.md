# Memory C — Domain Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a lightweight transverse `domain` layer (dev/research/…) above the Org→Project spine, so memory can be routed and filtered by area of concern — matching the "Vault Obsidian / Memory" diagram.

**Architecture:** A new `KindDomain` node kind in `herrscher-contracts`; then in `herrscher-obsidian-memory` an optional `InitSpec.Domain` that scaffolds a domain index node + bidirectional links + a `domain` frontmatter tag on the project, and `matchesQuery` treats `Meta["domain"]` as a searchable tag. Purely additive: zero-value `Domain` reproduces today's behaviour exactly.

**Tech Stack:** Go 1.25, two Go modules (`github.com/Herrscherd/herrscher-contracts`, `github.com/Herrscherd/herrscher-obsidian-memory`), standard `testing`.

**Spec:** `docs/superpowers/specs/2026-07-02-memory-c-domain-routing-design.md`

**Repo checkouts (already cloned, on `master` == latest tag):**
- `/home/shan/dev/herrscher-contracts` (master == v0.1.8)
- `/home/shan/dev/herrscher-obsidian-memory` (master == v0.1.1, depends on contracts v0.1.4)

**Cross-module sequencing:** contracts ships first (Phase A → tag `v0.1.9`), then obsidian-memory bumps to it (Phase B). During Phase B local development, a temporary `replace` directive points obsidian at the local contracts checkout so tests run before the tag exists; the replace is removed and the real version pinned before merge.

---

## Phase A — `herrscher-contracts`: `KindDomain`

Work in `/home/shan/dev/herrscher-contracts` on a feature branch.

### Task A1: Add the `KindDomain` node kind

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/memory.go` (const block, lines 10-23)
- Test: `/home/shan/dev/herrscher-contracts/memory_test.go`

- [ ] **Step 1: Create branch**

```bash
cd /home/shan/dev/herrscher-contracts && git checkout -b feat/kind-domain
```

- [ ] **Step 2: Write the failing test**

Add to `/home/shan/dev/herrscher-contracts/memory_test.go`:

```go
func TestKindDomainConstant(t *testing.T) {
	if KindDomain != "domain" {
		t.Fatalf("KindDomain = %q, want %q", KindDomain, "domain")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestKindDomainConstant`
Expected: FAIL — `undefined: KindDomain`

- [ ] **Step 4: Add the constant**

In `memory.go`, inside the `const ( … )` block, immediately after the `KindAgent` block (line 23), add:

```go
	// KindDomain is a transverse area-of-concern root (dev, research, …) grouping
	// projects and facts topically, above the ownership spine. A project links to
	// its domain with "in-domain"; a fact carries Meta["domain"] for filtering.
	KindDomain NodeKind = "domain"
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: PASS (all tests)

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add memory.go memory_test.go
git commit -m "feat(memory): add KindDomain node kind for transverse routing"
```

### Task A2: Release contracts

- [ ] **Step 1: Merge to master** (open PR `feat/kind-domain` → merge, or fast-forward locally if solo)

```bash
cd /home/shan/dev/herrscher-contracts && git checkout master && git merge --ff-only feat/kind-domain
```

- [ ] **Step 2: Tag and push**

```bash
cd /home/shan/dev/herrscher-contracts
git tag v0.1.9
git push origin master v0.1.9
```

Expected: tag `v0.1.9` visible via `git -C /home/shan/dev/herrscher-contracts tag | tail -1`.

---

## Phase B — `herrscher-obsidian-memory`: domain scaffolding + search

Work in `/home/shan/dev/herrscher-obsidian-memory` on a feature branch.

### Task B1: Point at local contracts for development

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/go.mod`

- [ ] **Step 1: Create branch**

```bash
cd /home/shan/dev/herrscher-obsidian-memory && git checkout -b feat/domain-routing
```

- [ ] **Step 2: Add a temporary replace directive**

Append to `/home/shan/dev/herrscher-obsidian-memory/go.mod`:

```
replace github.com/Herrscherd/herrscher-contracts => /home/shan/dev/herrscher-contracts
```

- [ ] **Step 3: Sync and verify build**

Run:
```bash
cd /home/shan/dev/herrscher-obsidian-memory && go mod tidy && go build ./...
```
Expected: builds; `contracts.KindDomain` now resolvable from the local checkout.
Note: this `replace` is temporary and removed in Task B5 before merge.

### Task B2: `InitSpec.Domain` field and domain scaffolding

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/scaffold.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/scaffold_test.go`

- [ ] **Step 1: Write the failing test**

Add to `scaffold_test.go`:

```go
func TestInitWithDomainScaffoldsDomainNodeAndLinks(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	spec := InitSpec{Domain: "dev", Project: "proj"}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// domain index node exists and is KindDomain
	dom, err := m.load("domaines/dev/index")
	if err != nil {
		t.Fatalf("domain node missing: %v", err)
	}
	if dom.Kind != contracts.KindDomain {
		t.Fatalf("domain kind = %s, want %s", dom.Kind, contracts.KindDomain)
	}

	// domain → project ("contains")
	foundContains := false
	for _, l := range dom.Links {
		if l.To == "projets/proj/index" && l.Rel == "contains" {
			foundContains = true
		}
	}
	if !foundContains {
		t.Fatalf("domain does not contain project: %+v", dom.Links)
	}

	// project → domain ("in-domain") and Meta["domain"] stamped
	proj, err := m.load("projets/proj/index")
	if err != nil {
		t.Fatalf("project node missing: %v", err)
	}
	if proj.Meta["domain"] != "dev" {
		t.Fatalf("project domain meta = %q, want dev", proj.Meta["domain"])
	}
	foundInDomain := false
	for _, l := range proj.Links {
		if l.To == "domaines/dev/index" && l.Rel == "in-domain" {
			foundInDomain = true
		}
	}
	if !foundInDomain {
		t.Fatalf("project not linked in-domain: %+v", proj.Links)
	}
}

func TestInitWithoutDomainIsUnchanged(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	if err := m.Init(ctx, InitSpec{Project: "solo"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := m.load("domaines/solo/index"); err == nil {
		t.Fatalf("no-domain Init created a domain node")
	}
	proj, err := m.load("projets/solo/index")
	if err != nil {
		t.Fatalf("project missing: %v", err)
	}
	if _, has := proj.Meta["domain"]; has {
		t.Fatalf("no-domain Init stamped a domain meta: %+v", proj.Meta)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestInitWith`
Expected: FAIL — `unknown field 'Domain' in struct literal`

- [ ] **Step 3: Add the `Domain` field**

In `scaffold.go`, change the `InitSpec` struct to:

```go
// InitSpec describes a project to scaffold. Org is optional; when empty the
// project lives flat under "projets/<Project>". Domain is optional; when set it
// attaches the project to a transverse domain root under "domaines/<Domain>".
type InitSpec struct {
	Org     string
	Domain  string
	Project string
	Repos   []string
	Servers []string
}
```

- [ ] **Step 4: Scaffold the domain node, links, and project meta**

In `scaffold.go`, inside `Init`, locate the block that builds `projLinks` and records the project node. Make three edits:

(a) After `base := s.base()` and before the `if s.Org != ""` org block, add the project→domain link seed and domain-node scaffold. Insert:

```go
	// Domain (optional, transverse): attach the project to a "domaines/<slug>" root.
	var domainKey string
	if s.Domain != "" {
		domainKey = "domaines/" + s.Domain + "/index"
	}
```

(b) Where `projLinks` is assembled (after the architecture/production `contains` links, before the repos loop), add the in-domain back-link:

```go
	if domainKey != "" {
		projLinks = append(projLinks, contracts.Link{To: domainKey, Rel: "in-domain"})
	}
```

(c) Change the project-node `ensure` call to stamp the domain meta. Replace the existing project `ensure`:

```go
	projNode := contracts.Node{Key: projKey, Kind: contracts.KindProject,
		Title: s.Project, Links: projLinks}
	if s.Domain != "" {
		projNode.Meta = map[string]string{"domain": s.Domain}
	}
	if err := m.ensure(ctx, projNode); err != nil {
		return err
	}
```

(d) After the project `ensure` (and before the architecture/production ensures), scaffold the domain index node itself, linking down to the project:

```go
	if domainKey != "" {
		if err := m.ensure(ctx, contracts.Node{Key: domainKey, Kind: contracts.KindDomain,
			Title: s.Domain, Links: []contracts.Link{{To: projKey, Rel: "contains"}}}); err != nil {
			return err
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./...`
Expected: PASS — new domain tests pass; existing `TestInitScaffoldsHierarchy`, `TestInitProjectReachesChildrenViaRecall`, `TestInitIsIdempotentAndNeverOverwrites` still pass.

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add scaffold.go scaffold_test.go
git commit -m "feat(scaffold): optional Domain attaches project to a transverse domain root"
```

### Task B3: Domain is searchable as a tag

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory.go` (`matchesQuery`)
- Test: `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`

- [ ] **Step 1: Write the failing test**

Add to `memory_test.go`:

```go
func TestSearchMatchesDomainAsTag(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "projets/x/index", Kind: contracts.KindProject,
		Title: "X", Meta: map[string]string{"domain": "dev"}}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := m.Search(ctx, contracts.Query{Tags: []string{"dev"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Key != "projets/x/index" {
		t.Fatalf("domain tag search did not find node: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestSearchMatchesDomainAsTag`
Expected: FAIL — node not found (domain meta not counted as a tag).

- [ ] **Step 3: Widen `matchesQuery` to count `domain` meta as a tag**

In `memory.go`, in `matchesQuery`, replace the tag-building block:

```go
	if len(q.Tags) > 0 {
		tags := map[string]bool{}
		for _, t := range strings.Split(n.Meta["tags"], ",") {
			tags[strings.TrimSpace(strings.ToLower(t))] = true
		}
```

with (adds the `domain` meta value into the same tag set):

```go
	if len(q.Tags) > 0 {
		tags := map[string]bool{}
		for _, t := range strings.Split(n.Meta["tags"], ",") {
			tags[strings.TrimSpace(strings.ToLower(t))] = true
		}
		if d := strings.TrimSpace(strings.ToLower(n.Meta["domain"])); d != "" {
			tags[d] = true
		}
```

(the closing `for _, want := range q.Tags { … }` loop below is unchanged.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./...`
Expected: PASS (new test + all existing).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go memory_test.go
git commit -m "feat(search): match Meta[\"domain\"] as a searchable tag"
```

### Task B4: Recall reaches project from its domain

**Files:**
- Test only: `/home/shan/dev/herrscher-obsidian-memory/scaffold_test.go`

- [ ] **Step 1: Write the test** (proves the routing graph is navigable end-to-end)

```go
func TestRecallDomainReachesProject(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	if err := m.Init(ctx, InitSpec{Domain: "dev", Project: "proj"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sg, err := m.Recall(ctx, "domaines/dev/index", 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	reached := map[string]bool{}
	for _, n := range sg.Nodes {
		reached[n.Key] = true
	}
	if !reached["projets/proj/index"] {
		t.Fatalf("Recall(domain) did not reach project: %v", reached)
	}
}
```

- [ ] **Step 2: Run test**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestRecallDomainReachesProject`
Expected: PASS (no code change needed — validates B2 wiring).

- [ ] **Step 3: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add scaffold_test.go
git commit -m "test(scaffold): Recall(domain) reaches contained project"
```

### Task B5: Pin the released contracts version and drop the replace

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/go.mod`

- [ ] **Step 1: Remove the temporary replace directive**

Delete this line from `go.mod`:

```
replace github.com/Herrscherd/herrscher-contracts => /home/shan/dev/herrscher-contracts
```

- [ ] **Step 2: Require the released contracts tag**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
go get github.com/Herrscherd/herrscher-contracts@v0.1.9
go mod tidy
```

- [ ] **Step 3: Full test run against the released dependency**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go build ./... && go test ./...`
Expected: PASS — no `replace`, real `contracts@v0.1.9`.

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add go.mod go.sum
git commit -m "build: pin herrscher-contracts v0.1.9 (KindDomain)"
```

### Task B6: Release obsidian-memory

- [ ] **Step 1: Merge to master**

```bash
cd /home/shan/dev/herrscher-obsidian-memory && git checkout master && git merge --ff-only feat/domain-routing
```

- [ ] **Step 2: Tag and push**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git tag v0.2.0
git push origin master v0.2.0
```

(Minor bump: additive `InitSpec.Domain` + widened search, no breaking change.)

---

## Phase C — (optional) bump herrscher to pick up the new versions

The main `herrscher` repo only needs the bump when a later chantier (B/D) uses
domains through the host. C alone changes no host behaviour, so this is optional
and can be deferred. If desired:

- [ ] **Step 1:** `cd /home/shan/dev/herrscher && go get github.com/Herrscherd/herrscher-contracts@v0.1.9 github.com/Herrscherd/herrscher-obsidian-memory@v0.2.0 && go mod tidy && go build ./... && go test ./...`
- [ ] **Step 2:** commit `build: bump contracts v0.1.9 + obsidian-memory v0.2.0 (domain routing)` on a branch.

---

## Self-Review

**Spec coverage:**
- KindDomain node → Task A1 ✓
- `domaines/<slug>` key + KindDomain index node → Task B2 (d) ✓
- project ↔ domain links (`in-domain` / `contains`) → Task B2 (b),(d) ✓
- `Meta["domain"]` routing tag → Task B2 (c) ✓
- `InitSpec.Domain`, zero-value = unchanged → Task B2 + `TestInitWithoutDomainIsUnchanged` ✓
- domain filtering via `matchesQuery` reusing `q.Tags` → Task B3 ✓
- no breaking changes → additive const/field, widened match; regression guards in B2/B3 ✓

**Placeholder scan:** none — every code step shows exact code; every run step shows exact command + expected result.

**Type consistency:** `KindDomain` (contracts), `InitSpec.Domain` (string), keys `domaines/<slug>/index`, rels `in-domain`/`contains`, `Meta["domain"]` — used identically across A1, B2, B3, B4.
