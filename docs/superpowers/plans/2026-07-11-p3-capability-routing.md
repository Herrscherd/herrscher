# Routage déterministe par capacités — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ajouter `⟢ route: <task>` : le lead décrit une tâche sans nommer d'agent, le host choisit déterministiquement l'agent le mieux doté par match de capacités, et délègue.

**Architecture:** Les agents déclarent des tags dans `<home>/TAGS` (lu dans `agent.Agent.Tags`). Un nouveau trailer `⟢ route:` mappe une méthode de port `Route`. Le host coordinator énumère le roster, score chaque agent (fonction pure `pickAgent`) et réutilise `spawn` en sémantique Delegate. Aucun LLM dans le host — Model O préservé.

**Tech Stack:** Go, module `github.com/Herrscherd/herrscher` (`core/...`) + `github.com/Herrscherd/herrscher-contracts`, résolus via `/home/shan/dev/go.work`.

## Global Constraints

- Branche : **`feat/p3-join-merge`** — ne pas créer d'autre branche, ne pas merger.
- Identité de commit : `git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit`.
- Invariant Model O : **aucun appel LLM dans le host**. Le routage est un barème pur et déterministe.
- Chaque trailer déclenché par un agent mappe une méthode de port ; le host reste déterministe (les gardes tournent avant tout effet de bord).
- Séparateurs littéraux : le trailer `⟢ route:` n'a **pas** de split em-dash (corps entier = tâche), contrairement à delegate/handoff/fanout.
- Ordre de dispatch dans `maybeCoordinate` : `done → delegate → fanout → route → seal → merge → handoff` (un seul trailer par tour, premier match gagne).
- Aucun fallback vers un agent par défaut : score max == 0 → refus explicite.
- `go test ./...` doit passer dans les DEUX modules (contracts et herrscher).

---

### Task 1: Contrat — `RouteRequest` + méthode de port `Route`

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/coordinator.go`
- Test: `/home/shan/dev/herrscher-contracts/coordinator_test.go`

**Interfaces:**
- Produces: `contracts.RouteRequest{FromSession, Task string}` ; `Coordinator.Route(ctx, RouteRequest) (agent string, session string, err error)` (méthode à **trois** valeurs de retour, contrairement aux autres).

- [ ] **Step 1: Écrire le test de forme de port (échoue à la compilation)**

Dans `coordinator_test.go`, ajouter après le bloc `fanoutStub` / `TestCoordinatorPortIncludesFanOut` :

```go
// routeStub carries the full Coordinator surface (incl. Route) to assert the
// port shape at compile time.
type routeStub struct{}

func (routeStub) Handoff(context.Context, HandoffRequest) (string, error)   { return "", nil }
func (routeStub) Delegate(context.Context, DelegateRequest) (string, error) { return "", nil }
func (routeStub) Report(context.Context, ReportRequest) (string, error)     { return "", nil }
func (routeStub) Merge(context.Context, MergeRequest) (string, error)       { return "", nil }
func (routeStub) Seal(context.Context, SealRequest) (string, error)         { return "", nil }
func (routeStub) FanOut(context.Context, FanOutRequest) ([]string, error)   { return nil, nil }
func (routeStub) Route(context.Context, RouteRequest) (string, string, error) {
	return "", "", nil
}

