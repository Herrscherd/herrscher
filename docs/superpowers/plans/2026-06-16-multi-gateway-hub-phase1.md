# Multi-gateway hub — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lay the multi-gateway foundation — sessions record which gateways they bind to (`Gateways []string`, with a Discord back-compat default), and the core can instantiate *all* registered gateway plugins into one hub — with zero behavior change (still Discord-only, no UI).

**Architecture:** Add a `Gateways` field + `BoundGateways()` helper to `state.Session`. Add a `GatewayHub` in `core/host` that builds every registered gateway plugin into a `kind → GatewaySet` map, tolerating gateways whose config can't resolve (so a future terminal-only stack still runs) but aggregating failures so a single-gateway stack still fails fast with a clear message. Reimplement the existing `buildGateway` on top of the hub, preserving its current "first gateway" behavior exactly. This is the spec's Phase 1; Phases 2–4 (structured bus + bridge runner, terminal gateway + TUI, attach) get their own plans.

**Tech Stack:** Go (stdlib only — `testing`, `encoding/json`), the existing `github.com/Herrscherd/herrscher-contracts` registry/`GatewaySet` API.

Spec: `docs/superpowers/specs/2026-06-16-serve-tui-multi-gateway-hub-design.md`.

---

## File Structure

- `core/internal/state/state.go` — add `Gateways []string` to `Session` + `BoundGateways()` method.
- `core/internal/state/state_test.go` — tests for `BoundGateways()` back-compat.
- `core/host/gatewayhub.go` (new) — `GatewayHub`, `BuildHub(ctx, plugins, getenv)`, `Kinds()`, `Get(kind)`, `First()`.
- `core/host/gatewayhub_test.go` (new) — tests with fake plugins (build-all, tolerate-failure, aggregate-error, dedup).
- `serve.go` (host package main) — reimplement `buildGateway` via `host.BuildHub`.
- `core/internal/manager/gateways.go` (new) — `ParseGateways(list string, terminalOnly bool) []string` helper.
- `core/internal/manager/gateways_test.go` (new) — tests for `ParseGateways`.
- `core/internal/manager/session.go` — set `sess.Gateways` from input in `sessionCreateRun` (one wiring line; no channel-creation change).

---

## Task 1: `state.Session.Gateways` + `BoundGateways()`

**Files:**
- Modify: `core/internal/state/state.go:18-29` (struct) and add a method after it
- Test: `core/internal/state/state_test.go`

- [ ] **Step 1: Write the failing test**

Add to `core/internal/state/state_test.go`:

```go
func TestBoundGateways(t *testing.T) {
	cases := []struct {
		name string
		sess Session
		want []string
	}{
		{"explicit", Session{Gateways: []string{"terminal"}}, []string{"terminal"}},
		{"both", Session{Gateways: []string{"discord", "terminal"}}, []string{"discord", "terminal"}},
		{"legacy with channel", Session{ChannelID: "c1"}, []string{"discord"}},
		{"empty no channel", Session{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.sess.BoundGateways()
			if !slices.Equal(got, tc.want) {
				t.Errorf("BoundGateways() = %v, want %v", got, tc.want)
			}
		})
	}
}
```

If `slices` is not already imported in the test file, add `"slices"` to its import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/state/ -run TestBoundGateways`
Expected: FAIL — `sess.BoundGateways undefined` (compile error).

- [ ] **Step 3: Add the field and method**

In `core/internal/state/state.go`, add the field to `Session` (after `Project`, before `Participants`):

```go
	Project   string `json:"project,omitempty"`  // workspace sub-dir the session started from

	// Gateways binds the session to a set of gateway kinds (e.g. "discord",
	// "terminal"). Empty means "legacy": a session with a ChannelID is Discord.
	Gateways []string `json:"gateways,omitempty"`

	Participants []string `json:"participants,omitempty"` // observed authors (cache; journal is source of truth)
