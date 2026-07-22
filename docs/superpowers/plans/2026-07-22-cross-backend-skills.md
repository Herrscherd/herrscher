# Cross-backend Skills Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make any `SKILL.md` skill usable from every Herrscher backend by injecting a skills menu into the prompt context and expanding a skill's body on demand, for backends that do not load skills natively.

**Architecture:** A pure `core/skills` package discovers `SKILL.md` files and runs a progressive-disclosure `Engine` (menu → on-demand body). The bridge hub builds one `Engine` per session (cwd = workspace, so `os.Getwd()` finds repo skills), augments `Prompt.Context` each turn, and scans the reply for activation markers. A new `contracts.SkillNative` interface lets the claude backend opt out (its CLI already loads skills).

**Tech Stack:** Go 1.25, `herrscher-contracts`, `herrscher` core (`core/bridge`, `core/config`), `herrscher-claude-backend`. Standard library only (no YAML dep — a minimal frontmatter parser).

## Global Constraints

- Go module floor: `go 1.25.0`.
- No code comments beyond the existing house style (doc comments on exported identifiers only; no inline `//` narration of obvious code).
- No lint suppressions — fix the root cause.
- Commit messages: conventional (`feat(skills): …`, `test(skills): …`), NO `Co-Authored-By: Claude` trailer.
- Prefer classes/OOP: the engine is a struct with methods, not free functions over shared state.
- Run tests with `GOWORK=off` inside each module for a faithful release build; a final task verifies the `go.work` build too.

---

### Task 1: Skill discovery in `core/skills`

**Files:**
- Create: `core/skills/skill.go`
- Create: `core/skills/discover.go`
- Test: `core/skills/discover_test.go`

**Interfaces:**
- Produces:
  - `type Skill struct { Name, Description, Dir, bodyPath string }`
  - `func (s Skill) Body() (string, error)` — reads `bodyPath` lazily.
  - `func Discover(roots []string) []Skill` — scans each root for `*/SKILL.md`, de-dupes by `Name` (first root wins), skips malformed entries.
  - `func parseFrontmatter(md []byte) (name, description string, ok bool)` — minimal `---`-fenced `key: value` parser.

- [ ] **Step 1: Write the failing test**

```go
package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, root, name, front, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\n" + front + "---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverParsesAndDedupes(t *testing.T) {
	repo := t.TempDir()
	global := t.TempDir()
	writeSkill(t, repo, "pdf-fill", "name: pdf-fill\ndescription: fill PDFs\n", "step one\n")
	writeSkill(t, global, "pdf-fill", "name: pdf-fill\ndescription: GLOBAL loses\n", "x\n")
	writeSkill(t, global, "web", "name: web\ndescription: browse\n", "y\n")
	writeSkill(t, repo, "broken", "no-frontmatter-here\n", "z\n")

	got := Discover([]string{repo, global})
	if len(got) != 2 {
		t.Fatalf("want 2 skills (deduped, malformed skipped), got %d: %+v", len(got), got)
	}
	byName := map[string]Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if byName["pdf-fill"].Description != "fill PDFs" {
		t.Fatalf("repo root must win de-dup, got %q", byName["pdf-fill"].Description)
	}
	body, err := byName["pdf-fill"].Body()
	if err != nil || body != "step one\n" {
		t.Fatalf("Body() = %q, %v", body, err)
	}
	if _, ok := byName["web"]; !ok {
		t.Fatalf("global-only skill missing: %+v", byName)
	}
}

func TestDiscoverMissingRootIsSkipped(t *testing.T) {
	got := Discover([]string{filepath.Join(t.TempDir(), "does-not-exist")})
	if len(got) != 0 {
		t.Fatalf("missing root should yield no skills, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd core/skills && GOWORK=off go test ./... 2>&1 | tail`
Expected: build failure — `undefined: Discover` / `undefined: Skill`.

- [ ] **Step 3: Write minimal implementation**

`core/skills/skill.go`:

```go
// Package skills discovers SKILL.md skills and runs a progressive-disclosure
// engine that injects a skill menu into a turn and expands a skill's body on
// demand — so backends that do not load skills natively can still use them.
package skills

import "os"

// Skill is one discovered SKILL.md: its frontmatter name/description, the
// directory holding it (so an expanded body can point the model at bundled
// resource files by absolute path), and the path to the markdown body.
type Skill struct {
	Name        string
	Description string
	Dir         string
	bodyPath    string
}

// Body reads the skill's markdown body (everything after the frontmatter is not
// stripped — the full file is returned so the model sees the complete SKILL.md).
func (s Skill) Body() (string, error) {
	b, err := os.ReadFile(s.bodyPath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
```