func TestCoordinatorPortIncludesRoute(t *testing.T) {
	var _ Coordinator = routeStub{}
	req := RouteRequest{FromSession: "lead", Task: "écris le module réseau"}
	if req.FromSession != "lead" || req.Task == "" {
		t.Fatalf("RouteRequest fields not wired: %+v", req)
	}
}
```

Ajouter aussi la méthode `Route` aux stubs existants `mergeStub`, `sealStub`, `fanoutStub` (une ligne chacun, sinon ils ne satisfont plus `Coordinator`) :

```go
func (mergeStub) Route(context.Context, RouteRequest) (string, string, error)  { return "", "", nil }
func (sealStub) Route(context.Context, RouteRequest) (string, string, error)   { return "", "", nil }
func (fanoutStub) Route(context.Context, RouteRequest) (string, string, error) { return "", "", nil }
```

Et à `fakeCoordinator` : ajouter le champ `gotRoute RouteRequest` dans la struct, puis la méthode :

```go
func (f *fakeCoordinator) Route(_ context.Context, req RouteRequest) (string, string, error) {
	f.gotRoute = req
	return "picked", req.FromSession + "-routed", nil
}
```

- [ ] **Step 2: Lancer le test — échoue (Route absente de l'interface)**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: FAIL compilation — `routeStub does not implement Coordinator (missing method Route)` / `RouteRequest` indéfini.

- [ ] **Step 3: Ajouter `RouteRequest` + `Route` à l'interface**

Dans `coordinator.go`, ajouter la struct après `FanOutRequest` :

```go
// RouteRequest is a lead handing off a task WITHOUT naming the agent: the host
// picks the best-matching agent deterministically (capability match) and
// delegates. No LLM judgment enters the host — the match is a pure score of the
// agents' declared tags against the task text.
type RouteRequest struct {
	FromSession string // the lead routing (the chosen worker's base and parent)
	Task        string // the task to route; also the text scored against agent tags
}
```

Et dans l'interface `Coordinator`, après `FanOut(...)` :

```go
	// Route picks the best-matching agent for Task by a deterministic capability
	// score (the agents' declared tags against the task text — no LLM), then
	// delegates: the chosen worker is a child of FromSession off its committed tip,
	// FromSession stays alive. Returns the chosen agent and the worker's session.
	// It errors on an empty task, a missing lead, a spawn failure, or when no agent
	// matches (the host refuses rather than falling back to a default).
	Route(ctx context.Context, req RouteRequest) (agent string, session string, err error)
```

- [ ] **Step 4: Lancer le test — passe**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-contracts && git add coordinator.go coordinator_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(contracts): RouteRequest + Route port method (p3 tranche 4)"
```

---

### Task 2: Agent — fichier `TAGS` + `Agent.Tags` peuplé par Get/List

**Files:**
- Modify: `/home/shan/dev/herrscher/core/internal/agent/agent.go`
- Modify: `/home/shan/dev/herrscher/core/internal/agent/store.go`
- Test: `/home/shan/dev/herrscher/core/internal/agent/store_test.go`

**Interfaces:**
- Consumes: rien de nouveau.
- Produces: champ `agent.Agent.Tags []string` ; peuplé par `Store.Get` et `Store.List` en lisant `<home>/TAGS` (tokens séparés par espaces/virgules/retours-ligne, lowercasés, dé-dupliqués ; absent → `nil`).

- [ ] **Step 1: Écrire les tests (échouent)**

Dans `store_test.go`, ajouter :

```go
func TestGetReadsTags(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	if _, err := s.Create(CreateSpec{Name: "netter", Soul: "x"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "netter", "TAGS"), []byte("network, Sockets\nhttp"), 0o644); err != nil {
		t.Fatalf("write TAGS: %v", err)
	}
	a, ok := s.Get("netter")
	if !ok {
		t.Fatal("Get netter = !ok")
	}
	got := strings.Join(a.Tags, ",")
	if got != "network,sockets,http" {
		t.Fatalf("Tags = %q, want network,sockets,http", got)
	}
}

func TestGetNoTagsFileYieldsNil(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	if _, err := s.Create(CreateSpec{Name: "bare", Soul: "x"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	a, ok := s.Get("bare")
	if !ok || a.Tags != nil {
		t.Fatalf("bare Tags = %v, want nil", a.Tags)
	}
}

func TestListReadsTags(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	for _, n := range []string{"a", "b"} {
		if _, err := s.Create(CreateSpec{Name: n, Soul: "x"}); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a", "TAGS"), []byte("lua  roblox"), 0o644); err != nil {
		t.Fatalf("write TAGS: %v", err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Name != "a" {
		t.Fatalf("list = %+v", list)
	}
	if strings.Join(list[0].Tags, ",") != "lua,roblox" {
		t.Fatalf("a.Tags = %v", list[0].Tags)
	}
	if list[1].Tags != nil {
		t.Fatalf("b.Tags = %v, want nil", list[1].Tags)
	}
}
```

