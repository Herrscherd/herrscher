# Memory B — LLM Extractor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a new module `herrscher-llm-extractor` — a generic, LLM-driven `orchestrator.Extractor` that turns the call journal + session transcript into memory candidates, filling the one empty slot in herrscher's already-built curation loop so the vault self-populates.

**Architecture:** A blank-import plugin (xcaddy pattern). Its `init()` registers an `LLMExtractor` under the name `"llm"`. At `Extract` time it lazily builds a curation `contracts.Backend` from the plugin registry (mirroring the host's `firstBackend`), asks the model for a JSON array of candidates, and tolerantly parses that into `[]orchestrator.Candidate` with stable Keys (cross-session upsert) and provenance Meta. Zero changes to `herrscher-orchestrator`, `herrscher-contracts`, or `herrscher` core — the nudge (every-N-turns `Consolidate`) is already wired end to end; the host only adds a blank import.

**Tech Stack:** Go 1.25, `encoding/json`, `github.com/Herrscherd/herrscher-contracts` (Node/Backend/registry/Resolve), `github.com/Herrscherd/herrscher-orchestrator` (Extractor/Candidate/RegisterExtractor). TDD with `go test -race`.

**Spec:** `docs/superpowers/specs/2026-07-02-memory-b-auto-capture-design.md`

**Reference signatures (verified against source, do not re-derive):**
- `orchestrator.Candidate{ Node contracts.Node; Private bool }`
- `orchestrator.Extractor interface { Extract(ctx context.Context, journal, transcript string) ([]Candidate, error) }`
- `orchestrator.RegisterExtractor(name string, e Extractor)`
- `contracts.Node{ Key string; Kind NodeKind; Title string; Body string; Links []Link; Meta map[string]string }`
- `contracts.Link{ To, Rel string }`; `contracts.RelAppliesTo = "applies-to"`, `contracts.RelContains = "contains"`
- NodeKinds: `KindOrganization,KindProject,KindRepo,KindServer,KindArchitecture,KindProduction,KindSession,KindDecision,KindUser,KindAgent,KindDomain` (all `NodeKind` string consts)
- `contracts.Backend interface { Respond(ctx, Prompt, func(BackendEvent)) (string, error); Close() error }`
- `contracts.Prompt{ Content, Context, Author, MessageID, ChannelID string; Attachments []string }`
- `contracts.Default.Backends() []Plugin`; `contracts.Plugin{ Manifest Manifest; Backend BackendFactory }`; `BackendFactory func(ctx, PluginConfig)(Backend, error)`
- `contracts.Resolve(settings []Setting, getenv func(string) string) (PluginConfig, error)`
- `contracts.Manifest{ Kind string; Category Category; Config []Setting }`; `contracts.Setting{ Key, Env, Help string; Required bool; Default string }`
- `contracts.Memory interface { Recall(ctx,key,depth)(Subgraph,error); Record(ctx,Node) error; Search(ctx,Query)([]Node,error); Links(ctx,from,to,rel) error; Close() error }`
- `contracts.RecordShared/RecordPrivate(ctx, m Memory, s MemoryScope, n Node) error` — `Record` then `Links` under Project/Agent root; Key used verbatim.

---

## File Structure

New repo `github.com/Herrscherd/herrscher-llm-extractor`, package `llmextractor`:

- `go.mod` — module + requires + dev-only local `replace` directives.
- `candidate.go` — JSON DTOs, `parseCandidates`, `firstCandidateArray`, `toNode`, `mapKind`, `stableKey`, `slugOrHash`, `slug`. The pure data core.
- `prompt.go` — `extractionPrompt` + the `instructions` constant.
- `extractor.go` — `LLMExtractor`, `Option`, `New`, `Extract`, `resolveBackend`.
- `backend.go` — `curationEnv`, `backendFrom`, `lazyBackend`. Registry-driven backend acquisition.
- `register.go` — `init()` → `RegisterExtractor("llm", newDefault())`; `defaultThreshold`, `defaultMax`, `newDefault`.
- `README.md`, `.github/workflows/ci.yml`.
- Tests colocated: `candidate_test.go`, `prompt_test.go`, `extractor_test.go`, `backend_test.go`, `register_test.go`, `integration_test.go`.

Host wiring (`herrscher`) is Task 10, gated on the extractor being released (Task 9, user-gated).

---

## Task 1: Scaffold the module

**Files:**
- Create: `/home/shan/dev/herrscher-llm-extractor/go.mod`
- Create: `/home/shan/dev/herrscher-llm-extractor/doc.go`

- [ ] **Step 1: Create the repo directory and init git**

```bash
mkdir -p /home/shan/dev/herrscher-llm-extractor
cd /home/shan/dev/herrscher-llm-extractor
git init -q
```

- [ ] **Step 2: Write `go.mod` with dev-only local replaces**

Create `/home/shan/dev/herrscher-llm-extractor/go.mod`:

```
module github.com/Herrscherd/herrscher-llm-extractor

go 1.25

require (
	github.com/Herrscherd/herrscher-contracts v0.1.9
	github.com/Herrscherd/herrscher-orchestrator v0.1.4
)

// dev-only — dropped before release (see Task 9)
replace github.com/Herrscherd/herrscher-contracts => /home/shan/dev/herrscher-contracts

replace github.com/Herrscherd/herrscher-orchestrator => /home/shan/dev/herrscher-orchestrator
```

- [ ] **Step 3: Write a package doc file so the module compiles empty**

Create `/home/shan/dev/herrscher-llm-extractor/doc.go`:

```go
// Package llmextractor is herrscher's open, reference memory curator: a generic
// LLM-driven orchestrator.Extractor. A blank import registers it under "llm"; the
// host then opts a session into auto-capture with
// `session create --extractor llm --journal <path> --consolidate-every N`.
//
// The Roblox-specific curation heuristics are a separate, closed extractor; this
// package is the reusable default that ships in the open.
package llmextractor
```

- [ ] **Step 4: Verify it builds**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go build ./...`
Expected: no output, exit 0 (Go downloads nothing — both deps are local `replace`s).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-llm-extractor
printf '/*.test\n*.out\n' > .gitignore
git add go.mod doc.go .gitignore
git commit -q -m "chore: scaffold herrscher-llm-extractor module"
```

---

## Task 2: Tolerant candidate parsing (the pure core)

**Files:**
- Create: `/home/shan/dev/herrscher-llm-extractor/candidate.go`
- Test: `/home/shan/dev/herrscher-llm-extractor/candidate_test.go`

- [ ] **Step 1: Write the failing tests**

Create `/home/shan/dev/herrscher-llm-extractor/candidate_test.go`:

```go
package llmextractor

import (
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

const twoValid = `[
 {"kind":"decision","title":"Use NATS","body":"**Why:** decoupling","tags":["nats","transport"],"private":false,"confidence":0.9},
 {"kind":"session","title":"Learned flock trick","body":"skill","private":true,"confidence":0.8}
]`

func TestParseCandidates_MapsFieldsAndScope(t *testing.T) {
	cs := parseCandidates(twoValid, 0.6, 0)
	if len(cs) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(cs))
	}
	if cs[0].Private || !cs[1].Private {
		t.Fatalf("scope routing wrong: %v / %v", cs[0].Private, cs[1].Private)
	}
	if cs[0].Node.Kind != contracts.KindDecision {
		t.Fatalf("kind: got %q", cs[0].Node.Kind)
	}
	if cs[0].Node.Meta["capturedBy"] != "llm-extractor" {
		t.Fatalf("provenance missing: %v", cs[0].Node.Meta)
	}
	if cs[0].Node.Meta["tags"] != "nats,transport" {
		t.Fatalf("tags: got %q", cs[0].Node.Meta["tags"])
	}
}

func TestParseCandidates_StableKeyIsDeterministic(t *testing.T) {
	a := parseCandidates(twoValid, 0.6, 0)
	b := parseCandidates(twoValid, 0.6, 0)
	if a[0].Node.Key != b[0].Node.Key || a[0].Node.Key == "" {
		t.Fatalf("keys not stable: %q vs %q", a[0].Node.Key, b[0].Node.Key)
	}
	if a[0].Node.Key != "facts/decision/use-nats" {
		t.Fatalf("shared key shape: got %q", a[0].Node.Key)
	}
	if a[1].Node.Key != "skills/learned-flock-trick" {
		t.Fatalf("private key shape: got %q", a[1].Node.Key)
	}
}

func TestParseCandidates_DropsBelowThresholdAndCaps(t *testing.T) {
	if got := parseCandidates(twoValid, 0.85, 0); len(got) != 1 {
		t.Fatalf("threshold: want 1 (0.9 kept, 0.8 dropped), got %d", len(got))
	}
	if got := parseCandidates(twoValid, 0.0, 1); len(got) != 1 {
		t.Fatalf("cap: want 1, got %d", len(got))
	}
}

func TestParseCandidates_TolerantOfWrapperAndGarbage(t *testing.T) {
	fenced := "Sure! Here you go:\n```json\n" + twoValid + "\n```\nHope that helps."
	if got := parseCandidates(fenced, 0.6, 0); len(got) != 2 {
		t.Fatalf("fenced/prose-wrapped: want 2, got %d", len(got))
	}
	if got := parseCandidates("not json at all", 0.6, 0); got != nil {
		t.Fatalf("garbage: want nil, got %v", got)
	}
	if got := parseCandidates(`[{"title":"","confidence":1}]`, 0.6, 0); got != nil {
		t.Fatalf("empty title: want nil, got %v", got)
	}
}

func TestMapKind_UnknownFallsBackToSession(t *testing.T) {
	if mapKind("architecture") != contracts.KindArchitecture {
		t.Fatal("known kind lost")
	}
	if mapKind("wibble") != contracts.KindSession {
		t.Fatal("unknown kind should fall back to session")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test ./...`
Expected: FAIL — `undefined: parseCandidates` / `undefined: mapKind`.

- [ ] **Step 3: Write `candidate.go`**

Create `/home/shan/dev/herrscher-llm-extractor/candidate.go`:

```go
package llmextractor

import (
	"encoding/json"
	"strings"

	"github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-orchestrator"
)

// capturedBy stamps every auto-recorded node so human curators can audit and
// prune what the extractor wrote.
const capturedBy = "llm-extractor"

type rawLink struct {
	To  string `json:"to"`
	Rel string `json:"rel"`
}

type rawCandidate struct {
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Domain     string    `json:"domain"`
	Tags       []string  `json:"tags"`
	Links      []rawLink `json:"links"`
	Private    bool      `json:"private"`
	Confidence float64   `json:"confidence"`
}

// parseCandidates tolerantly turns a model reply into candidates: it extracts the
// JSON array (ignoring any prose or code-fence wrapper), drops entries with no
// title or below threshold, caps the result at max (0 = uncapped), and never
// errors — a garbage reply yields nil so Consolidate stays best-effort.
func parseCandidates(reply string, threshold float64, max int) []orchestrator.Candidate {
	arr := extractJSONArray(reply)
	if arr == "" {
		return nil
	}
	var raws []rawCandidate
	if err := json.Unmarshal([]byte(arr), &raws); err != nil {
		return nil
	}
	var out []orchestrator.Candidate
	for _, r := range raws {
		title := strings.TrimSpace(r.Title)
		if title == "" || r.Confidence < threshold {
			continue
		}
		out = append(out, orchestrator.Candidate{Node: toNode(r, title), Private: r.Private})
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

// extractJSONArray returns the substring from the first '[' to the last ']'
// inclusive, so a fenced or prose-wrapped array still parses. Empty when absent.
func extractJSONArray(s string) string {
	i := strings.IndexByte(s, '[')
	j := strings.LastIndexByte(s, ']')
	if i < 0 || j < i {
		return ""
	}
	return s[i : j+1]
}

func toNode(r rawCandidate, title string) contracts.Node {
	meta := map[string]string{"capturedBy": capturedBy}
	if d := strings.TrimSpace(r.Domain); d != "" {
		meta["domain"] = d
	}
	if len(r.Tags) > 0 {
		meta["tags"] = strings.Join(r.Tags, ",")
	}
	var links []contracts.Link
	for _, l := range r.Links {
		if strings.TrimSpace(l.To) == "" {
			continue
		}
		rel := l.Rel
		if rel == "" {
			rel = contracts.RelAppliesTo
		}
		links = append(links, contracts.Link{To: l.To, Rel: rel})
	}
	kind := mapKind(r.Kind)
	return contracts.Node{
		Key:   stableKey(kind, title, r.Private),
		Kind:  kind,
		Title: title,
		Body:  r.Body,
		Links: links,
		Meta:  meta,
	}
}

// mapKind maps the model's kind string to a NodeKind, defaulting unknown or blank
// to KindSession (transient) rather than dropping the candidate.
func mapKind(s string) contracts.NodeKind {
	switch contracts.NodeKind(strings.ToLower(strings.TrimSpace(s))) {
	case contracts.KindOrganization:
		return contracts.KindOrganization
	case contracts.KindProject:
		return contracts.KindProject
	case contracts.KindRepo:
		return contracts.KindRepo
	case contracts.KindServer:
		return contracts.KindServer
	case contracts.KindArchitecture:
		return contracts.KindArchitecture
	case contracts.KindProduction:
		return contracts.KindProduction
	case contracts.KindDecision:
		return contracts.KindDecision
	case contracts.KindUser:
		return contracts.KindUser
	case contracts.KindAgent:
		return contracts.KindAgent
	case contracts.KindDomain:
		return contracts.KindDomain
	default:
		return contracts.KindSession
	}
}

// stableKey derives a deterministic Key so the same fact re-extracted in a later
// session upserts by Key instead of duplicating. Shared facts live under facts/,
// private skills under skills/.
func stableKey(kind contracts.NodeKind, title string, private bool) string {
	prefix := "facts/" + string(kind)
	if private {
		prefix = "skills"
	}
	return prefix + "/" + slug(title)
}

// slug lowercases title and keeps [a-z0-9], collapsing every other run into a
// single hyphen, so a Key is filesystem- and wikilink-safe.
func slug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test ./...`
Expected: PASS (`ok  github.com/Herrscherd/herrscher-llm-extractor`).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-llm-extractor
git add candidate.go candidate_test.go
git commit -q -m "feat: tolerant candidate parsing with stable keys and provenance"
```

---

## Task 3: Extraction prompt

**Files:**
- Create: `/home/shan/dev/herrscher-llm-extractor/prompt.go`
- Test: `/home/shan/dev/herrscher-llm-extractor/prompt_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-llm-extractor/prompt_test.go`:

```go
package llmextractor

import "testing"

func TestExtractionPrompt_CarriesJournalAndTranscript(t *testing.T) {
	p := extractionPrompt("JOURNAL-MARKER", "TRANSCRIPT-MARKER")
	if p.Content == "" {
		t.Fatal("empty prompt content")
	}
	for _, want := range []string{"JOURNAL-MARKER", "TRANSCRIPT-MARKER", "JSON array", "private", "confidence"} {
		if !contains(p.Content, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (indexOf(hay, needle) >= 0)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test -run TestExtractionPrompt ./...`
Expected: FAIL — `undefined: extractionPrompt`.

- [ ] **Step 3: Write `prompt.go`**

Create `/home/shan/dev/herrscher-llm-extractor/prompt.go`:

```go
package llmextractor

import (
	"strings"

	"github.com/Herrscherd/herrscher-contracts"
)

// extractionPrompt builds the curation prompt: it hands the model the call
// journal and session transcript and asks for a JSON array of memory candidates,
// mirroring herrscher's own memory-writing guidance (one fact per node,
// Why/How-to-apply for decisions, link liberally, mark agent-private skills).
func extractionPrompt(journal, transcript string) contracts.Prompt {
	var b strings.Builder
	b.WriteString(instructions)
	b.WriteString("\n\n## Call journal\n\n")
	b.WriteString(journal)
	b.WriteString("\n\n## Session transcript\n\n")
	b.WriteString(transcript)
	return contracts.Prompt{Content: b.String()}
}

const instructions = `You are herrscher's memory curator. From the work below,
distill only what is worth remembering for future sessions. Return ONLY a JSON
array (no prose, no code fence) of candidate memory nodes:

[{"kind":"decision|architecture|user|production|session",
  "title":"short stable title",
  "body":"the fact in markdown; for a decision add **Why:** and **How to apply:**",
  "domain":"dev",
  "tags":["nats","transport"],
  "links":[{"to":"projects/x/index","rel":"applies-to"}],
  "private":false,
  "confidence":0.0}]

Rules:
- One durable fact per element; concise, stable titles.
- private=true for a skill this agent learned; private=false for a fact every
  agent of the project should share.
- confidence is 0..1; omit anything you are not sure is durable.
- Prefer decisions, architecture, user preferences, production facts over
  transient chatter.
- If nothing is worth remembering, return [].`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test -run TestExtractionPrompt ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-llm-extractor
git add prompt.go prompt_test.go
git commit -q -m "feat: extraction prompt mirroring memory-writing guidance"
```

---

## Task 4: LLMExtractor over an explicit backend

**Files:**
- Create: `/home/shan/dev/herrscher-llm-extractor/extractor.go`
- Test: `/home/shan/dev/herrscher-llm-extractor/extractor_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-llm-extractor/extractor_test.go`:

```go
package llmextractor

import (
	"context"
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-orchestrator"
)

type fakeBackend struct {
	reply  string
	err    error
	got    contracts.Prompt
	closed bool
}

func (f *fakeBackend) Respond(_ context.Context, p contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	f.got = p
	return f.reply, f.err
}

func (f *fakeBackend) Close() error { f.closed = true; return nil }

// compile-time proof the type satisfies the seam.
var _ orchestrator.Extractor = (*LLMExtractor)(nil)

func TestExtract_HappyPath(t *testing.T) {
	fb := &fakeBackend{reply: twoValid}
	cs, err := New(fb).Extract(context.Background(), "journal", "transcript")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("want 2, got %d", len(cs))
	}
	if !contains(fb.got.Content, "transcript") {
		t.Fatal("transcript not passed to backend")
	}
}

func TestExtract_EmptyInputsIsNoOp(t *testing.T) {
	fb := &fakeBackend{reply: twoValid}
	cs, err := New(fb).Extract(context.Background(), "  ", "")
	if err != nil || cs != nil {
		t.Fatalf("empty in → want (nil,nil), got (%v,%v)", cs, err)
	}
	if fb.got.Content != "" {
		t.Fatal("backend should not be called on empty input")
	}
}

func TestExtract_BackendErrorPropagates(t *testing.T) {
	cs, err := New(&fakeBackend{err: errors.New("boom")}).Extract(context.Background(), "j", "t")
	if err == nil || cs != nil {
		t.Fatalf("want error, got (%v,%v)", cs, err)
	}
}

func TestExtract_NilBackendIsNoOp(t *testing.T) {
	cs, err := (&LLMExtractor{}).Extract(context.Background(), "j", "t")
	if err != nil || cs != nil {
		t.Fatalf("nil backend → want (nil,nil), got (%v,%v)", cs, err)
	}
}

func TestOptions_ThresholdAndMax(t *testing.T) {
	e := New(&fakeBackend{reply: twoValid}, WithThreshold(0.85), WithMax(5))
	if e.threshold != 0.85 || e.max != 5 {
		t.Fatalf("options not applied: %v %v", e.threshold, e.max)
	}
	cs, _ := e.Extract(context.Background(), "j", "t")
	if len(cs) != 1 {
		t.Fatalf("threshold via Extract: want 1, got %d", len(cs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test -run 'TestExtract|TestOptions' ./...`
Expected: FAIL — `undefined: New` / `undefined: LLMExtractor`.

- [ ] **Step 3: Write `extractor.go`**

Create `/home/shan/dev/herrscher-llm-extractor/extractor.go`:

```go
package llmextractor

import (
	"context"
	"strings"
	"sync"

	"github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-orchestrator"
)

// LLMExtractor is the open, reference Extractor: it asks a contracts.Backend to
// distill a stretch of work (journal + transcript) into memory candidates. The
// backend is either injected (New) or built lazily from the plugin registry on
// first use (the registered default — see register.go / backend.go).
type LLMExtractor struct {
	backend    contracts.Backend
	newBackend func() (contracts.Backend, error) // lazy source when backend is nil
	once       sync.Once
	threshold  float64
	max        int
}

var _ orchestrator.Extractor = (*LLMExtractor)(nil)

// Option configures an LLMExtractor.
type Option func(*LLMExtractor)

// WithThreshold drops candidates below the given confidence (default 0.6).
func WithThreshold(t float64) Option { return func(e *LLMExtractor) { e.threshold = t } }

// WithMax caps candidates recorded per Consolidate (0 = uncapped; default 8).
func WithMax(m int) Option { return func(e *LLMExtractor) { e.max = m } }

// New builds an extractor over an explicit backend (tests and callers that
// already hold a model edge).
func New(b contracts.Backend, opts ...Option) *LLMExtractor {
	e := &LLMExtractor{backend: b, threshold: defaultThreshold, max: defaultMax}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Extract asks the curation backend to distill journal + transcript into
// candidates. It is best-effort: no backend or empty inputs yield a clean no-op;
// a bad JSON reply yields no candidates without erroring.
func (e *LLMExtractor) Extract(ctx context.Context, journal, transcript string) ([]orchestrator.Candidate, error) {
	b := e.resolveBackend()
	if b == nil || (strings.TrimSpace(journal) == "" && strings.TrimSpace(transcript) == "") {
		return nil, nil
	}
	raw, err := b.Respond(ctx, extractionPrompt(journal, transcript), nil)
	if err != nil {
		return nil, err
	}
	return parseCandidates(raw, e.threshold, e.max), nil
}

// resolveBackend returns the injected backend, or lazily builds one from
// newBackend exactly once. A build error leaves the backend nil (no-op degrade).
func (e *LLMExtractor) resolveBackend() contracts.Backend {
	if e.backend != nil {
		return e.backend
	}
	if e.newBackend == nil {
		return nil
	}
	e.once.Do(func() {
		if b, err := e.newBackend(); err == nil {
			e.backend = b
		}
	})
	return e.backend
}
```

Note: `defaultThreshold`/`defaultMax` are defined in Task 6's `register.go`. Until then this file references undefined consts. To keep this task self-contained and compilable, add them here temporarily is **wrong** — instead, define them now in a tiny file so both tasks share them:

- [ ] **Step 3b: Create the shared defaults file**

Create `/home/shan/dev/herrscher-llm-extractor/defaults.go`:

```go
package llmextractor

// Tuning defaults for the registered extractor and New's zero-option form.
const (
	// defaultThreshold drops low-confidence candidates.
	defaultThreshold = 0.6
	// defaultMax bounds candidates recorded per Consolidate (cost guard).
	defaultMax = 8
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test -run 'TestExtract|TestOptions' ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-llm-extractor
git add extractor.go defaults.go extractor_test.go
git commit -q -m "feat: LLMExtractor over an injectable backend with threshold/max options"
```

---

## Task 5: Registry-driven backend acquisition

**Files:**
- Create: `/home/shan/dev/herrscher-llm-extractor/backend.go`
- Test: `/home/shan/dev/herrscher-llm-extractor/backend_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-llm-extractor/backend_test.go`:

```go
package llmextractor

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestCurationEnv_OverridesModelKeysOnly(t *testing.T) {
	base := map[string]string{"CLAUDE_MODEL": "expensive", "CLAUDE_CMD": "claude"}
	env := curationEnv(func(k string) string {
		if k == "HERRSCHER_CURATION_MODEL" {
			return "cheap"
		}
		return base[k]
	})
	if got := env("CLAUDE_MODEL"); got != "cheap" {
		t.Fatalf("model key not overridden: %q", got)
	}
	if got := env("CLAUDE_CMD"); got != "claude" {
		t.Fatalf("non-model key changed: %q", got)
	}
}

func TestCurationEnv_NoOverridePassesThrough(t *testing.T) {
	env := curationEnv(func(k string) string {
		if k == "CLAUDE_MODEL" {
			return "default-model"
		}
		return ""
	})
	if got := env("CLAUDE_MODEL"); got != "default-model" {
		t.Fatalf("passthrough broken: %q", got)
	}
}

func TestBackendFrom_BuildsFirstBackendPlugin(t *testing.T) {
	built := &fakeBackend{}
	plugins := []contracts.Plugin{
		{Manifest: contracts.Manifest{Category: contracts.CategoryBackend, Config: []contracts.Setting{
			{Key: "model", Env: "CLAUDE_MODEL"},
		}}, Backend: func(_ context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			if cfg.Get("model") != "cheap" {
				t.Fatalf("curation model not resolved into cfg: %q", cfg.Get("model"))
			}
			return built, nil
		}},
	}
	env := curationEnv(func(k string) string {
		if k == "HERRSCHER_CURATION_MODEL" {
			return "cheap"
		}
		return ""
	})
	b, err := backendFrom(plugins, env)
	if err != nil {
		t.Fatalf("backendFrom: %v", err)
	}
	if b != built {
		t.Fatal("did not return the built backend")
	}
}

func TestBackendFrom_NoBackendRegisteredIsNoOp(t *testing.T) {
	b, err := backendFrom(nil, curationEnv(func(string) string { return "" }))
	if b != nil || err != nil {
		t.Fatalf("want (nil,nil), got (%v,%v)", b, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test -run 'TestCurationEnv|TestBackendFrom' ./...`
Expected: FAIL — `undefined: curationEnv` / `undefined: backendFrom`.

- [ ] **Step 3: Write `backend.go`**

Create `/home/shan/dev/herrscher-llm-extractor/backend.go`:

```go
package llmextractor

import (
	"context"
	"os"
	"strings"

	"github.com/Herrscherd/herrscher-contracts"
)

// curationEnv wraps a base getenv so any model key resolves to
// HERRSCHER_CURATION_MODEL when that override is set — letting curation run on a
// cheaper/faster model than the conversation backend. Every other key passes
// through unchanged. A model key is any env var whose name ends in "MODEL"
// (e.g. CLAUDE_MODEL), matching how backends name their model setting.
func curationEnv(base func(string) string) func(string) string {
	override := base("HERRSCHER_CURATION_MODEL")
	return func(key string) string {
		if override != "" && strings.HasSuffix(key, "MODEL") {
			return override
		}
		return base(key)
	}
}

// backendFrom builds the first backend plugin in plugins from its resolved env
// config, mirroring the host's firstBackend (Resolve(manifest.Config, getenv) →
// factory). Returns (nil, nil) when no backend plugin is registered, so curation
// degrades to a clean no-op and recall keeps working.
func backendFrom(plugins []contracts.Plugin, getenv func(string) string) (contracts.Backend, error) {
	for _, p := range plugins {
		if p.Backend == nil {
			continue
		}
		cfg, err := contracts.Resolve(p.Manifest.Config, getenv)
		if err != nil {
			return nil, err
		}
		return p.Backend(context.Background(), cfg)
	}
	return nil, nil
}

// lazyBackend builds a curation backend from the global plugin registry using a
// model-overriding view of the process environment. Used by the registered
// default extractor on first Extract.
func lazyBackend() (contracts.Backend, error) {
	return backendFrom(contracts.Default.Backends(), curationEnv(os.Getenv))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test -run 'TestCurationEnv|TestBackendFrom' ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-llm-extractor
git add backend.go backend_test.go
git commit -q -m "feat: lazy curation backend from the plugin registry with model override"
```

---

## Task 6: Register the default under "llm"

**Files:**
- Create: `/home/shan/dev/herrscher-llm-extractor/register.go`
- Test: `/home/shan/dev/herrscher-llm-extractor/register_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-llm-extractor/register_test.go`:

```go
package llmextractor

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher-orchestrator"
)

// The registry is package-private in orchestrator; we verify registration by
// observing that a Learner built with extractor name "llm" is wired. Since
// lookupExtractor is unexported there, we instead assert newDefault produces a
// usable, lazily-backed extractor that no-ops cleanly with no backend registered.
func TestNewDefault_IsLazyAndNoOpsWithoutBackend(t *testing.T) {
	e := newDefault()
	if e.threshold != defaultThreshold || e.max != defaultMax {
		t.Fatalf("defaults not applied: %v %v", e.threshold, e.max)
	}
	if e.newBackend == nil {
		t.Fatal("registered default must have a lazy backend source")
	}
	// With no backend plugin registered in this test binary, lazyBackend yields
	// nil, so Extract is a clean no-op rather than a panic.
	cs, err := e.Extract(context.Background(), "journal", "transcript")
	if err != nil || cs != nil {
		t.Fatalf("want clean no-op, got (%v,%v)", cs, err)
	}
}

// compile-time proof the registered value satisfies the seam.
var _ orchestrator.Extractor = newDefault()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test -run TestNewDefault ./...`
Expected: FAIL — `undefined: newDefault`.

- [ ] **Step 3: Write `register.go`**

Create `/home/shan/dev/herrscher-llm-extractor/register.go`:

```go
package llmextractor

import "github.com/Herrscherd/herrscher-orchestrator"

// init registers the open reference extractor under "llm" so a host that blank-
// imports this package and passes `--extractor llm` opts into auto-capture. This
// is the xcaddy pattern: the orchestrator's register.go looks the name up at
// session construction.
func init() {
	orchestrator.RegisterExtractor("llm", newDefault())
}

// newDefault builds the registered extractor: tuning defaults, with its curation
// backend resolved lazily from the plugin registry on first use.
func newDefault() *LLMExtractor {
	return &LLMExtractor{newBackend: lazyBackend, threshold: defaultThreshold, max: defaultMax}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test ./...`
Expected: PASS (all suites).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-llm-extractor
git add register.go register_test.go
git commit -q -m "feat: register the llm extractor via init (xcaddy pattern)"
```

---

## Task 7: End-to-end integration through the real Learner

**Files:**
- Create: `/home/shan/dev/herrscher-llm-extractor/integration_test.go`

- [ ] **Step 1: Write the failing test**

This drives the **real** `orchestrator.NewLearner` with our fake-backed extractor and a tiny in-memory `contracts.Memory`, proving Consolidate records the right nodes under the right scope. It exercises the actual scope helpers and the every-N-turns trigger.

Create `/home/shan/dev/herrscher-llm-extractor/integration_test.go`:

```go
package llmextractor

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-orchestrator"
)

// memStub is a minimal in-memory Memory: enough for the Learner's Consolidate
// (Recall the session transcript; Record + Links candidates under scope roots).
type memStub struct {
	nodes map[string]contracts.Node
	edges []contracts.Link // To carries the edge target; From tracked separately
	links [][3]string      // {from, to, rel}
}

func newMemStub(transcript string) *memStub {
	return &memStub{nodes: map[string]contracts.Node{
		"sess-1": {Key: "sess-1", Kind: contracts.KindSession, Body: transcript},
	}}
}

func (m *memStub) Recall(_ context.Context, key string, _ int) (contracts.Subgraph, error) {
	return contracts.Subgraph{Root: m.nodes[key]}, nil
}
func (m *memStub) Record(_ context.Context, n contracts.Node) error {
	if m.nodes == nil {
		m.nodes = map[string]contracts.Node{}
	}
	m.nodes[n.Key] = n
	return nil
}
func (m *memStub) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return nil, nil
}
func (m *memStub) Links(_ context.Context, from, to, rel string) error {
	m.links = append(m.links, [3]string{from, to, rel})
	return nil
}
func (m *memStub) Close() error { return nil }

func TestLearnerConsolidate_RecordsScopedNodes(t *testing.T) {
	mem := newMemStub("we decided to use NATS; agent learned a flock trick")
	scope := contracts.MemoryScope{Project: "projects/game", Agent: "agents/bob"}
	ex := New(&fakeBackend{reply: twoValid})

	// journal="" is fine: transcript alone is non-empty, so Extract runs.
	learner := orchestrator.NewLearner(mem, "sess-1", scope, ex, "/nonexistent/journal.log", 0)

	if err := learner.Consolidate(context.Background()); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	// Shared fact recorded and linked under the project root.
	if _, ok := mem.nodes["facts/decision/use-nats"]; !ok {
		t.Fatalf("shared fact not recorded; nodes: %v", keys(mem.nodes))
	}
	// Private skill recorded and linked under the agent root.
	if _, ok := mem.nodes["skills/learned-flock-trick"]; !ok {
		t.Fatalf("private skill not recorded; nodes: %v", keys(mem.nodes))
	}
	assertLink(t, mem.links, "projects/game", "facts/decision/use-nats", contracts.RelContains)
	assertLink(t, mem.links, "agents/bob", "skills/learned-flock-trick", contracts.RelContains)
}

func keys(m map[string]contracts.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func assertLink(t *testing.T, links [][3]string, from, to, rel string) {
	t.Helper()
	for _, l := range links {
		if l[0] == from && l[1] == to && l[2] == rel {
			return
		}
	}
	t.Fatalf("missing link %s -%s-> %s in %v", from, rel, to, links)
}
```

- [ ] **Step 2: Run test to verify it fails, then passes**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test -run TestLearnerConsolidate ./...`
Expected: PASS (all production code already exists; this is a wiring proof). If it FAILS, the failure pinpoints a scope/key mismatch to fix in `candidate.go` — not new production code.

- [ ] **Step 3: Run the full suite with the race detector**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go test -race ./...`
Expected: PASS, no race warnings.

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher-llm-extractor
git add integration_test.go
git commit -q -m "test: end-to-end Consolidate through the real Learner records scoped nodes"
```

---

## Task 8: README + CI

**Files:**
- Create: `/home/shan/dev/herrscher-llm-extractor/README.md`
- Create: `/home/shan/dev/herrscher-llm-extractor/.github/workflows/ci.yml`

- [ ] **Step 1: Write the README**

Create `/home/shan/dev/herrscher-llm-extractor/README.md`:

```markdown
# herrscher-llm-extractor

The open, reference **memory curator** for [herrscher](https://github.com/Herrscherd/herrscher):
a generic, LLM-driven `orchestrator.Extractor`. It turns a session's call journal
and transcript into durable memory nodes — shared **facts** (under the project)
and private **skills** (under the agent) — so herrscher self-populates its vault.

## Wire it in

Blank-import the package into a herrscher host (xcaddy pattern):

    import _ "github.com/Herrscherd/herrscher-llm-extractor"

Then opt a session into auto-capture:

    session create --extractor llm --journal <worktree>/.neublox/calls.log --consolidate-every 10

The nudge (every-N-turns `Consolidate`) is owned by the
[orchestrator](https://github.com/Herrscherd/herrscher-orchestrator); this package
only supplies the extractor it calls.

## Config

- `HERRSCHER_CURATION_MODEL` (optional) — run curation on a cheaper/faster model
  than the conversation backend. Overrides any `*_MODEL` env key the registered
  backend reads.

## What it writes

Each candidate becomes a `contracts.Node` with a **stable Key**
(`facts/<kind>/<slug>` or `skills/<slug>`) so re-extraction upserts instead of
duplicating, and `Meta["capturedBy"]="llm-extractor"` for human audit and pruning.

The Roblox-specific curation heuristics are a separate, **closed** extractor; this
is the reusable default that ships in the open.
```

- [ ] **Step 2: Write the CI workflow** (mirror the PR-1 workflow — go 1.25, build+vet+test-race)

Because private-module fetch needs auth in CI, the workflow keeps the same shape as the other repos. Create `/home/shan/dev/herrscher-llm-extractor/.github/workflows/ci.yml` via Bash heredoc (the Write tool refuses unread new files under some setups):

```bash
mkdir -p /home/shan/dev/herrscher-llm-extractor/.github/workflows
cat > /home/shan/dev/herrscher-llm-extractor/.github/workflows/ci.yml <<'YML'
name: ci
on:
  push:
    branches: [master]
  pull_request:
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      GOPRIVATE: github.com/Herrscherd/*
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true
      - name: configure private module auth
        run: git config --global url."https://x-access-token:${{ secrets.GITHUB_TOKEN }}@github.com/".insteadOf "https://github.com/"
      - run: go build ./...
      - run: go vet ./...
      - run: go test -race ./...
YML
```

- [ ] **Step 3: Verify the suite still builds locally**

Run: `cd /home/shan/dev/herrscher-llm-extractor && go build ./... && go vet ./... && go test ./...`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher-llm-extractor
git add README.md .github/workflows/ci.yml
git commit -q -m "docs+ci: README and build/vet/test-race workflow"
```

---

## Task 9: Release (USER-GATED — do not run without approval)

Releasing is outward-facing. **Stop and confirm the version with the user before running.** The target tag is `v0.1.0` (first release of a new repo).

**Files:**
- Modify: `/home/shan/dev/herrscher-llm-extractor/go.mod` (drop dev replaces)

- [ ] **Step 1: Drop the dev-only replace directives**

```bash
cd /home/shan/dev/herrscher-llm-extractor
go mod edit -dropreplace github.com/Herrscherd/herrscher-contracts
go mod edit -dropreplace github.com/Herrscherd/herrscher-orchestrator
```

- [ ] **Step 2: Tidy against the real tagged modules**

```bash
cd /home/shan/dev/herrscher-llm-extractor
GOPRIVATE='github.com/Herrscherd/*' GOFLAGS=-mod=mod go mod tidy
```
Expected: `go.mod` now requires `herrscher-contracts v0.1.9` and
`herrscher-orchestrator v0.1.4` with no replaces; `go.sum` populated.

- [ ] **Step 3: Full gate against tagged deps**

Run: `cd /home/shan/dev/herrscher-llm-extractor && GOPRIVATE='github.com/Herrscherd/*' go build ./... && go vet ./... && go test -race ./...`
Expected: all PASS with real modules.

- [ ] **Step 4: Commit the go.mod/go.sum and confirm the version with the user**

```bash
cd /home/shan/dev/herrscher-llm-extractor
git add go.mod go.sum
git commit -q -m "build: pin herrscher-contracts v0.1.9 + herrscher-orchestrator v0.1.4 (drop dev replaces)"
```

- [ ] **Step 5: Create the GitHub repo, push, tag (after user says go)**

```bash
cd /home/shan/dev/herrscher-llm-extractor
gh repo create Herrscherd/herrscher-llm-extractor --private --source=. --remote=origin --push
git tag v0.1.0
git push origin v0.1.0
```

---

## Task 10: Host wiring (USER-GATED — after Task 9 tag exists)

**Files:**
- Modify: `/home/shan/dev/herrscher/go.mod`
- Modify: the host's generated plugin-import list (find in Step 1)

- [ ] **Step 1: Locate the host's plugin blank-import list**

Run: `grep -rln '_ "github.com/Herrscherd/herrscher-orchestrator"' /home/shan/dev/herrscher --include='*.go'`
Expected: the file (e.g. `plugins.go`) that blank-imports the orchestrator. This is where the extractor import goes, next to it.

- [ ] **Step 2: Add the extractor to require + blank import**

```bash
cd /home/shan/dev/herrscher
GOPRIVATE='github.com/Herrscherd/*' GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-llm-extractor@v0.1.0
```

Then add to the plugin-import file found in Step 1, alongside the orchestrator import:

```go
	_ "github.com/Herrscherd/herrscher-llm-extractor"
```

- [ ] **Step 3: Build the host**

Run: `cd /home/shan/dev/herrscher && GOPRIVATE='github.com/Herrscherd/*' go build ./...`
Expected: builds. The `"llm"` extractor is now registered; a session started with
`--extractor llm --journal ... --consolidate-every N` will self-populate memory.

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher
git add go.mod go.sum <plugin-import-file>
git commit -m "feat(memory): wire herrscher-llm-extractor — auto-capture via --extractor llm"
```

---

## Self-Review

**1. Spec coverage:**
- Module layout (spec §1) → Task 1. ✓
- `LLMExtractor`/`New`/`Extract`/`Option`s (spec §2) → Task 4. ✓
- Lazy backend from registry + `curationEnv` (spec §3) → Task 5. ✓
- Extraction prompt → JSON, tolerant parse, stable keys, provenance, kind fallback (spec §4) → Tasks 2 (parse/keys/kind/provenance) + 3 (prompt). ✓
- Host wiring = one blank import, nudge already plumbed (spec §5) → Task 10. ✓
- Testing list (spec) → Tasks 2/4/5/6 cover threshold, cap, malformed, garbage, empty-input no-op, nil backend, stable key; Task 7 covers the real-Learner integration. ✓
- Provenance `originSessionId` deferred (spec §4) → intentionally not implemented; `capturedBy` stamped instead. ✓
- `max` truncation logging (spec open question) → **gap:** spec says "log when truncated rather than silently cap." The Learner does the persisting and can't see the cap; the extractor caps in `parseCandidates`. Left unlogged for now (no logger dependency in this pure package) — acceptable because `max` is a soft cost guard, not a correctness cap, and the spec flagged it non-blocking. Noted here rather than adding a logging dep.

**2. Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to Task N". Every code step shows full code. Step 3b in Task 4 resolves the forward-reference to `defaultThreshold`/`defaultMax` by defining them in `defaults.go` before `register.go` needs them. ✓

**3. Type consistency:**
- `parseCandidates(reply string, threshold float64, max int)` — same signature in Task 2 def, Task 4 use. ✓
- `New(b contracts.Backend, opts ...Option)`, `WithThreshold`, `WithMax` — consistent Tasks 4/6. ✓
- `LLMExtractor` fields (`backend`, `newBackend`, `once`, `threshold`, `max`) — defined Task 4, used Tasks 4/6. ✓
- `lazyBackend`/`backendFrom`/`curationEnv` — defined Task 5, referenced Tasks 4-comment/6. ✓
- Keys `facts/decision/use-nats`, `skills/learned-flock-trick` — asserted identically in Tasks 2 and 7 (derived from `twoValid`). ✓
- `orchestrator.NewLearner(mem, session, scope, ex, journal, every)` — matches verified source signature. ✓