`core/skills/discover.go`:

```go
package skills

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// Discover scans each root for immediate <root>/<name>/SKILL.md entries, parses
// their frontmatter, and returns the valid skills. De-duplication is by Name
// with earlier roots winning, so a repo skill overrides a global one of the same
// name. A missing root, an unreadable entry, or a SKILL.md without a name is
// skipped, never fatal.
func Discover(roots []string) []Skill {
	var out []Skill
	seen := map[string]bool{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			path := filepath.Join(dir, "SKILL.md")
			md, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			name, desc, ok := parseFrontmatter(md)
			if !ok || name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, Skill{Name: name, Description: desc, Dir: dir, bodyPath: path})
		}
	}
	return out
}

// parseFrontmatter reads a leading --- fenced block of key: value lines and
// returns the name and description. ok is false when the file does not open with
// a --- fence.
func parseFrontmatter(md []byte) (name, description string, ok bool) {
	sc := bufio.NewScanner(bytes.NewReader(md))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", "", false
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "---" {
			return name, description, true
		}
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			name = strings.TrimSpace(val)
		case "description":
			description = strings.TrimSpace(val)
		}
	}
	return "", "", false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd core/skills && GOWORK=off go test ./... -v 2>&1 | tail`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add core/skills/skill.go core/skills/discover.go core/skills/discover_test.go
git commit -m "feat(skills): discover SKILL.md skills with root-priority de-dup"
```

---

### Task 2: Progressive-disclosure `Engine`

**Files:**
- Create: `core/skills/engine.go`
- Test: `core/skills/engine_test.go`

**Interfaces:**
- Consumes: `Skill`, `Discover` (Task 1).
- Produces:
  - `type Engine struct { … }` (unexported fields).
  - `func NewEngine(roots []string) *Engine` — discovers once; a `*Engine` with no skills is a valid no-op.
  - `func (e *Engine) Menu() string` — `""` when no skills; else a `<skills>` block listing `name: description` + the activation instruction.
  - `func (e *Engine) Detect(reply string)` — activate every known skill named by a `<use-skill>NAME</use-skill>` marker in reply.
  - `func (e *Engine) Expansions() string` — `""` when nothing active; else each active skill's body inside `<skill name=".." dir="..">…</skill>`.

- [ ] **Step 1: Write the failing test**

```go
package skills

import (
	"strings"
	"testing"
)

func TestEngineMenuDetectExpand(t *testing.T) {
	repo := t.TempDir()
	writeSkill(t, repo, "pdf-fill", "name: pdf-fill\ndescription: fill PDFs\n", "FILL THE PDF\n")
	writeSkill(t, repo, "web", "name: web\ndescription: browse\n", "BROWSE\n")

	e := NewEngine([]string{repo})

	menu := e.Menu()
	for _, want := range []string{"<skills>", "pdf-fill: fill PDFs", "web: browse", "<use-skill>"} {
		if !strings.Contains(menu, want) {
			t.Fatalf("menu missing %q:\n%s", want, menu)
		}
	}
	if e.Expansions() != "" {
		t.Fatalf("nothing active yet, want empty expansions, got %q", e.Expansions())
	}

	e.Detect("sure, I'll use it <use-skill> pdf-fill </use-skill> now")
	exp := e.Expansions()
	if !strings.Contains(exp, "FILL THE PDF") {
		t.Fatalf("active skill body missing:\n%s", exp)
	}
	if !strings.Contains(exp, `name="pdf-fill"`) || !strings.Contains(exp, repo) {
		t.Fatalf("expansion should carry name + abs dir:\n%s", exp)
	}
	if strings.Contains(exp, "BROWSE") {
		t.Fatalf("only activated skill should expand:\n%s", exp)
	}
}