Vérifier que `store_test.go` importe `os`, `path/filepath`, `strings` (les ajouter à l'`import` si absents).

- [ ] **Step 2: Lancer — échoue (Tags absent)**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/agent/`
Expected: FAIL compilation — `a.Tags` indéfini.

- [ ] **Step 3: Ajouter le champ `Tags` + la constante `tagsFile`**

Dans `agent.go`, ajouter à la struct `Agent` :

```go
type Agent struct {
	Name string
	Home string // absolute path to the agent's home directory
	Tags []string // capability tokens from <home>/TAGS (nil when absent), for host routing
}
```

Et dans le bloc `const` des noms de fichiers (à côté de `soulFile`) :

```go
	tagsFile     = "TAGS"
```

- [ ] **Step 4: Lire `TAGS` dans Get et List**

Dans `store.go`, ajouter le helper (près de `validateName`) :

```go
// readTags reads <home>/TAGS into a lowercased, de-duplicated token slice — the
// agent's capability declaration for host-side routing. Tokens are separated by
// whitespace or commas. A missing or unreadable file yields nil: an agent without
// tags is valid (it is simply never auto-selected by routing).
func readTags(home string) []string {
	buf, err := os.ReadFile(filepath.Join(home, tagsFile))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, tok := range strings.Fields(strings.ReplaceAll(string(buf), ",", " ")) {
		t := strings.ToLower(tok)
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
```

Dans `Get`, remplacer la ligne de retour finale :

```go
	return Agent{Name: name, Home: home, Tags: readTags(home)}, true
```

Dans `List`, remplacer l'`append` :

```go
		home := filepath.Join(s.root, e.Name())
		out = append(out, Agent{Name: e.Name(), Home: home, Tags: readTags(home)})
```

- [ ] **Step 5: Lancer — passe**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/agent/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/internal/agent/agent.go core/internal/agent/store.go core/internal/agent/store_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(agent): TAGS file → Agent.Tags for host routing (p3 tranche 4)"
```

---

### Task 3: Host pur — `pickAgent` (barème) + `parseRoute` (trailer)

**Files:**
- Create: `/home/shan/dev/herrscher/core/host/routing.go`
- Modify: `/home/shan/dev/herrscher/core/host/handoff.go`
- Test: `/home/shan/dev/herrscher/core/host/routing_test.go`
- Test: `/home/shan/dev/herrscher/core/host/handoff_test.go`

**Interfaces:**
- Consumes: `agent.Agent.Tags` (Task 2).
- Produces: `pickAgent(roster []agent.Agent, task string) (name string, ok bool)` (pur, déterministe) ; `parseRoute(reply string) (task string, ok bool)` ; const `routeMarker = "⟢ route:"`.

- [ ] **Step 1: Écrire les tests de `pickAgent` (échouent)**

Créer `routing_test.go` :

```go
package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestPickAgentScoresTags(t *testing.T) {
	roster := []agent.Agent{
		{Name: "netter", Tags: []string{"network", "sockets"}},
		{Name: "scripter", Tags: []string{"lua", "roblox"}},
	}
	got, ok := pickAgent(roster, "Implémente le module network avec des sockets")
	if !ok || got != "netter" {
		t.Fatalf("pickAgent = %q,%v want netter,true", got, ok)
	}
}

func TestPickAgentNoMatchRefuses(t *testing.T) {
	roster := []agent.Agent{{Name: "scripter", Tags: []string{"lua"}}}
	if got, ok := pickAgent(roster, "écris de la doc en markdown"); ok {
		t.Fatalf("pickAgent = %q,true want _,false", got)
	}
}

func TestPickAgentTieBreaksByName(t *testing.T) {
	// Both score 1 on "lua"; roster is sorted by name, so the smallest wins.
	roster := []agent.Agent{
		{Name: "alpha", Tags: []string{"lua"}},
		{Name: "beta", Tags: []string{"lua"}},
	}
	if got, _ := pickAgent(roster, "un module lua"); got != "alpha" {
		t.Fatalf("pickAgent tie = %q want alpha", got)
	}
}

func TestPickAgentHighestScoreWins(t *testing.T) {
	roster := []agent.Agent{
		{Name: "alpha", Tags: []string{"lua"}},
		{Name: "beta", Tags: []string{"lua", "roblox"}},
	}
	if got, _ := pickAgent(roster, "module lua pour roblox"); got != "beta" {
		t.Fatalf("pickAgent = %q want beta", got)
	}
}

func TestPickAgentCaseAndPunctuationInsensitive(t *testing.T) {
	roster := []agent.Agent{{Name: "netter", Tags: []string{"http"}}}
	if got, ok := pickAgent(roster, "gère le HTTP, stp."); !ok || got != "netter" {
		t.Fatalf("pickAgent = %q,%v want netter,true", got, ok)
	}
}

func TestPickAgentEmptyRosterRefuses(t *testing.T) {
	if _, ok := pickAgent(nil, "n'importe quoi"); ok {
		t.Fatal("pickAgent(nil) = ok, want !ok")
	}
}

func TestPickAgentIgnoresUntaggedAgents(t *testing.T) {
	roster := []agent.Agent{
		{Name: "bare"},
		{Name: "netter", Tags: []string{"network"}},
	}
	if got, ok := pickAgent(roster, "un peu de network"); !ok || got != "netter" {
		t.Fatalf("pickAgent = %q,%v want netter,true", got, ok)
	}
}
```

- [ ] **Step 2: Lancer — échoue (pickAgent absent)**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestPickAgent`
Expected: FAIL compilation — `undefined: pickAgent`.

- [ ] **Step 3: Écrire `routing.go`**

```go
package host

import (
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// tokenizeTask lowercases task and splits it into a set of alphanumeric tokens —
// the vocabulary a task's wording offers for matching against agent tags.
func tokenizeTask(task string) map[string]bool {
	set := map[string]bool{}
	for _, tok := range strings.FieldsFunc(strings.ToLower(task), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		set[tok] = true
	}
	return set
}

// pickAgent scores each agent's tags against the task's token set and returns the
// highest-scoring agent. Score = number of the agent's tags present as a token in
// the task. The roster is expected sorted by name (Store.List), so the first agent
// reaching the max score wins — ties break to the lexicographically smallest name,
// deterministically. ok=false when every score is 0 (no agent matches): the host
// refuses rather than falling back to a default, which would be a hidden judgment.
// Pure and LLM-free — this is the whole of the routing "decision" (Model O).
func pickAgent(roster []agent.Agent, task string) (string, bool) {
	tokens := tokenizeTask(task)
	best := ""
	bestScore := 0
	for _, a := range roster {
		score := 0
		for _, tag := range a.Tags {
			if tokens[strings.ToLower(tag)] {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = a.Name
		}
	}
	if bestScore == 0 {
		return "", false
	}
	return best, true
}
```

- [ ] **Step 4: Lancer — passe**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestPickAgent`
Expected: PASS

- [ ] **Step 5: Écrire le test de `parseRoute` (échoue)**

Dans `handoff_test.go`, ajouter :

```go
func TestParseRoute(t *testing.T) {
	cases := []struct {
		name    string
		reply   string
		want    string
		wantOK  bool
	}{
		{"simple", "ok\n⟢ route: écris le module réseau", "écris le module réseau", true},
		{"trim", "⟢ route:   tâche espacée  ", "tâche espacée", true},
		{"empty body", "⟢ route:", "", false},
		{"whitespace body", "⟢ route:    ", "", false},
		{"no marker", "juste une réponse", "", false},
		{"not last line", "⟢ route: x\ndu texte après", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseRoute(c.reply)
			if ok != c.wantOK || got != c.want {
				t.Fatalf("parseRoute(%q) = %q,%v want %q,%v", c.reply, got, ok, c.want, c.wantOK)
			}
		})
	}
}
```

- [ ] **Step 6: Lancer — échoue (parseRoute absent)**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestParseRoute`
Expected: FAIL compilation — `undefined: parseRoute`.

- [ ] **Step 7: Ajouter `routeMarker` + `parseRoute` dans `handoff.go`**

Dans le bloc `const` des markers, ajouter `routeMarker` et mettre à jour le commentaire de priorité :

```go
// Coordination trailers: an agent signals an inter-session intent on a single
// line at the very end of its reply. done has priority over delegate over fanout
// over route over seal over merge over handoff when dispatched (see maybeCoordinate).
const (
	handoffMarker  = "⟢ handoff:"
	delegateMarker = "⟢ delegate:"
	doneMarker     = "⟢ done:"
	sealMarker     = "⟢ seal:"
	mergeMarker    = "⟢ merge:"
	fanoutMarker   = "⟢ fanout:"
	routeMarker    = "⟢ route:"
)
```

Et ajouter la fonction (après `parseFanOut`) :

```go
// parseRoute extracts a routing intent: "⟢ route: <task>". Unlike delegate/handoff,
// NO agent is named — the host picks by capability match. The whole body is the
// task (no em-dash split); an empty body is not a route.
func parseRoute(reply string) (task string, ok bool) {
	body, ok := parseTrailer(reply, routeMarker)
	if !ok || body == "" {
		return "", false
	}
	return body, true
}
```

- [ ] **Step 8: Lancer les deux suites — passent**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'TestPickAgent|TestParseRoute'`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/host/routing.go core/host/routing_test.go core/host/handoff.go core/host/handoff_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): pickAgent capability score + parseRoute trailer (p3 tranche 4)"
```

---

### Task 4: Host — `agentLookup.List` + `coordinator.Route` + dispatch turnloop

**Files:**
- Modify: `/home/shan/dev/herrscher/core/host/coordinator.go`
- Modify: `/home/shan/dev/herrscher/core/host/turnloop.go`
- Test: `/home/shan/dev/herrscher/core/host/coordinator_test.go`
- Test: `/home/shan/dev/herrscher/core/host/hub_test.go`
- Test: `/home/shan/dev/herrscher/core/host/turnloop_test.go`

**Interfaces:**
- Consumes: `contracts.RouteRequest` / `Coordinator.Route` (Task 1), `pickAgent` (Task 3), `agent.Agent.Tags` (Task 2).
- Produces: `coordinator.Route` ; `agentLookup` étendu avec `List() ([]agent.Agent, error)` ; branche de dispatch `⟢ route:` dans `maybeCoordinate`.

- [ ] **Step 1: Étendre les fakes + écrire les tests `Route` (échouent)**

Dans `coordinator_test.go`, étendre `fakeAgents` pour porter un roster et satisfaire `List` :

```go
type fakeAgents struct {
	known  map[string]bool
	roster []agent.Agent
}

func (f fakeAgents) Get(name string) (agent.Agent, bool) {
	return agent.Agent{}, f.known[name]
}

func (f fakeAgents) List() ([]agent.Agent, error) {
	return f.roster, nil
}
```

Puis ajouter les tests `Route` (le harnais `newCoordinator` + fakes existe déjà dans ce fichier ; s'inspirer d'un test `TestFanOut...` voisin pour le montage `creator/agents/wt/sessions/closer/seed`). Le roster fake doit inclure des tags ; la session lead doit exister dans le `sessionLister` fake et avoir un worktree propre.

```go
func TestRoutePicksAndDelegates(t *testing.T) {
	seeded := map[string]string{}
	c := newCoordinator(
		&fakeCreator{},
		fakeAgents{roster: []agent.Agent{
			{Name: "netter", Tags: []string{"network"}},
			{Name: "scripter", Tags: []string{"lua"}},
		}},
		&fakeWTC{clean: true, branches: map[string]bool{}},
		fakeSessions{sessions: []state.Session{
			{Name: "lead", Worktree: "/wt/lead", Project: "p"},
		}},
		&fakeCloser{},
		func(session, task string) bool { seeded[session] = task; return true },
	)
	agentName, session, err := c.Route(context.Background(), contracts.RouteRequest{
		FromSession: "lead", Task: "un module network",
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if agentName != "netter" {
		t.Fatalf("chose %q, want netter", agentName)
	}
	if session != "lead-netter" {
		t.Fatalf("session = %q, want lead-netter", session)
	}
	if seeded[session] != "un module network" {
		t.Fatalf("worker not seeded the task: %v", seeded)
	}
}

func TestRouteEmptyTaskRefused(t *testing.T) {
	c := newCoordinator(&fakeCreator{}, fakeAgents{}, &fakeWTC{clean: true},
		fakeSessions{sessions: []state.Session{{Name: "lead", Worktree: "/wt/lead"}}},
		&fakeCloser{}, func(string, string) bool { return true })
	if _, _, err := c.Route(context.Background(), contracts.RouteRequest{FromSession: "lead", Task: "  "}); err == nil {
		t.Fatal("Route empty task = nil err, want refusal")
	}
}

func TestRouteLeadNotFound(t *testing.T) {
	c := newCoordinator(&fakeCreator{},
		fakeAgents{roster: []agent.Agent{{Name: "netter", Tags: []string{"network"}}}},
		&fakeWTC{clean: true}, fakeSessions{}, &fakeCloser{},
		func(string, string) bool { return true })
	if _, _, err := c.Route(context.Background(), contracts.RouteRequest{FromSession: "ghost", Task: "network"}); err == nil {
		t.Fatal("Route unknown lead = nil err, want refusal")
	}
}

func TestRouteNoMatchRefused(t *testing.T) {
	c := newCoordinator(&fakeCreator{},
		fakeAgents{roster: []agent.Agent{{Name: "scripter", Tags: []string{"lua"}}}},
		&fakeWTC{clean: true, branches: map[string]bool{}},
		fakeSessions{sessions: []state.Session{{Name: "lead", Worktree: "/wt/lead"}}},
		&fakeCloser{}, func(string, string) bool { return true })
	if _, _, err := c.Route(context.Background(), contracts.RouteRequest{FromSession: "lead", Task: "de la doc markdown"}); err == nil {
		t.Fatal("Route no match = nil err, want refusal")
	}
}

func TestRouteDirtyLeadSpawnsNone(t *testing.T) {
	seeded := map[string]string{}
	c := newCoordinator(&fakeCreator{},
		fakeAgents{roster: []agent.Agent{{Name: "netter", Tags: []string{"network"}}}},
		&fakeWTC{clean: false},
		fakeSessions{sessions: []state.Session{{Name: "lead", Worktree: "/wt/lead"}}},
		&fakeCloser{}, func(session, task string) bool { seeded[session] = task; return true })
	if _, _, err := c.Route(context.Background(), contracts.RouteRequest{FromSession: "lead", Task: "network"}); err == nil {
		t.Fatal("Route dirty lead = nil err, want refusal")
	}
	if len(seeded) != 0 {
		t.Fatalf("dirty lead seeded a worker: %v", seeded)
	}
}
```

> NOTE implémenteur : les noms exacts des fakes `fakeSessions`/`fakeCloser` et leurs champs sont ceux DÉJÀ présents dans `coordinator_test.go` (utilisés par les tests `TestFanOut...`, `TestDelegate...`). Réutilise-les tels quels ; n'invente pas de nouveaux fakes. Adapte le montage `newCoordinator(...)` à la signature réelle si elle diffère de l'esquisse ci-dessus. Le nom de worker attendu `lead-netter` suit la convention de `spawn` (`from.Name + "-" + toAgent`), avec un `fakeWTC.branches` vide (aucune collision).

- [ ] **Step 2: Lancer — échoue (List + Route absents)**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestRoute`
Expected: FAIL compilation — `coordinator has no field or method Route` ; `fakeAgents.List` requis par l'interface.

- [ ] **Step 3: Étendre `agentLookup` avec `List`**

Dans `coordinator.go`, l'interface `agentLookup` :

```go
type agentLookup interface {
	Get(name string) (agent.Agent, bool)
	List() ([]agent.Agent, error)
}
```

- [ ] **Step 4: Écrire `coordinator.Route`**

Ajouter (après `FanOut`) dans `coordinator.go` :

```go
// Route picks the best-matching agent for req.Task by a deterministic capability
// score (pickAgent over the agent roster's declared tags — no LLM enters here,
// Model O) and then delegates to it: the chosen worker is a child of the lead off
// its committed tip, the lead stays alive (spawn with parent = lead, like
// Delegate). Guards run before any spawn: an empty task, a missing lead, a roster
// error, or no matching agent each fail with nothing created. spawn's own
// lead-clean guard means a dirty lead yields no worker. Returns the chosen agent
// and the worker's session.
func (c *coordinator) Route(ctx context.Context, req contracts.RouteRequest) (string, string, error) {
	if strings.TrimSpace(req.Task) == "" {
		return "", "", fmt.Errorf("route: empty task")
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", "", fmt.Errorf("route: lead %q not found", req.FromSession)
	}
	roster, err := c.agents.List()
	if err != nil {
		return "", "", fmt.Errorf("route: %w", err)
	}
	chosen, ok := pickAgent(roster, req.Task)
	if !ok {
		return "", "", fmt.Errorf("route: no agent matches task")
	}
	session, err := c.spawn(ctx, from, chosen, req.Task, req.FromSession)
	if err != nil {
		return "", "", err
	}
	return chosen, session, nil
}
```

(`strings` et `fmt` sont déjà importés dans `coordinator.go`.)

- [ ] **Step 5: Lancer — passe**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestRoute`
Expected: PASS

- [ ] **Step 6: Ajouter la branche de dispatch dans `turnloop.go`**

Dans `maybeCoordinate`, **entre** le bloc `parseFanOut` et le bloc `parseSeal`, insérer :

```go
	if task, ok := parseRoute(reply); ok {
		if toAgent, session, err := d.coordinator.Route(ctx, contracts.RouteRequest{
			FromSession: d.name, Task: task,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "route refusé: " + err.Error()})
		} else {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "routé vers " + toAgent + " : " + session})
		}
		return
	}
```

Et mettre à jour le commentaire de priorité de `maybeCoordinate` :

```go
// A single trailer per turn: done wins over delegate over fanout over route over seal over merge over handoff.
```

- [ ] **Step 7: Ajouter `Route` aux fakes coordinator + le test de dispatch (échouent)**

Dans `hub_test.go`, ajouter à `stubForgetCoord` :

```go
func (s *stubForgetCoord) Route(context.Context, contracts.RouteRequest) (string, string, error) {
	return "", "", nil
}
```

Dans `turnloop_test.go`, ajouter à `erroringCoord` :

```go
func (e *erroringCoord) Route(context.Context, contracts.RouteRequest) (string, string, error) {
	return "", "", e.err
}
```

Et à `recordingCoord` : ajouter le champ `routes []contracts.RouteRequest` dans la struct, puis :

```go
func (r *recordingCoord) Route(_ context.Context, req contracts.RouteRequest) (string, string, error) {
	r.routes = append(r.routes, req)
	return "netter", req.FromSession + "-netter", nil
}
```

Puis le test de dispatch (à côté de `TestDriverInvokesCoordinatorOnFanOutTrailer`) :

```go
func TestDriverInvokesCoordinatorOnRouteTrailer(t *testing.T) {
	rc := &recordingCoord{}
	d := &sessionDriver{name: "lead", coordinator: rc}
	d.maybeCoordinate(context.Background(), "voici mon plan\n⟢ route: un module réseau")
	if len(rc.routes) != 1 {
		t.Fatalf("Route calls = %d, want 1", len(rc.routes))
	}
	if rc.routes[0].FromSession != "lead" || rc.routes[0].Task != "un module réseau" {
		t.Fatalf("Route req = %+v", rc.routes[0])
	}
}
```

> NOTE implémenteur : `sessionDriver{name:..., coordinator:...}` littéral est le montage utilisé par les tests de dispatch voisins ; si `TestDriverInvokesCoordinatorOnFanOutTrailer` construit son driver autrement, copie SON montage exact.

- [ ] **Step 8: Lancer — passe**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'TestRoute|TestDriverInvokesCoordinatorOnRouteTrailer'`
Expected: PASS

- [ ] **Step 9: Suite complète des deux modules**

Run: `cd /home/shan/dev/herrscher && go test ./... && cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: PASS partout.

- [ ] **Step 10: Commit**

```bash
cd /home/shan/dev/herrscher && git add core/host/coordinator.go core/host/turnloop.go core/host/coordinator_test.go core/host/hub_test.go core/host/turnloop_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): coordinator.Route + ⟢ route dispatch (p3 tranche 4)"
```

---

## Self-Review

- **Spec coverage :** D1 (fichier TAGS) → Task 2 ; D2 (Agent.Tags) → Task 2 ; D3 (trailer + port) → Tasks 1+3 ; D4 (pickAgent) → Task 3 ; D5 (sémantique Delegate) → Task 4 (Route réutilise spawn parent=lead) ; D6 (agentLookup.List) → Task 4 ; D7 (ordre dispatch + statuts) → Tasks 3+4. Couvert.
- **Pas de placeholder :** tout le code de production est fourni ; les seuls points laissés à l'implémenteur sont les montages de fakes DÉJÀ existants (signalés en NOTE), à réutiliser tels quels.
- **Cohérence des types :** `Route` retourne `(string, string, error)` partout (interface, coordinator, tous les stubs, fakeCoordinator, recordingCoord, erroringCoord, stubForgetCoord). `pickAgent(roster, task) (string, bool)`. `parseRoute(reply) (string, bool)`. `readTags(home) []string`. `Agent.Tags []string`.