```

Add the method directly after the `Session` struct definition:

```go
// BoundGateways returns the gateway kinds this session is bound to. When the
// stored set is empty it falls back to the legacy rule: a session with a
// ChannelID is a Discord session; one without is bound to nothing.
func (s Session) BoundGateways() []string {
	if len(s.Gateways) > 0 {
		return s.Gateways
	}
	if s.ChannelID != "" {
		return []string{"discord"}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/state/ -run TestBoundGateways`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/internal/state/state.go core/internal/state/state_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -q -m "feat(state): Session.Gateways + BoundGateways() back-compat"
```

---

## Task 2: `GatewayHub` — build all registered gateways

**Files:**
- Create: `core/host/gatewayhub.go`
- Test: `core/host/gatewayhub_test.go`

- [ ] **Step 1: Write the failing test**

Create `core/host/gatewayhub_test.go`:

```go
package host

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// fakeGateway is a minimal Gateway used only to make a GatewaySet non-zero.
type fakeGateway struct{}

func (fakeGateway) Post(context.Context, contracts.Conversation, string) (contracts.MessageID, error) {
	return "", nil
}
func (fakeGateway) Reply(context.Context, contracts.Conversation, contracts.MessageID, string) (contracts.MessageID, error) {
	return "", nil
}
func (fakeGateway) React(context.Context, contracts.Conversation, contracts.MessageID, string) error {
	return nil
}
func (fakeGateway) Menu(context.Context, contracts.Conversation, contracts.MessageID, string, []contracts.Choice) error {
	return nil
}

func gw(kind string, fail bool) contracts.Plugin {
	return contracts.Plugin{
		Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryGateway},
		Gateway: func(context.Context, contracts.PluginConfig) (contracts.GatewaySet, error) {
			if fail {
				return contracts.GatewaySet{}, errors.New("boom")
			}
			return contracts.GatewaySet{Gateway: fakeGateway{}}, nil
		},
	}
}

func TestBuildHubAll(t *testing.T) {
	hub, err := BuildHub(context.Background(), []contracts.Plugin{gw("discord", false), gw("terminal", false)}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got := hub.Kinds(); len(got) != 2 || got[0] != "discord" || got[1] != "terminal" {
		t.Fatalf("Kinds() = %v, want [discord terminal]", got)
	}
	if _, ok := hub.Get("terminal"); !ok {
		t.Error("terminal gateway should be present")
	}
}

func TestBuildHubToleratesFailure(t *testing.T) {
	hub, err := BuildHub(context.Background(), []contracts.Plugin{gw("discord", true), gw("terminal", false)}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got := hub.Kinds(); len(got) != 1 || got[0] != "terminal" {
		t.Fatalf("Kinds() = %v, want [terminal]", got)
	}
}

func TestBuildHubAllFailedAggregates(t *testing.T) {
	_, err := BuildHub(context.Background(), []contracts.Plugin{gw("discord", true)}, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "discord") {
		t.Fatalf("want aggregated error mentioning discord, got %v", err)
	}
}

func TestFirstReturnsRegistrationOrder(t *testing.T) {
	hub, _ := BuildHub(context.Background(), []contracts.Plugin{gw("discord", false), gw("terminal", false)}, func(string) string { return "" })
	if hub.First().Gateway == nil {
		t.Error("First() should return the first built set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestBuildHub`
Expected: FAIL — `BuildHub undefined` (compile error).

- [ ] **Step 3: Write the hub**

Create `core/host/gatewayhub.go`:

```go
package host

import (
	"context"
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// GatewayHub holds every registered gateway plugin instantiated into a
// GatewaySet, keyed by Manifest.Kind. It is the core's multi-gateway port: the
// daemon resolves a session's bound gateways through it instead of hand-wiring a
// single gateway. Kinds() preserves registration order.
type GatewayHub struct {
	sets  map[string]contracts.GatewaySet
	order []string
}

// BuildHub instantiates each gateway plugin in plugins. A plugin whose config
// can't resolve, or whose factory errors, is skipped (its required vars are
// absent — e.g. no Discord token — which must not stop other gateways from
// running). If NO gateway builds, the aggregated per-gateway reasons are
// returned so a single-gateway stack still fails fast with a clear message.
func BuildHub(ctx context.Context, plugins []contracts.Plugin, getenv func(string) string) (*GatewayHub, error) {
	h := &GatewayHub{sets: map[string]contracts.GatewaySet{}}
	var failures []string
	for _, p := range plugins {
		if p.Gateway == nil {
			continue
		}
		cfg, err := contracts.Resolve(p.Manifest.Config, getenv)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", p.Manifest.Kind, err))
			continue
		}
		set, err := p.Gateway(ctx, cfg)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", p.Manifest.Kind, err))
			continue
		}
		if _, dup := h.sets[p.Manifest.Kind]; !dup {
			h.order = append(h.order, p.Manifest.Kind)
		}
		h.sets[p.Manifest.Kind] = set
	}
	if len(h.sets) == 0 {
		if len(failures) == 0 {
			return nil, fmt.Errorf("no gateway plugin registered")
		}
		return nil, fmt.Errorf("no gateway available: %s", strings.Join(failures, "; "))
	}
	return h, nil
}

// Kinds returns the built gateway kinds in registration order.
func (h *GatewayHub) Kinds() []string { return append([]string(nil), h.order...) }

// Get returns the GatewaySet for a kind and whether it was built.
func (h *GatewayHub) Get(kind string) (contracts.GatewaySet, bool) {
	s, ok := h.sets[kind]
	return s, ok
}

// First returns the first built gateway set (registration order). It preserves
// the pre-hub "first registered gateway" behavior for callers not yet
// gateway-aware.
func (h *GatewayHub) First() contracts.GatewaySet { return h.sets[h.order[0]] }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'TestBuildHub|TestFirst'`
Expected: PASS (4 subtests/tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/host/gatewayhub.go core/host/gatewayhub_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -q -m "feat(host): GatewayHub builds all registered gateways, tolerant + aggregating"
```

---

## Task 3: Reimplement `buildGateway` on the hub (non-regression)

**Files:**
- Modify: `serve.go` (host package main) — the `buildGateway` function (currently iterates `contracts.Default.Gateways()` and returns the first)

- [ ] **Step 1: Replace the body**

In `serve.go`, replace the entire `buildGateway` function with:

```go
// buildGateway returns the first registered gateway's GatewaySet, built through
// the multi-gateway hub. Behavior is unchanged from the pre-hub version (first
// gateway wins); the hub additionally tolerates other gateways whose config is
// absent. A new gateway is still just a blank import + rebuild.
func buildGateway(ctx context.Context) (host.Deps, error) {
	hub, err := host.BuildHub(ctx, contracts.Default.Gateways(), os.Getenv)
	if err != nil {
		return host.Deps{}, err
	}
	return hub.First(), nil
}
```

Leave the `import` block as-is — `contracts`, `os`, and `host` are already imported in `serve.go`.

- [ ] **Step 2: Build the whole module**

Run: `cd /home/shan/dev/herrscher && go build ./...`
Expected: Success (no errors).

- [ ] **Step 3: Run the full test suite (non-regression)**

Run: `cd /home/shan/dev/herrscher && go test ./...`
Expected: PASS across all packages (including `TestCorePurity`, `TestHostPurity`).

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher
git add serve.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -q -m "refactor(host): buildGateway via GatewayHub.First (no behavior change)"
```

---

## Task 4: `ParseGateways` helper + persist on session create

**Files:**
- Create: `core/internal/manager/gateways.go`
- Test: `core/internal/manager/gateways_test.go`
- Modify: `core/internal/manager/session.go` (in `sessionCreateRun`, set `sess.Gateways`)

- [ ] **Step 1: Write the failing test**

Create `core/internal/manager/gateways_test.go`:

```go
package manager

import (
	"slices"
	"testing"
)

func TestParseGateways(t *testing.T) {
	cases := []struct {
		name         string
		list         string
		terminalOnly bool
		want         []string
	}{
		{"default", "", false, []string{"discord"}},
		{"terminal only flag", "", true, []string{"terminal"}},
		{"explicit list", "discord,terminal", false, []string{"discord", "terminal"}},
		{"trims and drops empties", " discord , , terminal ", false, []string{"discord", "terminal"}},
		{"dedups", "discord,discord", false, []string{"discord"}},
		{"flag wins over empty list only", "discord,terminal", true, []string{"discord", "terminal"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseGateways(tc.list, tc.terminalOnly)
			if !slices.Equal(got, tc.want) {
				t.Errorf("ParseGateways(%q,%v) = %v, want %v", tc.list, tc.terminalOnly, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/manager/ -run TestParseGateways`
Expected: FAIL — `ParseGateways undefined`.

- [ ] **Step 3: Write the helper**

Create `core/internal/manager/gateways.go`:

```go
package manager

import "strings"

// ParseGateways turns the `--gateways a,b` flag (and the `--terminal-only`
// shorthand) into the ordered, de-duplicated gateway set stored on a session.
// An explicit list always wins; an empty list with terminalOnly yields
// ["terminal"]; an empty list otherwise defaults to ["discord"].
func ParseGateways(list string, terminalOnly bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) > 0 {
		return out
	}
	if terminalOnly {
		return []string{"terminal"}
	}
	return []string{"discord"}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/manager/ -run TestParseGateways`
Expected: PASS (6 subtests).

- [ ] **Step 5: Persist the parsed set on create**

In `core/internal/manager/session.go`, inside `sessionCreateRun`, read the flags near the other `in.Lookup` calls (after the `backend` lookup around line 65) and set the field on the `Session` literal that is later passed to `AddSession`.

Add the parse near the top of the function body (after `name` is resolved):

```go
	gwList, _ := in.Lookup("gateways")
	gateways := ParseGateways(gwList, in.Bool("terminal-only"))
```

Then add `Gateways: gateways,` to the `state.Session{...}` composite literal(s) built in this function (there are two construction sites — the text-home branch ~line 114 and the forum branch ~line 123; add the field to whichever literal(s) set `Name`/`ChannelID`).

- [ ] **Step 6: Build and run the manager + full suite**

Run: `cd /home/shan/dev/herrscher && go build ./... && go test ./...`
Expected: PASS. (Behavior is unchanged for existing callers: with no `--gateways`/`--terminal-only`, `Gateways` becomes `["discord"]`, and `BoundGateways()` already returned that for a channel-backed session — so no regression.)

- [ ] **Step 7: Commit**

```bash
cd /home/shan/dev/herrscher
git add core/internal/manager/gateways.go core/internal/manager/gateways_test.go core/internal/manager/session.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -q -m "feat(manager): ParseGateways + persist Session.Gateways on create"
```

---

## Done criteria (Phase 1)

- `go test ./...` green, including `TestCorePurity` / `TestHostPurity`.
- A session persists its bound gateway set; legacy sessions still resolve to `["discord"]`.
- The core can build every registered gateway into one hub, tolerating absent config but failing fast when nothing builds.
- No user-visible behavior change yet — Discord-only, no UI. (Fan-out + bridge runner = Phase 2; terminal gateway + TUI = Phase 3; attach = Phase 4.)