func TestEngineUnknownMarkerIgnored(t *testing.T) {
	e := NewEngine([]string{t.TempDir()})
	e.Detect("<use-skill>nope</use-skill>")
	if e.Expansions() != "" {
		t.Fatalf("unknown skill must not activate, got %q", e.Expansions())
	}
	if e.Menu() != "" {
		t.Fatalf("no skills discovered, menu must be empty, got %q", e.Menu())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd core/skills && GOWORK=off go test ./... -run Engine 2>&1 | tail`
Expected: build failure — `undefined: NewEngine`.

- [ ] **Step 3: Write minimal implementation**

`core/skills/engine.go`:

```go
package skills

import (
	"regexp"
	"strings"
)

// useMarker matches an activation marker the model emits to request a skill,
// tolerant of surrounding whitespace and case: <use-skill> NAME </use-skill>.
var useMarker = regexp.MustCompile(`(?i)<\s*use-skill\s*>\s*([^<]+?)\s*<\s*/\s*use-skill\s*>`)

// Engine runs progressive disclosure for one session: it holds the discovered
// skills and the set the model has activated. Menu is injected every turn (cheap
// name+description lines); Expansions carries the full body of activated skills.
type Engine struct {
	byName map[string]Skill
	order  []string
	active map[string]bool
}

// NewEngine discovers skills under roots and returns an engine over them. An
// engine with no skills is a valid no-op (empty Menu/Expansions).
func NewEngine(roots []string) *Engine {
	e := &Engine{byName: map[string]Skill{}, active: map[string]bool{}}
	for _, s := range Discover(roots) {
		e.byName[s.Name] = s
		e.order = append(e.order, s.Name)
	}
	return e
}

// Menu renders the activation instruction and one line per discovered skill. It
// returns "" when no skills exist so the caller injects nothing.
func (e *Engine) Menu() string {
	if len(e.order) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<skills data-only=\"true\">\n")
	b.WriteString("Skills available for this session. To use one, output the marker <use-skill>NAME</use-skill> in your reply; its full instructions arrive next turn.\n")
	for _, name := range e.order {
		b.WriteString("- ")
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(e.byName[name].Description)
		b.WriteByte('\n')
	}
	b.WriteString("</skills>")
	return b.String()
}

// Detect activates every known skill named by a marker in reply. Unknown names
// are ignored.
func (e *Engine) Detect(reply string) {
	for _, m := range useMarker.FindAllStringSubmatch(reply, -1) {
		name := strings.TrimSpace(m[1])
		if _, ok := e.byName[name]; ok {
			e.active[name] = true
		}
	}
}

// Expansions returns the bodies of all active skills, each fenced with its name
// and absolute directory so the model can Read bundled files. A body that fails
// to load is skipped. Returns "" when nothing is active.
func (e *Engine) Expansions() string {
	var b strings.Builder
	for _, name := range e.order {
		if !e.active[name] {
			continue
		}
		s := e.byName[name]
		body, err := s.Body()
		if err != nil {
			continue
		}
		b.WriteString("<skill name=\"")
		b.WriteString(name)
		b.WriteString("\" dir=\"")
		b.WriteString(s.Dir)
		b.WriteString("\">\n")
		b.WriteString(body)
		b.WriteString("\n</skill>\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd core/skills && GOWORK=off go test ./... 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/skills/engine.go core/skills/engine_test.go
git commit -m "feat(skills): progressive-disclosure engine (menu, detect, expand)"
```

---

### Task 3: `contracts.SkillNative` + claude backend opt-out

**Files:**
- Create: `herrscher-contracts/skill.go`
- Test: `herrscher-contracts/skill_test.go`
- Modify: `herrscher-claude-backend/stream.go` (add method to `streamResponder` and `oneShotResponder`)
- Test: `herrscher-claude-backend/backend_test.go` (append a case)

**Interfaces:**
- Produces: `type SkillNative interface { NativeSkills() bool }`.
- Consumes (later, Task 5): the hub type-asserts a backend to `SkillNative`.

- [ ] **Step 1: Write the failing test** (in `herrscher-contracts`)

```go
package contracts

import "testing"

type skillNativeStub struct{}

func (skillNativeStub) NativeSkills() bool { return true }

func TestSkillNativeSatisfied(t *testing.T) {
	var _ SkillNative = skillNativeStub{}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd herrscher-contracts && GOWORK=off go test ./... -run SkillNative 2>&1 | tail`
Expected: build failure — `undefined: SkillNative`.

- [ ] **Step 3: Write minimal implementation**

`herrscher-contracts/skill.go`:

```go
package contracts

// SkillNative is implemented by a backend that discovers and loads skills itself
// (e.g. the claude CLI, which reads .claude/skills and ~/.claude/skills). The
// host skips its own skill menu injection and marker detection for such a
// backend so skills are not loaded twice.
type SkillNative interface {
	NativeSkills() bool
}
```

- [ ] **Step 4: Run contracts test to verify it passes**

Run: `cd herrscher-contracts && GOWORK=off go test ./... -run SkillNative 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5: Implement on the claude backend**

Append to `herrscher-claude-backend/stream.go`:

```go
// NativeSkills reports that the claude CLI loads skills itself, so the host must
// not inject its own skill menu for this backend. Implements contracts.SkillNative.
func (r *streamResponder) NativeSkills() bool { return true }

// NativeSkills reports that the claude CLI loads skills itself. Implements
// contracts.SkillNative.
func (o *oneShotResponder) NativeSkills() bool { return true }
```

- [ ] **Step 6: Write the backend assertion test**

Append to `herrscher-claude-backend/backend_test.go`:

```go
func TestClaudeBackendsAreSkillNative(t *testing.T) {
	var _ contracts.SkillNative = (*streamResponder)(nil)
	var _ contracts.SkillNative = (*oneShotResponder)(nil)
}
```

(If `contracts` is not already imported in that test file, add
`contracts "github.com/Herrscherd/herrscher-contracts"` to its imports.)

- [ ] **Step 7: Run backend tests to verify they pass**

Run: `cd herrscher-claude-backend && GOWORK=off go test ./... 2>&1 | tail`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
# in herrscher-contracts
git add skill.go skill_test.go && git commit -m "feat(contracts): add SkillNative backend capability"
# in herrscher-claude-backend
git add stream.go backend_test.go && git commit -m "feat(claude): declare NativeSkills (CLI loads skills itself)"
```

---

### Task 4: config `Skills` knob

**Files:**
- Modify: `core/config/config.go` (add field to `Config`)
- Test: `core/config/config_test.go` (append a case)

**Interfaces:**
- Produces: `Config.Skills` of type:
  ```go
  type SkillsConfig struct {
      Enabled *bool    `json:"enabled,omitempty"`
      Roots   []string `json:"roots,omitempty"`
  }
  ```
  A nil `Skills` or nil `Enabled` means enabled (default on). `Roots` are extra roots appended after the built-in workspace + global defaults.

- [ ] **Step 1: Write the failing test**

Append to `core/config/config_test.go`:

```go
func TestLoadSkillsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"skills":{"enabled":false,"roots":["/opt/skills"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Skills == nil || c.Skills.Enabled == nil || *c.Skills.Enabled {
		t.Fatalf("want skills.enabled=false, got %+v", c.Skills)
	}
	if len(c.Skills.Roots) != 1 || c.Skills.Roots[0] != "/opt/skills" {
		t.Fatalf("want roots=[/opt/skills], got %+v", c.Skills)
	}
}
```

(Ensure `filepath` and `os` are imported in the test file; add them if missing.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd core/config && GOWORK=off go test ./... -run SkillsConfig 2>&1 | tail`
Expected: FAIL — `c.Skills undefined`.

- [ ] **Step 3: Write minimal implementation**

In `core/config/config.go`, add the type and a field on `Config` (after the `Source` field):

```go
// SkillsConfig configures cross-backend skill injection. A nil pointer, or a nil
// Enabled, means the feature is on. Roots are extra skill roots appended after
// the built-in workspace and global (~/.claude/skills) defaults.
type SkillsConfig struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Roots   []string `json:"roots,omitempty"`
}
```

and inside `type Config struct { … }`:

```go
	Skills *SkillsConfig `json:"skills,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd core/config && GOWORK=off go test ./... -run SkillsConfig 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/config/config.go core/config/config_test.go
git commit -m "feat(config): add skills.enabled and skills.roots knobs"
```

---

### Task 5: Wire the engine into the bridge hub

**Files:**
- Create: `core/bridge/skills.go` (root resolution + engine construction, isolated for testability)
- Modify: `core/bridge/hub.go` (thread an `*skills.Engine` through `runHubTurnsCtl` and `runOneTurn`)
- Modify: `core/bridge/bridge.go` (`runHub` and `RunOneShot` build the engine)
- Test: `core/bridge/skills_test.go`
- Test: `core/bridge/hub_test.go` (append integration cases; reuse existing fake-backend helpers there)

**Interfaces:**
- Consumes: `skills.NewEngine` (Task 2), `contracts.SkillNative` (Task 3), `config.Load`/`config.DefaultPath`/`SkillsConfig` (Task 4).
- Produces:
  - `func skillRoots(cwd string, extra []string) []string` — `[cwd/.claude/skills, <home>/.claude/skills, extra...]`.
  - `func newSkillEngine(resp contracts.Backend) *skills.Engine` — returns `nil` when the backend is `SkillNative` and reports true, or when config disables skills; else an engine over `skillRoots`.
  - `runOneTurn` and `runHubTurnsCtl` gain a trailing `eng *skills.Engine` parameter (nil = disabled, current behavior).

- [ ] **Step 1: Write the failing test** (root resolution + gating)

`core/bridge/skills_test.go`:

```go
package bridge

import (
	"context"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type nativeBackend struct{}

func (nativeBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "", nil
}
func (nativeBackend) Close() error         { return nil }
func (nativeBackend) NativeSkills() bool    { return true }

type plainBackend struct{}

func (plainBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "", nil
}
func (plainBackend) Close() error { return nil }

func TestSkillRootsOrder(t *testing.T) {
	roots := skillRoots("/work/repo", []string{"/opt/x"})
	if roots[0] != filepath.Join("/work/repo", ".claude", "skills") {
		t.Fatalf("workspace root must come first, got %v", roots)
	}
	if roots[len(roots)-1] != "/opt/x" {
		t.Fatalf("extra roots must come last, got %v", roots)
	}
}

func TestNewSkillEngineNilForNativeBackend(t *testing.T) {
	if eng := newSkillEngine(nativeBackend{}); eng != nil {
		t.Fatalf("native backend must get no engine")
	}
	if eng := newSkillEngine(plainBackend{}); eng == nil {
		t.Fatalf("non-native backend must get an engine")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd core/bridge && GOWORK=off go test ./... -run 'SkillRoots|SkillEngine' 2>&1 | tail`
Expected: build failure — `undefined: skillRoots` / `newSkillEngine`.

- [ ] **Step 3: Write minimal implementation**

`core/bridge/skills.go`:

```go
package bridge

import (
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/skills"
)

// skillRoots is the ordered skill search path: the session workspace (the bridge
// runs with cwd = workspace), then the user-global skills, then any extra roots
// from config. Earlier roots win de-dup, so a repo skill overrides a global one.
func skillRoots(cwd string, extra []string) []string {
	roots := []string{filepath.Join(cwd, ".claude", "skills")}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".claude", "skills"))
	}
	return append(roots, extra...)
}

// newSkillEngine builds the per-session skill engine, or returns nil when skills
// are disabled: the backend loads skills natively (contracts.SkillNative), or
// config turns the feature off. A nil engine means the hub injects nothing.
func newSkillEngine(resp contracts.Backend) *skills.Engine {
	if n, ok := resp.(contracts.SkillNative); ok && n.NativeSkills() {
		return nil
	}
	cfg, _ := config.Load(config.DefaultPath())
	var extra []string
	if cfg.Skills != nil {
		if cfg.Skills.Enabled != nil && !*cfg.Skills.Enabled {
			return nil
		}
		extra = cfg.Skills.Roots
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return skills.NewEngine(skillRoots(cwd, extra))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd core/bridge && GOWORK=off go test ./... -run 'SkillRoots|SkillEngine' 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5: Thread the engine through the turn driver**

In `core/bridge/hub.go`, change the signatures and the `Context` assembly. Replace the `runOneTurn` signature and its `memCtx`/`Detect` handling:

```go
func runOneTurn(ctx context.Context, sink contracts.EventSink, resp contracts.Backend, orch contracts.Orchestrator, ev contracts.Event, ctrl *turnController, eng *skills.Engine) {
	turnCtx, endTurn := ctrl.begin(ctx)
	defer endTurn()
	var memCtx string
	if orch != nil {
		memCtx = orch.Context(turnCtx)
	}
	prompt := contracts.Prompt{Content: ev.Text, Context: withSkills(memCtx, eng), Author: ev.Who, Attachments: ev.Attachments}
	var cost float64
	var outTok int
	onEvent := func(be contracts.BackendEvent) {
		switch be.Kind {
		case "usage":
			outTok = be.OutTokens
		case "result":
			cost = be.Cost
			outTok = be.OutTokens
		}
		emitBackendEvent(sink, be, outTok)
	}
	out, err := resp.Respond(turnCtx, prompt, onEvent)
	if err != nil && out == "" {
		out = "⚠️ " + err.Error()
	}
	out = strings.TrimSpace(out)
	if eng != nil {
		eng.Detect(out)
	}
	sink.Emit(contracts.Event{T: "reply", Text: out, Done: true, Cost: cost, Tokens: outTok, Resume: resumeToken(resp)})
	if orch != nil {
		_ = orch.Observe(ctx, prompt, out)
	}
}

// withSkills appends the skill menu and any active-skill expansions to the
// memory context. A nil engine (skills disabled / native backend) returns memCtx
// unchanged.
func withSkills(memCtx string, eng *skills.Engine) string {
	if eng == nil {
		return memCtx
	}
	parts := make([]string, 0, 3)
	if memCtx != "" {
		parts = append(parts, memCtx)
	}
	if menu := eng.Menu(); menu != "" {
		parts = append(parts, menu)
	}
	if exp := eng.Expansions(); exp != "" {
		parts = append(parts, exp)
	}
	return strings.Join(parts, "\n\n")
}
```

Update `runHubTurnsCtl` to take and forward the engine:

```go
func runHubTurnsCtl(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, resp contracts.Backend, orch contracts.Orchestrator, ctrl *turnController, eng *skills.Engine) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-in:
			if !ok {
				return
			}
			switch ev.T {
			case "pick":
				runPick(ctx, sink, resp, ev.Value)
			default:
				runOneTurn(ctx, sink, resp, orch, ev, ctrl, eng)
			}
		}
	}
}
```

Update the `runHubTurns` test helper to pass `nil`:

```go
func runHubTurns(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, resp contracts.Backend, orch contracts.Orchestrator) {
	runHubTurnsCtl(ctx, in, sink, resp, orch, nil, nil)
}
```

Add the import `"github.com/Herrscherd/herrscher/core/skills"` to `hub.go`.

- [ ] **Step 6: Build the engine at the call sites**

In `core/bridge/hub.go` `runHub`, after `resp` is created and before `runHubTurnsCtl`:

```go
	eng := newSkillEngine(resp)
	runHubTurnsCtl(ctx, in, conn, resp, orch, ctrl, eng)
	return ctx.Err()
```

In `core/bridge/bridge.go` `RunOneShot`, pass an engine to `runOneTurn`:

```go
	select {
	case ev := <-in:
		runOneTurn(ctx, channelSink{ctx: ctx, out: out}, resp, orch, ev, nil, newSkillEngine(resp))
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
```

- [ ] **Step 7: Write the hub integration test**

Append to `core/bridge/hub_test.go` (a `captureBackend` records the last prompt Context and returns a scripted reply; adapt to the existing fake-backend/sink helpers already in that file rather than duplicating them if present):

```go
func TestHubInjectsSkillMenuAndExpandsOnMarker(t *testing.T) {
	root := t.TempDir()
	writeBridgeSkill(t, root, "demo", "name: demo\ndescription: a demo\n", "DEMO BODY\n")
	eng := skills.NewEngine([]string{filepath.Join(root, ".claude", "skills")})

	var seen []string
	resp := &captureBackend{reply: "ok <use-skill>demo</use-skill>", onPrompt: func(p contracts.Prompt) {
		seen = append(seen, p.Context)
	}}
	sink := &sliceSink{}

	// Turn 1: menu present, body absent (nothing active yet).
	runOneTurn(context.Background(), sink, resp, nil, contracts.Event{T: "input", Text: "hi"}, nil, eng)
	if !strings.Contains(seen[0], "demo: a demo") {
		t.Fatalf("turn 1 context missing menu:\n%s", seen[0])
	}
	if strings.Contains(seen[0], "DEMO BODY") {
		t.Fatalf("turn 1 must not carry body yet:\n%s", seen[0])
	}
	// Turn 2: marker from turn 1 activated demo → body now injected.
	runOneTurn(context.Background(), sink, resp, nil, contracts.Event{T: "input", Text: "again"}, nil, eng)
	if !strings.Contains(seen[1], "DEMO BODY") {
		t.Fatalf("turn 2 context missing expanded body:\n%s", seen[1])
	}
}

func TestHubSkipsInjectionWhenEngineNil(t *testing.T) {
	var seen string
	resp := &captureBackend{reply: "ok", onPrompt: func(p contracts.Prompt) { seen = p.Context }}
	runOneTurn(context.Background(), &sliceSink{}, resp, nil, contracts.Event{T: "input", Text: "hi"}, nil, nil)
	if seen != "" {
		t.Fatalf("nil engine must leave context empty, got %q", seen)
	}
}
```

Add helpers to `hub_test.go` if the file does not already define equivalents:

```go
func writeBridgeSkill(t *testing.T, workspace, name, front, body string) {
	t.Helper()
	dir := filepath.Join(workspace, ".claude", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\n"+front+"---\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type captureBackend struct {
	reply    string
	onPrompt func(contracts.Prompt)
}

func (b *captureBackend) Respond(_ context.Context, p contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	if b.onPrompt != nil {
		b.onPrompt(p)
	}
	return b.reply, nil
}
func (b *captureBackend) Close() error { return nil }

type sliceSink struct{ events []contracts.Event }

func (s *sliceSink) Emit(e contracts.Event) { s.events = append(s.events, e) }
```

Add imports to `hub_test.go` as needed: `"os"`, `"path/filepath"`, `"strings"`, `"github.com/Herrscherd/herrscher/core/skills"`.

- [ ] **Step 8: Run the bridge tests to verify they pass**

Run: `cd core/bridge && GOWORK=off go test ./... 2>&1 | tail`
Expected: PASS (new and existing).

- [ ] **Step 9: Commit**

```bash
git add core/bridge/skills.go core/bridge/skills_test.go core/bridge/hub.go core/bridge/bridge.go core/bridge/hub_test.go
git commit -m "feat(skills): inject skill menu + on-demand expansion in the bridge hub"
```

---

### Task 6: Full-build verification and workspace sanity

**Files:** none (verification only).

- [ ] **Step 1: Module-isolated builds and tests**

Run:
```bash
cd herrscher-contracts && GOWORK=off go build ./... && GOWORK=off go test ./... 2>&1 | tail
cd ../herrscher-claude-backend && GOWORK=off go build ./... && GOWORK=off go test ./... 2>&1 | tail
cd ../herrscher && GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./... 2>&1 | tail
```
Expected: all build, vet clean, tests PASS.

- [ ] **Step 2: Workspace build (go.work)**

Run: `cd herrscher && go build ./... && echo WS_OK`
Expected: `WS_OK`.

- [ ] **Step 3: Manual smoke of discovery (optional but recommended)**

Create a throwaway skill and confirm discovery via a tiny program or an existing session; or rely on `core/skills` tests as the behavioral proof. No commit.

- [ ] **Step 4: Nothing to commit** (verification task). If any fix was required, commit it with a `fix(skills): …` message.

---

## Release (after all tasks pass)

Consuming order matters (contracts → claude-backend → herrscher):

```bash
# 1. contracts
cd herrscher-contracts && git push origin HEAD && git tag -a vX.Y.Z -m "…" && git push origin vX.Y.Z
# 2. claude-backend: bump contracts, then tag
cd ../herrscher-claude-backend && GOFLAGS=-mod=mod GOWORK=off go get github.com/Herrscherd/herrscher-contracts@vX.Y.Z && GOFLAGS=-mod=mod GOWORK=off go mod tidy && git commit -am "chore(deps): contracts vX.Y.Z (SkillNative)" && git push origin HEAD && git tag -a vA.B.C -m "…" && git push origin vA.B.C
# 3. herrscher: bump both deps, then tag
cd ../herrscher && GOFLAGS=-mod=mod GOWORK=off go get github.com/Herrscherd/herrscher-contracts@vX.Y.Z github.com/Herrscherd/herrscher-claude-backend@vA.B.C && GOFLAGS=-mod=mod GOWORK=off go mod tidy && git commit -am "feat(skills): cross-backend skill injection" && git push origin HEAD && git tag -a v0.1.29 -m "v0.1.29: cross-backend skills" && git push origin v0.1.29
```

Exact versions decided at release time from each repo's latest tag.
