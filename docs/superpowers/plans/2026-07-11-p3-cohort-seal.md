# P3 Scellage de cohorte — Plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Donner au lead un trailer `⟢ seal: <N>` qui fige le nombre attendu de workers, transformant le « tous livrés » best-effort du join en barrière déterministe.

**Architecture:** Le scellage suit le Modèle O : l'agent-lead signale via un trailer, le host parse, le `coordinator` hub-level exécute. Comme chaque trailer déclenché par l'agent mappe une méthode de port (`done→Report`, `delegate→Delegate`, `merge→Merge`), `seal→Seal` est une nouvelle méthode du port `Coordinator` (touche `herrscher-contracts`). Le coordinator gagne un champ `expected map[string]int` (parent→N) sous le `mu` existant ; `Report` lit ce N (s'il existe) au lieu du compte de frères vivants, et `forget` le purge.

**Tech Stack:** Go, deux modules — `github.com/Herrscherd/herrscher-contracts` (le port) et `github.com/Herrscherd/herrscher` (l'implémentation host). `go.work` local (`/home/shan/dev/go.work`) résout `herrscher-contracts` depuis le checkout local, donc la Task 1 (contracts) est visible immédiatement par les tasks herrscher.

## Global Constraints

- **Générique OSS** : zéro connaissance métier Roblox/Neublox dans le code ou les messages — vocabulaire « worker »/« lead »/« cohorte » uniquement.
- **`herrscher-contracts` touché** (assumé) : nouvelle méthode `Seal` + `SealRequest` ; les autres signatures du port `Coordinator` (`Handoff`/`Delegate`/`Report`/`Merge`) restent inchangées.
- **Wording différencié** : scellé & complet → suffixe exact `" — cohorte complète"` ; non scellé & complet → suffixe exact `" — tous les workers ont livré"` (inchangé).
- **Garde `N >= cohorte courante`** : `Seal` refuse `Expected` inférieur au nombre de frères déjà délégués sous ce lead, en plus de `Expected > 0`.
- **Concurrence sûre** : `expected` n'est jamais lu ni écrit hors `mu` (comme `reported`) — vérifiable au `-race`.
- **Best-effort non scellé inchangé** : sans `seal`, `Report` garde bit-pour-bit son calcul `total = frères vivants` et son suffixe actuels.
- **CI verte des deux côtés** : `gofmt -l` vide, `go vet ./...` propre, `go build ./...`, `go test -race ./...` verts, `go mod tidy` laisse `go.mod` inchangé (herrscher ET contracts).
- **1 unité testable = 1 commit.** Commit avec l'identité : `git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit`. Footers obligatoires sur chaque commit :
  ```
  Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
  Co-Authored-By: Claude <noreply@anthropic.com>
  ```
- **Ne jamais** : fabriquer des données, affaiblir le code de prod pour faire passer un test, supprimer un commentaire « pourquoi ».

---

## File Structure

- `herrscher-contracts/coordinator.go` — ajoute `SealRequest` + méthode `Seal` sur l'interface `Coordinator`.
- `herrscher-contracts/coordinator_test.go` — étend les fakes de port (`fakeCoordinator`, `mergeStub`) avec `Seal` + un test de surface.
- `herrscher/core/host/coordinator.go` — champ `expected`, méthode `Seal`, `Report` enrichi (calcul `total` + wording), `forget` purge `expected`.
- `herrscher/core/host/coordinator_test.go` — tests `Seal` + tests `Report` scellé/non-scellé + purge.
- `herrscher/core/host/handoff.go` — const `sealMarker` + `parseSeal`.
- `herrscher/core/host/handoff_test.go` — `TestParseSeal`.
- `herrscher/core/host/turnloop.go` — `maybeCoordinate` insère la branche `parseSeal`.
- `herrscher/core/host/turnloop_test.go` — fakes `recordingCoord`/`erroringCoord` étendus + test de dispatch seal.
- `herrscher/core/host/hub_test.go` — fake `stubForgetCoord` étendu avec `Seal` inerte.

**Ordre imposé (compile-cohérence).** Dès que la Task 1 ajoute `Seal` à l'interface `Coordinator`, le paquet `herrscher/core/host` ne compile plus tant que le vrai `*coordinator` **et** les trois fakes de test host n'implémentent pas `Seal`. Donc : Task 1 (contracts) → Task 2 (coordinator + fakes host, rétablit le build) → Task 3 (parsing + dispatch).

---

### Task 1: Port contracts — `SealRequest` + `Seal`

**Files:**
- Modify: `herrscher-contracts/coordinator.go`
- Test: `herrscher-contracts/coordinator_test.go`

**Interfaces:**
- Consumes: l'interface `Coordinator` existante (`Handoff`/`Delegate`/`Report`/`Merge`) et le type `MergeRequest`.
- Produces: `type SealRequest struct { FromSession string; Expected int }` et la méthode `Seal(ctx context.Context, req SealRequest) (lead string, err error)` sur `Coordinator`. La Task 2 implémente cette méthode sur `*coordinator` ; la Task 3 l'appelle via l'interface dans `maybeCoordinate`.

- [ ] **Step 1: Écrire le test de surface qui échoue**

Dans `herrscher-contracts/coordinator_test.go`, ajouter à la fin :

```go
// sealStub carries the full Coordinator surface (incl. Seal) to assert the port
// shape at compile time.
type sealStub struct{}

func (sealStub) Handoff(context.Context, HandoffRequest) (string, error)   { return "", nil }
func (sealStub) Delegate(context.Context, DelegateRequest) (string, error) { return "", nil }
func (sealStub) Report(context.Context, ReportRequest) (string, error)     { return "", nil }
func (sealStub) Merge(context.Context, MergeRequest) (string, error)       { return "", nil }
func (sealStub) Seal(context.Context, SealRequest) (string, error)         { return "", nil }

func TestCoordinatorPortIncludesSeal(t *testing.T) {
	var _ Coordinator = sealStub{}
	req := SealRequest{FromSession: "lead", Expected: 5}
	if req.FromSession != "lead" || req.Expected != 5 {
		t.Fatalf("SealRequest fields not wired: %+v", req)
	}
}
```

Ajouter aussi la méthode `Seal` au fake existant `fakeCoordinator` (juste après sa méthode `Merge`), pour qu'il continue de satisfaire l'interface :

```go
func (f *fakeCoordinator) Seal(_ context.Context, req SealRequest) (string, error) {
	f.gotSeal = req
	return req.FromSession, nil
}
```

et déclarer le champ `gotSeal SealRequest` dans la struct `fakeCoordinator`.

- [ ] **Step 2: Lancer le test — échec de compilation attendu**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: FAIL — `undefined: SealRequest`, `Coordinator (missing Seal method)`.

- [ ] **Step 3: Implémenter le port**

Dans `herrscher-contracts/coordinator.go`, ajouter le type (près de `MergeRequest`) :

```go
// SealRequest is a lead declaring how many workers its cohort expects, turning
// the best-effort join count into a deterministic barrier.
type SealRequest struct {
	FromSession string // the lead that declares
	Expected    int    // N expected (> 0)
}
```

et la méthode sur l'interface `Coordinator` (après `Merge`) :

```go
	// Seal records the number of workers FromSession's cohort expects, so the
	// join can report "cohort complete" deterministically instead of best-effort.
	Seal(ctx context.Context, req SealRequest) (lead string, err error)
```

- [ ] **Step 4: Lancer le test — passe**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: PASS (tous les tests contracts verts).

- [ ] **Step 5: Vérifs CI + commit**

```bash
cd /home/shan/dev/herrscher-contracts
gofmt -l . && go vet ./... && go build ./... && go test -race ./...
go mod tidy && git diff --exit-code go.mod go.sum
git add coordinator.go coordinator_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(coordinator): Seal port method + SealRequest (P3 cohort seal)

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```
Expected: `gofmt -l` n'affiche rien, vet/build/test verts, `git diff --exit-code` sur go.mod réussit (0), commit créé.

---

### Task 2: Coordinator — `expected`, `Seal`, `Report` enrichi, `forget` purge

**Files:**
- Modify: `herrscher/core/host/coordinator.go`
- Modify: `herrscher/core/host/hub_test.go` (fake `stubForgetCoord`)
- Modify: `herrscher/core/host/turnloop_test.go` (fakes `recordingCoord`, `erroringCoord`)
- Test: `herrscher/core/host/coordinator_test.go`

**Interfaces:**
- Consumes: `contracts.SealRequest` + la méthode `Seal` du port (Task 1). Le harnais de test existant : `newTestCoordinator(cr *fakeCreator, known []string, clean bool, sessions []state.Session, seeded *[]string) *coordinator` (le `seed` pousse `sess+"|"+task` dans `*seeded`). Le champ `mu sync.Mutex` et `reported map[string]map[string]bool` existent déjà sur `*coordinator`, initialisés dans `newCoordinator`.
- Produces: `*coordinator` implémente désormais `Seal`, donc satisfait `contracts.Coordinator` → le paquet `core/host` recompile. `Report` lit `expected[parent]`.

- [ ] **Step 1: Écrire les tests qui échouent**

Dans `herrscher/core/host/coordinator_test.go`, ajouter :

```go
func TestSealRecordsExpected(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)

	lead, err := c.Seal(context.Background(), contracts.SealRequest{FromSession: "lead", Expected: 3})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if lead != "lead" {
		t.Fatalf("seal should return the lead name, got %q", lead)
	}
	if c.expected["lead"] != 3 {
		t.Fatalf("expected[lead] should be 3, got %d", c.expected["lead"])
	}
}

func TestSealRefusesNonPositive(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)

	if _, err := c.Seal(context.Background(), contracts.SealRequest{FromSession: "lead", Expected: 0}); err == nil {
		t.Fatal("seal with Expected=0 should be refused")
	}
	if _, ok := c.expected["lead"]; ok {
		t.Fatal("refused seal must record nothing")
	}
}

func TestSealRefusesBelowCurrentCohort(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
			{Name: "w3", Worktree: "/wt/w3", Parent: "lead"},
		}, &seeded)

	if _, err := c.Seal(context.Background(), contracts.SealRequest{FromSession: "lead", Expected: 2}); err == nil {
		t.Fatal("seal below current cohort size (3) should be refused")
	}
	if _, ok := c.expected["lead"]; ok {
		t.Fatal("refused seal must record nothing")
	}
}

func TestSealUnknownLead(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, nil, &seeded)
	if _, err := c.Seal(context.Background(), contracts.SealRequest{FromSession: "ghost", Expected: 1}); err == nil {
		t.Fatal("seal on unknown lead should be refused")
	}
}

func TestReportUsesSealedTotal(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
			{Name: "w3", Worktree: "/wt/w3", Parent: "lead"},
		}, &seeded)

	if _, err := c.Seal(context.Background(), contracts.SealRequest{FromSession: "lead", Expected: 3}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	for _, w := range []string{"w1", "w2", "w3"} {
		if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: w, Summary: "ok"}); err != nil {
			t.Fatalf("report %s: %v", w, err)
		}
	}
	if !strings.Contains(seeded[0], "(1/3)") || !strings.Contains(seeded[1], "(2/3)") {
		t.Fatalf("sealed counts wrong: %v", seeded)
	}
	if !strings.Contains(seeded[2], "(3/3)") || !strings.Contains(seeded[2], "cohorte complète") {
		t.Fatalf("sealed completion wrong: %q", seeded[2])
	}
}

func TestReportSealedCompleteWording(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
		}, &seeded)

	if _, err := c.Seal(context.Background(), contracts.SealRequest{FromSession: "lead", Expected: 1}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
		t.Fatalf("report: %v", err)
	}
	if !strings.Contains(seeded[0], "cohorte complète") {
		t.Fatalf("sealed suffix should be 'cohorte complète': %q", seeded[0])
	}
	if strings.Contains(seeded[0], "tous les workers ont livré") {
		t.Fatalf("sealed cohort must not use the best-effort wording: %q", seeded[0])
	}
}

func TestReportUnsealedStaysBestEffort(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
		}, &seeded)

	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
		t.Fatalf("report: %v", err)
	}
	if !strings.Contains(seeded[0], "(1/1)") || !strings.Contains(seeded[0], "tous les workers ont livré") {
		t.Fatalf("unsealed cohort must keep best-effort wording: %q", seeded[0])
	}
	if strings.Contains(seeded[0], "cohorte complète") {
		t.Fatalf("unsealed cohort must not claim 'cohorte complète': %q", seeded[0])
	}
}

func TestForgetPurgesSeal(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)
	if _, err := c.Seal(context.Background(), contracts.SealRequest{FromSession: "lead", Expected: 2}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	c.forget("lead")
	if _, ok := c.expected["lead"]; ok {
		t.Fatal("forget(lead) should purge expected[lead]")
	}
}
```

Étendre les trois fakes host qui implémentent `contracts.Coordinator`, sinon le paquet ne compile pas :

Dans `herrscher/core/host/hub_test.go`, après `stubForgetCoord.Merge` :

```go
func (s *stubForgetCoord) Seal(context.Context, contracts.SealRequest) (string, error) {
	return "", nil
}
```

Dans `herrscher/core/host/turnloop_test.go`, ajouter le champ `seals []contracts.SealRequest` à `recordingCoord` et sa méthode (après `Merge`) :

```go
func (r *recordingCoord) Seal(_ context.Context, req contracts.SealRequest) (string, error) {
	r.seals = append(r.seals, req)
	return req.FromSession, nil
}
```

et pour `erroringCoord` (après `Merge`) :

```go
func (e *erroringCoord) Seal(context.Context, contracts.SealRequest) (string, error) {
	return "", e.err
}
```

- [ ] **Step 2: Lancer les tests — échec attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'Seal|Report' -v`
Expected: FAIL — `c.Seal undefined` / `c.expected undefined`.

- [ ] **Step 3: Implémenter dans `coordinator.go`**

Ajouter le champ à la struct `coordinator` (sous `reported`) :

```go
	expected map[string]int             // parent → N attendu (sous mu), figé par Seal
```

L'initialiser dans `newCoordinator` (à côté de `reported: map[string]map[string]bool{}`) :

```go
		expected: map[string]int{},
```

Ajouter la méthode `Seal` (après `Merge`) :

```go
// Seal records how many workers FromSession's cohort expects, turning the join's
// best-effort "all delivered" into a deterministic barrier: once sealed, Report
// reports "cohorte complète" only at done >= N. Guards run before any effect on
// one atomic snapshot: lead known → N > 0 → N >= current cohort size (refusing an
// under-seal at the source). Re-seal is last-wins. Seal does not seed into the
// lead's turn — like Delegate it changes state and fans a status; the barrier
// surfaces in later Report messages.
func (c *coordinator) Seal(ctx context.Context, req contracts.SealRequest) (string, error) {
	sessions := c.sessions.SnapshotSessions()
	if _, ok := findByName(sessions, req.FromSession); !ok {
		return "", fmt.Errorf("seal: lead %q not found", req.FromSession)
	}
	if req.Expected <= 0 {
		return "", fmt.Errorf("seal refused: expected must be > 0")
	}
	cohort := 0
	for _, s := range sessions {
		if s.Parent == req.FromSession {
			cohort++
		}
	}
	if req.Expected < cohort {
		return "", fmt.Errorf("seal refused: expected %d below current cohort size %d", req.Expected, cohort)
	}
	c.mu.Lock()
	c.expected[req.FromSession] = req.Expected
	c.mu.Unlock()
	return req.FromSession, nil
}
```

Modifier `Report` : remplacer le bloc de calcul de `total` + le suffixe. Aujourd'hui (lignes ~205-216) :

```go
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
```

Le remplacer par (le lookup `expected` se fait sous `mu`, comme `reported`) :

```go
	c.mu.Lock()
	sealed, isSealed := c.expected[from.Parent]
	c.mu.Unlock()

	total := 0
	if isSealed {
		total = sealed
	} else {
		for _, s := range sessions {
			if s.Parent == from.Parent {
				total++
			}
		}
	}

	branch := c.wt.Branch(req.FromSession)
	msg := fmt.Sprintf("%s a terminé sur %s (%d/%d) — %s", req.FromSession, branch, done, total, req.Summary)
	if done >= total {
		if isSealed {
			msg += " — cohorte complète"
		} else {
			msg += " — tous les workers ont livré"
		}
	}
```

Modifier `forget` pour purger `expected` — ajouter, juste après `delete(c.reported, name)` (la ligne qui jette la cohorte d'un lead) :

```go
	delete(c.expected, name)
```

(Le worker n'a pas d'entrée dans `expected` — seul un lead en a une —, donc seule cette ligne est nécessaire ; elle vit déjà sous `mu` que `forget` détient.)

- [ ] **Step 4: Lancer les tests — passent**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'Seal|Report|Forget' -v`
Expected: PASS (nouveaux tests seal + tests Report/Forget existants toujours verts).

- [ ] **Step 5: Suite complète + commit**

```bash
cd /home/shan/dev/herrscher
go test ./core/host/
gofmt -l core/host/ && go vet ./core/host/... && go build ./...
git add core/host/coordinator.go core/host/coordinator_test.go core/host/hub_test.go core/host/turnloop_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): coordinator.Seal + Report sealed barrier (P3 cohort seal)

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```
Expected: paquet `core/host` vert, `gofmt -l` vide, build OK, commit créé.

---

### Task 3: Parsing + dispatch — `parseSeal` et `maybeCoordinate`

**Files:**
- Modify: `herrscher/core/host/handoff.go`
- Modify: `herrscher/core/host/turnloop.go` (`maybeCoordinate`)
- Test: `herrscher/core/host/handoff_test.go`
- Test: `herrscher/core/host/turnloop_test.go` (utilise `recordingCoord.seals` de la Task 2)

**Interfaces:**
- Consumes: `parseTrailer(reply, marker string) (body string, ok bool)` (existant, `handoff.go`), la méthode `Seal` du port (Task 1) via `d.coordinator`, et `recordingCoord` avec son champ `seals` (Task 2).
- Produces: `parseSeal(reply string) (n int, ok bool)` + la branche de dispatch `parseSeal` dans `maybeCoordinate`.

- [ ] **Step 1: Écrire les tests qui échouent**

Dans `herrscher/core/host/handoff_test.go`, ajouter :

```go
func TestParseSeal(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		wantN int
		wantOK bool
	}{
		{"valide", "je scelle.\n⟢ seal: 5", 5, true},
		{"un", "⟢ seal: 1", 1, true},
		{"zéro refusé", "⟢ seal: 0", 0, false},
		{"négatif refusé", "⟢ seal: -2", 0, false},
		{"non entier refusé", "⟢ seal: trois", 0, false},
		{"corps vide refusé", "⟢ seal:", 0, false},
		{"marker absent", "⟢ done: fini", 0, false},
		{"pas en dernière ligne", "⟢ seal: 5\nautre chose", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := parseSeal(tc.reply)
			if ok != tc.wantOK || n != tc.wantN {
				t.Fatalf("parseSeal(%q) = (%d,%v), want (%d,%v)", tc.reply, n, ok, tc.wantN, tc.wantOK)
			}
		})
	}
}
```

Dans `herrscher/core/host/turnloop_test.go`, ajouter un test de dispatch :

```go
func TestDriverInvokesCoordinatorOnSealTrailer(t *testing.T) {
	from := make(chan contracts.Event, 2)
	d := newSessionDriver("lead", nil, make(chan contracts.Event, 1), from)
	rc := &recordingCoord{}
	d.coordinator = rc

	from <- contracts.Event{T: "reply", Done: true, Text: "cohorte lancée.\n⟢ seal: 4"}
	if ok := d.awaitTurn(context.Background()); !ok {
		t.Fatal("awaitTurn should complete on reply{done}")
	}
	if len(rc.seals) != 1 {
		t.Fatalf("expected 1 seal, got %d", len(rc.seals))
	}
	if rc.seals[0].FromSession != "lead" || rc.seals[0].Expected != 4 {
		t.Fatalf("bad seal request: %+v", rc.seals[0])
	}
}
```

- [ ] **Step 2: Lancer les tests — échec attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'ParseSeal|SealTrailer' -v`
Expected: FAIL — `undefined: parseSeal`.

- [ ] **Step 3: Implémenter `parseSeal` + le marker**

Dans `herrscher/core/host/handoff.go`, ajouter `strconv` à l'import (le fichier n'importe aujourd'hui que `strings`) :

```go
import (
	"strconv"
	"strings"
)
```

Ajouter le marker au bloc const (et mettre à jour le commentaire d'ordre de priorité) :

```go
	sealMarker     = "⟢ seal:"
```

Mettre à jour le commentaire du bloc const pour refléter le nouvel ordre : `done has priority over delegate over seal over merge over handoff when dispatched (see maybeCoordinate).`

Ajouter la fonction (après `parseMerge`) :

```go
// parseSeal extracts a cohort-seal intent: "⟢ seal: <N>". The body is a single
// positive integer (the expected worker count); a non-integer, a non-positive
// value, or an empty body is not a seal.
func parseSeal(reply string) (n int, ok bool) {
	body, ok := parseTrailer(reply, sealMarker)
	if !ok || body == "" {
		return 0, false
	}
	v, err := strconv.Atoi(body)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}
```

- [ ] **Step 4: Câbler le dispatch dans `maybeCoordinate`**

Dans `herrscher/core/host/turnloop.go`, insérer la branche `parseSeal` **entre** la branche `parseDelegate` et la branche `parseMerge` (respecte l'ordre `done → delegate → seal → merge → handoff`) :

```go
	if n, ok := parseSeal(reply); ok {
		if lead, err := d.coordinator.Seal(ctx, contracts.SealRequest{
			FromSession: d.name, Expected: n,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "seal refusé: " + err.Error()})
		} else {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "cohorte scellée pour " + lead})
		}
		return
	}
