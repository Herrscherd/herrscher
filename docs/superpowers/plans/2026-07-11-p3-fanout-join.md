# P3 fan-out + join Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Donner au lead un suivi déterministe d'une cohorte de workers parallèles : chaque `Report` seed le lead avec « (done/total) », et « tous les workers ont livré » quand le dernier arrive.

**Architecture:** Le fan-out ne demande aucun code neuf (c'est `Delegate` émis N fois, déjà livré). Le join vit entièrement dans `core/host/coordinator.go` : un état en mémoire `reported map[string]map[string]bool` (parent → workers livrés) sous `sync.Mutex`, `Report` enrichi pour compter, et `forget(name)` (purge à la fermeture) branchée sur `hub.goDead`. Aucun changement dans `herrscher-contracts` ni dans la persistance `state.go`.

**Tech Stack:** Go (stdlib : `fmt`, `sync`, `context`). Tests `go test -race`.

## Global Constraints

- **Générique OSS** : zéro vocabulaire métier Roblox/Neublox dans le code ou les messages (« worker »/« lead » uniquement).
- **Aucun changement `herrscher-contracts`** : `Report` garde sa signature `(ctx, contracts.ReportRequest) (string, error)` ; `forget` est host-interne, PAS dans le port `contracts.Coordinator`.
- **CI verte** : `gofmt -l core/` vide, `go vet ./...` propre, `go build ./...`, `go test -race ./...` verts, `go mod tidy` laisse `go.mod` inchangé.
- **Concurrence sûre** : `reported` n'est jamais lu ni écrit hors `mu`.
- **Identité de commit** : `git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit`, footers `Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc` + `Co-Authored-By: Claude <noreply@anthropic.com>`.
- **Message de livraison exact** : `"<W> a terminé sur session/<W> (<done>/<total>) — <résumé>"`, suffixe ` " — tous les workers ont livré"` ssi `done >= total`.

---

## File Structure

- `core/host/coordinator.go` — champs `reported`/`mu` + init dans `newCoordinator` ; `Report` enrichi (comptage) ; nouvelle méthode `forget`.
- `core/host/hub.go` — interface host-interne `forgetter`, appel dans `goDead`.
- `core/host/coordinator_test.go` — tests du comptage, idempotence, purge.
- `core/host/hub_test.go` (ou fichier de test hub existant) — test que `goDead` appelle `forget`.

État actuel vérifié (source) :

`newCoordinator` (coordinator.go:53) :
```go
func newCoordinator(creator sessionCreator, agents agentLookup, wt cleanBrancher, sessions sessionLister, closer sessionCloser, seed func(string, string) bool) *coordinator {
	return &coordinator{creator: creator, agents: agents, wt: wt, sessions: sessions, closer: closer, seed: seed}
}
```

`Report` actuel (coordinator.go:164-192), fin de corps :
```go
	if _, ok := findByName(sessions, from.Parent); !ok {
		return "", fmt.Errorf("report: parent %q of %q not found", from.Parent, req.FromSession)
	}
	branch := c.wt.Branch(req.FromSession)
	msg := fmt.Sprintf("%s a terminé sur %s — %s", req.FromSession, branch, req.Summary)
	if !c.seedWithRetry(ctx, from.Parent, msg) {
		return "", fmt.Errorf("report: delivery to parent %q timed out", from.Parent)
	}
	return from.Parent, nil
```

`hub.goDead` actuel (hub.go:72-80) :
```go
func (h *hub) goDead(name string) {
	h.mu.Lock()
	cancel := h.live[name]
	delete(h.live, name)
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
```

Harnais de test coordinator (coordinator_test.go:52-63) — `newTestCoordinator(cr, known []string, clean bool, sessions []state.Session, seeded *[]string)` ; le `seed` capture `sess+"|"+task` dans `*seeded`. Les tests sont dans `package host`, donc l'accès au champ privé `c.reported` est permis.

---

### Task 1: État `reported` + comptage dans `Report`

**Files:**
- Modify: `core/host/coordinator.go` (struct `coordinator` ~44-51, `newCoordinator` ~53-55, `Report` ~186-191)
- Test: `core/host/coordinator_test.go`

**Interfaces:**
- Consumes: `findByName(sessions, name)`, `c.wt.Branch(name)`, `c.seedWithRetry(ctx, session, task)` (existants).
- Produces: champ `reported map[string]map[string]bool` + `mu sync.Mutex` sur `*coordinator` (Task 2 les réutilise) ; message de livraison au format `(done/total)` + suffixe.

- [ ] **Step 1: Écrire les tests qui échouent**

Ajouter à `core/host/coordinator_test.go` :

```go
func TestReportCountsSiblingProgress(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
			{Name: "w3", Worktree: "/wt/w3", Parent: "lead"},
		}, &seeded)

	for _, w := range []string{"w1", "w2", "w3"} {
		if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: w, Summary: "ok"}); err != nil {
			t.Fatalf("report %s: %v", w, err)
		}
	}
	if len(seeded) != 3 {
		t.Fatalf("3 livraisons attendues: %v", seeded)
	}
	if !strings.Contains(seeded[0], "(1/3)") {
		t.Fatalf("premier compte faux: %q", seeded[0])
	}
	if !strings.Contains(seeded[1], "(2/3)") {
		t.Fatalf("deuxième compte faux: %q", seeded[1])
	}
	if !strings.Contains(seeded[2], "(3/3)") || !strings.Contains(seeded[2], "tous les workers ont livré") {
		t.Fatalf("dernier compte/suffixe faux: %q", seeded[2])
	}
}

func TestReportAllDoneSuffixOnlyOnLast(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
		}, &seeded)

	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
		t.Fatalf("report w1: %v", err)
	}
	if strings.Contains(seeded[0], "tous les workers ont livré") {
		t.Fatalf("suffixe prématuré au 1er report: %q", seeded[0])
	}
	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w2", Summary: "ok"}); err != nil {
		t.Fatalf("report w2: %v", err)
	}
	if !strings.Contains(seeded[1], "tous les workers ont livré") {
		t.Fatalf("suffixe absent au dernier report: %q", seeded[1])
	}
}

func TestReportDoubleReportIdempotent(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
		}, &seeded)

	for i := 0; i < 2; i++ {
		if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
			t.Fatalf("report w1 #%d: %v", i, err)
		}
	}
	if !strings.Contains(seeded[1], "(1/2)") {
		t.Fatalf("double report devrait rester (1/2): %q", seeded[1])
	}
	if strings.Contains(seeded[1], "tous les workers ont livré") {
		t.Fatalf("double report ne doit pas déclencher tous-livrés: %q", seeded[1])
	}
}
```

- [ ] **Step 2: Lancer les tests, vérifier l'échec de compilation/assert**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'TestReport(CountsSiblingProgress|AllDoneSuffixOnlyOnLast|DoubleReportIdempotent)'`
Expected: FAIL — les messages ne contiennent pas encore `(1/3)` etc.

- [ ] **Step 3: Ajouter les champs et l'init**

Dans `core/host/coordinator.go`, importer `sync` (ajouter `"sync"` au bloc d'import). Struct `coordinator` :

```go
type coordinator struct {
	creator  sessionCreator
	agents   agentLookup
	wt       cleanBrancher
	sessions sessionLister
	closer   sessionCloser
	seed     func(session, task string) bool

	mu       sync.Mutex
	reported map[string]map[string]bool // parent → { worker → true } livrés
}
```

`newCoordinator` :

```go
func newCoordinator(creator sessionCreator, agents agentLookup, wt cleanBrancher, sessions sessionLister, closer sessionCloser, seed func(string, string) bool) *coordinator {
	return &coordinator{
		creator: creator, agents: agents, wt: wt, sessions: sessions, closer: closer, seed: seed,
		reported: map[string]map[string]bool{},
	}
}
```

- [ ] **Step 4: Enrichir `Report`**

Dans `Report`, remplacer le bloc final (depuis `branch := c.wt.Branch(...)` jusqu'au `msg := ...`) par :

```go
	c.mu.Lock()
	if c.reported[from.Parent] == nil {
		c.reported[from.Parent] = map[string]bool{}
	}
	c.reported[from.Parent][req.FromSession] = true
	done := len(c.reported[from.Parent])
	c.mu.Unlock()

	total := 0
	for _, s := range sessions {
		if s.Parent == from.Parent {
			total++
		}
	}

	branch := c.wt.Branch(req.FromSession)
	msg := fmt.Sprintf("%s a terminé sur %s (%d/%d) — %s", req.FromSession, branch, done, total, req.Summary)
	if done >= total {
		msg += " — tous les workers ont livré"
	}
	if !c.seedWithRetry(ctx, from.Parent, msg) {
		return "", fmt.Errorf("report: delivery to parent %q timed out", from.Parent)
	}
	return from.Parent, nil
```

(`total` compte les frères depuis le snapshot déjà pris — le worker appelant a `Parent == P`, donc `total ≥ 1`.)

- [ ] **Step 5: Lancer les tests, vérifier le succès**

Run: `cd /home/shan/dev/herrscher && go test -race ./core/host/`
Expected: PASS — tous les tests host verts (nouveaux + `TestReport*` existants, dont `TestReportDeliversBranchRefAndSummaryToParent` qui tolère l'ajout `(1/1)`/suffixe car il n'assert que des sous-chaînes).

- [ ] **Step 6: gofmt + commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/host/coordinator.go core/host/coordinator_test.go
git add core/host/coordinator.go core/host/coordinator_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): join — Report compte la cohorte (done/total + tous livrés)

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: Purge `forget` (anti-fuite + justesse du compte)

**Files:**
- Modify: `core/host/coordinator.go` (nouvelle méthode `forget` après `Report`)
- Test: `core/host/coordinator_test.go`

**Interfaces:**
- Consumes: `c.reported`, `c.mu` (Task 1).
- Produces: méthode `func (c *coordinator) forget(name string)` (Task 3 l'appelle via l'interface `forgetter`).

- [ ] **Step 1: Écrire les tests qui échouent**

Ajouter à `core/host/coordinator_test.go` :

```go
func TestForgetPurgesLeadCohort(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
		}, &seeded)

	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
		t.Fatalf("report w1: %v", err)
	}
	if len(c.reported["lead"]) == 0 {
		t.Fatalf("cohorte du lead devrait être peuplée avant purge")
	}
	c.forget("lead")
	if c.reported["lead"] != nil {
		t.Fatalf("forget(lead) devrait jeter la cohorte, got %v", c.reported["lead"])
	}
}

func TestForgetRemovesWorkerKeepsCountConsistent(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
		}, &seeded)

	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
		t.Fatalf("report w1: %v", err)
	}
	c.forget("w1") // w1 se ferme après avoir livré
	if c.reported["lead"]["w1"] {
		t.Fatalf("forget(w1) devrait retirer w1 des livrés")
	}
	// w2 livre ensuite : done ne doit PAS compter le w1 périmé.
	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w2", Summary: "ok"}); err != nil {
		t.Fatalf("report w2: %v", err)
	}
	if !strings.Contains(seeded[1], "(1/2)") {
		t.Fatalf("w1 périmé ne doit pas gonfler done: %q", seeded[1])
	}
	if strings.Contains(seeded[1], "tous les workers ont livré") {
		t.Fatalf("pas de faux tous-livrés après purge: %q", seeded[1])
	}
}
```

- [ ] **Step 2: Lancer les tests, vérifier l'échec**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'TestForget'`
Expected: FAIL — `c.forget undefined (type *coordinator has no field or method forget)`.

- [ ] **Step 3: Implémenter `forget`**

Dans `core/host/coordinator.go`, après la méthode `Report` :

```go
// forget purge l'état de join d'une session qui se ferme. Deux effets : si `name`
// était un lead, sa cohorte est jetée (anti-fuite mémoire) ; si `name` était un
// worker, il sort des livrés de son parent — sinon un worker livré puis fermé
// resterait compté dans `done` alors que `total` (frères vivants) a baissé, faussant
// le « tous les workers ont livré ». Garde l'invariant done ≤ total.
func (c *coordinator) forget(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.reported, name)
	for _, workers := range c.reported {
		delete(workers, name)
	}
}
```

- [ ] **Step 4: Lancer les tests, vérifier le succès**

Run: `cd /home/shan/dev/herrscher && go test -race ./core/host/ -run 'TestForget'`
Expected: PASS.

- [ ] **Step 5: gofmt + commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/host/coordinator.go core/host/coordinator_test.go
git add core/host/coordinator.go core/host/coordinator_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): forget purge l'état de join à la fermeture d'une session

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Brancher `forget` sur `hub.goDead`

**Files:**
- Modify: `core/host/hub.go` (interface `forgetter`, `goDead` ~72-80)
- Test: `core/host/hub_test.go` (**existe déjà** — `package host`, imports `context`/`testing`/`contracts` déjà présents ; y ajouter le type + le test, sans retoucher le bloc `package`/`import`)

**Interfaces:**
- Consumes: `(*coordinator).forget(string)` (Task 2), champ `hub.coordinator contracts.Coordinator`.
- Produces: interface host-interne `forgetter interface { forget(string) }` ; `goDead` déclenche `forget` par type-assert. Le port `contracts.Coordinator` reste intact.

- [ ] **Step 1: Écrire le test qui échoue**

`core/host/hub_test.go` existe déjà (`package host`, avec `context`/`testing`/`contracts` importés). **Ajouter uniquement** le type + la fonction ci-dessous à la fin du fichier — ne PAS retoucher le bloc `package`/`import` (les trois imports nécessaires sont déjà là).

```go
// stubForgetCoord implémente contracts.Coordinator (uniquement ce dont goDead a
// besoin via type-assert) ET forget, pour prouver le câblage.
type stubForgetCoord struct{ forgotten []string }

func (s *stubForgetCoord) Handoff(context.Context, contracts.HandoffRequest) (string, error) {
	return "", nil
}
func (s *stubForgetCoord) Delegate(context.Context, contracts.DelegateRequest) (string, error) {
	return "", nil
}
func (s *stubForgetCoord) Report(context.Context, contracts.ReportRequest) (string, error) {
	return "", nil
}
func (s *stubForgetCoord) forget(name string) { s.forgotten = append(s.forgotten, name) }

func TestGoDeadCallsForget(t *testing.T) {
	stub := &stubForgetCoord{}
	h := &hub{
		coordinator: stub,
		live:        map[string]context.CancelFunc{},
	}
	h.goDead("worker")
	if len(stub.forgotten) != 1 || stub.forgotten[0] != "worker" {
		t.Fatalf("goDead devrait appeler forget(\"worker\"): %v", stub.forgotten)
	}
}
```

- [ ] **Step 2: Lancer le test, vérifier l'échec**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'TestGoDeadCallsForget'`
Expected: FAIL — `stub.forgotten` reste vide (goDead n'appelle rien).

- [ ] **Step 3: Ajouter l'interface + l'appel**

Dans `core/host/hub.go`, ajouter l'interface près du type `hub` (après la déclaration du struct, avant `newHub`) :

```go
// forgetter est satisfaite par *coordinator ; elle vit hors du port
// contracts.Coordinator car forget est un détail host-interne (purge de l'état de
// join), pas une capacité exposée aux gateways.
type forgetter interface{ forget(string) }
```

Puis compléter `goDead` :

```go
func (h *hub) goDead(name string) {
	h.mu.Lock()
	cancel := h.live[name]
	delete(h.live, name)
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if f, ok := h.coordinator.(forgetter); ok {
		f.forget(name)
	}
}
```

- [ ] **Step 4: Lancer le test, vérifier le succès**

Run: `cd /home/shan/dev/herrscher && go test -race ./core/host/ -run 'TestGoDeadCallsForget'`
Expected: PASS.

- [ ] **Step 5: Vérification CI complète + commit**

```bash
cd /home/shan/dev/herrscher
gofmt -l core/                      # attendu : aucune sortie
go vet ./...                        # attendu : propre
go test -race ./...                 # attendu : tout vert
go mod tidy && git diff --quiet go.mod && echo "go.mod clean"
gofmt -w core/host/hub.go core/host/hub_test.go
git add core/host/hub.go core/host/hub_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): goDead appelle forget via l'interface host-interne forgetter

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Notes de coordination

- **Fan-out** : rien à coder — le lead émet `⟢ delegate: <agent> — <tâche>` sur N tours (primitive `Delegate` déjà livrée). Ce plan ne couvre QUE le join.
- **Best-effort assumé** : un « tous livrés » prématuré (worker qui finit avant que le lead ait distribué toute la cohorte) est un indice bénin, pas une garantie. Pas de scellage de cohorte (décision spec).
- **Hors périmètre** : agrégation réelle (merge des branches workers), barrière dure, superviseur→workers, routage LLM — tranches ultérieures.