```

Mettre à jour le commentaire de doc de `maybeCoordinate` s'il énumère l'ordre des trailers, pour inclure `seal`.

- [ ] **Step 5: Lancer les tests — passent**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'ParseSeal|SealTrailer' -v`
Expected: PASS.

- [ ] **Step 6: Suite complète + `-race` + commit**

```bash
cd /home/shan/dev/herrscher
go test -race ./core/host/
gofmt -l core/host/ && go vet ./core/host/... && go build ./...
git add core/host/handoff.go core/host/handoff_test.go core/host/turnloop.go core/host/turnloop_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): parseSeal trailer + maybeCoordinate dispatch (P3 cohort seal)

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>"
```
Expected: `core/host` vert au `-race`, `gofmt -l` vide, build OK, commit créé.

---

## Self-Review (contrôle plan vs spec)

**Couverture spec :**
- Décision 1 (Seal = méthode de port) → Task 1. ✓
- Décision 2 (garde `N >= cohorte`) → Task 2, `Seal` garde 3 + `TestSealRefusesBelowCurrentCohort`. ✓
- Décision 3 (wording différencié) → Task 2, `Report` + `TestReportSealedCompleteWording`/`TestReportUnsealedStaysBestEffort`. ✓
- Décision 4 (pas de self-seed, status) → Task 3 dispatch `"cohorte scellée pour <lead>"`. ✓
- Décision 5 (re-seal last-wins) → Task 2, `expected[lead]=N` écrase ; couvert implicitement (map assignment). ✓
- `forget` purge `expected` → Task 2 + `TestForgetPurgesSeal`. ✓
- Dispatch `done→delegate→seal→merge→handoff` → Task 3. ✓
- Parsing (5+ cas, non-entier/0/négatif/vide/absent) → Task 3 `TestParseSeal`. ✓
- Concurrence (`expected` sous `mu`) → Task 2 (`Seal` et `Report` prennent `mu` pour lire/écrire `expected`) ; `-race` en Task 3. ✓

**Placeholder scan :** aucun TBD/TODO ; chaque step porte le code complet.

**Type consistency :** `SealRequest{FromSession string; Expected int}` et `Seal(ctx, SealRequest) (string, error)` identiques en Task 1 (déf), Task 2 (impl `c.Seal` + fakes), Task 3 (appel `contracts.SealRequest{FromSession, Expected}`). Champ `expected map[string]int` cohérent Task 2. `recordingCoord.seals []contracts.SealRequest` défini Task 2, utilisé Task 3. ✓
