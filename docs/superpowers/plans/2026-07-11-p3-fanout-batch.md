# P3 — Fan-out par lot — Plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Donner au lead la capacité de spawner toute une cohorte en un seul trailer `⟢ fanout: <agent> — <t1> ;; <t2> ;; …`, avec auto-scellage à la taille réelle.

**Architecture:** Modèle O (host-driven). Nouvelle méthode de port `FanOut` sur `Coordinator` ; le `coordinator` host réutilise `spawn` (création+seed+parent) en boucle puis pose `expected[lead]` sous `mu` (même mécanisme que `Seal`). Nouveau parser `parseFanOut` inséré dans `maybeCoordinate`. Zéro nouvel état.

**Tech Stack:** Go, deux modules résolus via `go.work` : `herrscher-contracts` (port) et `herrscher` (host). Tests std `testing`, `-race`.

## Global Constraints

- **Générique OSS** : zéro connaissance métier Roblox/Neublox dans le code ou les messages. Vocabulaire « worker »/« lead »/« cohorte » uniquement.
- **`herrscher-contracts` touché** : nouvelle méthode `FanOut` + type `FanOutRequest` ; les autres signatures du port `Coordinator` restent inchangées.
- **CI verte des deux côtés** : `gofmt -l` vide, `go vet ./...` propre, `go build ./...`, `go test -race ./...` verts, `go mod tidy` laisse `go.mod` inchangé (herrscher ET contracts).
- **DRY** : `FanOut` réutilise `spawn` et la map `expected`/le verrou `mu` de `Seal` — aucune duplication de la création de session ni du scellage.
- **Concurrence sûre** : aucun nouveau champ ; `expected` touché sous `mu` seulement (vérifiable `-race`).
- **Séparateurs** : agent/tâches sur l'em-dash `—` (comme `splitAgentTask`) ; tâches entre elles sur `;;`.
- **Commits** : identité `-c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com`. Branche de travail : `feat/p3-join-merge` (les tranches s'y accumulent).
- **Dispatch order** : `done → delegate → fanout → seal → merge → handoff` (fanout juste après delegate, symétrique).

---

### Task 1 : Port `FanOut` (herrscher-contracts)

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/coordinator.go`
- Test: `/home/shan/dev/herrscher-contracts/coordinator_test.go`

**Interfaces:**
- Produces: `FanOutRequest{FromSession string; ToAgent string; Tasks []string}` et la méthode `FanOut(ctx context.Context, req FanOutRequest) (spawned []string, err error)` sur `Coordinator`. Task 2 (host `coordinator`) et les fakes host l'implémentent.

Note : ajouter `FanOut` à l'interface `Coordinator` **casse la compilation de herrscher** (le `coordinator` host et les 3 fakes de test ne l'implémentent pas encore). C'est attendu : Task 2 restaure la compilation. Ne pas toucher herrscher dans cette tâche. `herrscher-contracts` se compile et teste seul (`cd /home/shan/dev/herrscher-contracts && go test ./...`).

- [ ] **Step 1 : Écrire le test de port (échoue à la compilation)**

Dans `coordinator_test.go`, ajouter après `TestCoordinatorPortIncludesSeal` (fin de fichier) :

```go
// fanoutStub carries the full Coordinator surface (incl. FanOut) to assert the
// port shape at compile time.
type fanoutStub struct{}

func (fanoutStub) Handoff(context.Context, HandoffRequest) (string, error)   { return "", nil }
func (fanoutStub) Delegate(context.Context, DelegateRequest) (string, error) { return "", nil }
func (fanoutStub) Report(context.Context, ReportRequest) (string, error)     { return "", nil }
func (fanoutStub) Merge(context.Context, MergeRequest) (string, error)       { return "", nil }
func (fanoutStub) Seal(context.Context, SealRequest) (string, error)         { return "", nil }
func (fanoutStub) FanOut(context.Context, FanOutRequest) ([]string, error)   { return nil, nil }

func TestCoordinatorPortIncludesFanOut(t *testing.T) {
	var _ Coordinator = fanoutStub{}
	req := FanOutRequest{FromSession: "lead", ToAgent: "scripter", Tasks: []string{"a", "b"}}
	if req.FromSession != "lead" || req.ToAgent != "scripter" || len(req.Tasks) != 2 {
		t.Fatalf("FanOutRequest fields not wired: %+v", req)
	}
}
```

Il faut aussi étendre les fakes existants qui implémentent `Coordinator` — `fakeCoordinator`, `mergeStub`, `sealStub` — avec une méthode `FanOut` (sinon elles ne satisfont plus l'interface). Ajouter :

- champ `gotFan FanOutRequest` à `fakeCoordinator` (après `gotSeal SealRequest`), et la méthode :

```go
func (f *fakeCoordinator) FanOut(_ context.Context, req FanOutRequest) ([]string, error) {
	f.gotFan = req
	spawned := make([]string, len(req.Tasks))
	for i := range req.Tasks {
		spawned[i] = req.ToAgent + "-worker"
	}
	return spawned, nil
}
```

- une méthode inerte sur `mergeStub` et sur `sealStub` :

```go
func (mergeStub) FanOut(context.Context, FanOutRequest) ([]string, error) { return nil, nil }
```
```go
func (sealStub) FanOut(context.Context, FanOutRequest) ([]string, error) { return nil, nil }
```

- [ ] **Step 2 : Lancer le test — échoue à la compilation**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: FAIL — `undefined: FanOutRequest` et `fanoutStub does not implement Coordinator (missing method FanOut)`.

- [ ] **Step 3 : Ajouter le type et la méthode au port**

Dans `coordinator.go`, ajouter le type après `SealRequest` (avant l'interface `Coordinator`) :

```go
// FanOutRequest is a lead spawning a whole cohort in one signal: one worker per
// task, all children of FromSession off its committed tip, all provisioned from
// the single agent ToAgent. It is the batch counterpart of DelegateRequest.
type FanOutRequest struct {
	FromSession string   // the lead spawning the cohort (each worker's base and parent)
	ToAgent     string   // durable agent profile shared by every worker
	Tasks       []string // one task per worker (≥ 1); each seeds its worker's opening turn
}
```

Et la méthode dans l'interface `Coordinator`, après `Seal` :

```go
	// FanOut spawns one worker per task (all children of FromSession off its
	// committed tip, all from ToAgent) and seals the cohort to its real size,
	// returning the spawned worker names. It is the batch counterpart of Delegate.
	// Same per-worker guards as Delegate (unknown agent, missing/dirty lead, failed
	// create); a spawn failure mid-batch is not rolled back — the workers already
	// created are returned alongside the error, and the cohort is sealed to what was
	// actually spawned.
	FanOut(ctx context.Context, req FanOutRequest) (spawned []string, err error)
```

- [ ] **Step 4 : Lancer les tests — passent**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... && gofmt -l . && go vet ./...`
Expected: PASS, `gofmt -l` sans sortie, `go vet` propre.

- [ ] **Step 5 : Commit**

```bash
cd /home/shan/dev/herrscher-contracts && git add coordinator.go coordinator_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(coordinator): FanOut port method + FanOutRequest (batch fan-out)"
```

---

### Task 2 : `coordinator.FanOut` + fakes host (herrscher)

**Files:**
- Modify: `/home/shan/dev/herrscher/core/host/coordinator.go`
- Modify: `/home/shan/dev/herrscher/core/host/coordinator_test.go`
- Modify: `/home/shan/dev/herrscher/core/host/hub_test.go` (fake `stubForgetCoord`)
- Modify: `/home/shan/dev/herrscher/core/host/turnloop_test.go` (fakes `recordingCoord`, `erroringCoord`)

**Interfaces:**
- Consumes: `contracts.FanOutRequest`, `contracts.FanOut` (Task 1) ; helpers existants `c.agents.Get`, `c.sessions.SnapshotSessions`, `findByName`, `c.spawn(ctx, from, toAgent, task, parent)`, champ `c.expected` + `c.mu`.
- Produces: méthode `(*coordinator) FanOut` ; `spawned` = noms créés dans l'ordre des tâches.

Cette tâche restaure la compilation de herrscher (l'interface `Coordinator` exige maintenant `FanOut`). Les 3 fakes host DOIVENT gagner une méthode `FanOut` sinon le paquet `host` ne compile pas.

- [ ] **Step 1 : Écrire les tests coordinator (échouent)**

Dans `coordinator_test.go`, ajouter en fin de fichier. D'abord un creator qui échoue au k-ième appel (pour l'échec partiel) :

```go
// nthFailCreator succeeds until (and excluding) the failAt-th Create call, then
// returns an error — so a fan-out can be observed spawning some workers then
// failing mid-batch. failAt is 1-based; failAt=2 fails the second Create.
type nthFailCreator struct {
	failAt int
	n      int
	last   contracts.CreateSession
}

func (c *nthFailCreator) Create(_ context.Context, s contracts.CreateSession) (string, error) {
	c.n++
	c.last = s
	if c.n == c.failAt {
		return "", fmt.Errorf("create boom at call %d", c.n)
	}
	return s.Name, nil
}
```

Ajouter l'import `"fmt"` en tête de `coordinator_test.go` (le bloc d'import n'a pas `fmt`).

Puis les tests :

```go
func TestFanOutSpawnsCohort(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true,
		[]state.Session{{Name: "lead", Project: "proj", Worktree: "/wt/lead"}}, &seeded)

	spawned, err := c.FanOut(context.Background(), contracts.FanOutRequest{
		FromSession: "lead", ToAgent: "scripter", Tasks: []string{"t1", "t2", "t3"},
	})
	if err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if len(spawned) != 3 {
		t.Fatalf("attendu 3 workers spawnés, got %v", spawned)
	}
	if len(seeded) != 3 {
		t.Fatalf("attendu 3 tâches seedées, got %v", seeded)
	}
	for i, task := range []string{"t1", "t2", "t3"} {
		if !strings.HasSuffix(seeded[i], "|"+task) {
			t.Fatalf("tâche %d mal seedée: %q", i, seeded[i])
		}
	}
}

func TestFanOutSealsToCohortSize(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)

	if _, err := c.FanOut(context.Background(), contracts.FanOutRequest{
		FromSession: "lead", ToAgent: "scripter", Tasks: []string{"a", "b", "c"},
	}); err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if c.expected["lead"] != 3 {
		t.Fatalf("cohorte devrait être scellée à 3, got %d", c.expected["lead"])
	}
}

func TestFanOutUnknownAgent(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, nil, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)
	if _, err := c.FanOut(context.Background(), contracts.FanOutRequest{
		FromSession: "lead", ToAgent: "ghost", Tasks: []string{"a"}}); err == nil {
		t.Fatal("agent inconnu devrait échouer")
	}
	if cr.spec.Name != "" {
		t.Fatal("aucune session ne devrait être créée")
	}
	if _, ok := c.expected["lead"]; ok {
		t.Fatal("aucun scellage sur agent inconnu")
	}
}

func TestFanOutLeadNotFound(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true, nil, &seeded)
	if _, err := c.FanOut(context.Background(), contracts.FanOutRequest{
		FromSession: "ghost", ToAgent: "scripter", Tasks: []string{"a"}}); err == nil {
		t.Fatal("lead inconnu devrait échouer")
	}
}

func TestFanOutNoTasks(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)
	if _, err := c.FanOut(context.Background(), contracts.FanOutRequest{
		FromSession: "lead", ToAgent: "scripter", Tasks: nil}); err == nil {
		t.Fatal("fanout sans tâche devrait échouer")
	}
}

func TestFanOutDirtyLeadSpawnsNone(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, false, // lead sale
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)
	spawned, err := c.FanOut(context.Background(), contracts.FanOutRequest{
		FromSession: "lead", ToAgent: "scripter", Tasks: []string{"a", "b"}})
	if err == nil {
		t.Fatal("lead sale devrait faire échouer le premier spawn")
	}
	if len(spawned) != 0 {
		t.Fatalf("aucun worker sur lead sale, got %v", spawned)
	}
	if _, ok := c.expected["lead"]; ok {
		t.Fatal("aucun scellage quand rien n'est spawné")
	}
}

func TestFanOutPartialSealsToSpawned(t *testing.T) {
	cr := &nthFailCreator{failAt: 2} // le 2e Create échoue
	var seeded []string
	km := map[string]bool{"scripter": true}
	seed := func(sess, task string) bool { seeded = append(seeded, sess+"|"+task); return true }
	c := newCoordinator(cr, fakeAgents{known: km}, &fakeWTC{clean: true},
		fakeSessions{list: []state.Session{{Name: "lead", Worktree: "/wt/lead"}}}, &fakeCloser{}, seed)

	spawned, err := c.FanOut(context.Background(), contracts.FanOutRequest{
		FromSession: "lead", ToAgent: "scripter", Tasks: []string{"a", "b", "c"}})
	if err == nil {
		t.Fatal("un échec de Create mid-lot doit remonter une erreur")
	}
	if len(spawned) != 1 {
		t.Fatalf("un seul worker devrait survivre, got %v", spawned)
	}
	if c.expected["lead"] != 1 {
		t.Fatalf("scellage devrait suivre le réel (1), got %d", c.expected["lead"])
	}
}

func TestFanOutIncludesPreexistingCohort(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w0", Worktree: "/wt/w0", Parent: "lead"}, // 1 worker préexistant
		}, &seeded)

	if _, err := c.FanOut(context.Background(), contracts.FanOutRequest{
		FromSession: "lead", ToAgent: "scripter", Tasks: []string{"a", "b"}}); err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if c.expected["lead"] != 3 { // 1 préexistant + 2 spawnés
		t.Fatalf("cohorte devrait être scellée à 3 (1 préexistant + 2), got %d", c.expected["lead"])
	}
}
```

- [ ] **Step 2 : Lancer les tests — échouent (compilation)**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ 2>&1 | head -20`
Expected: FAIL compilation — `c.FanOut undefined` et `*coordinator does not implement contracts.Coordinator` / fakes ne satisfont plus l'interface.

- [ ] **Step 3 : Implémenter `FanOut` sur le coordinator**

Dans `coordinator.go`, ajouter après la méthode `Seal` (avant `forget`) :

```go
// FanOut spawns one worker per task — all children of FromSession off its
// committed tip, all from ToAgent — then seals the cohort to its real size, the
// batch counterpart of Delegate. Guards run before any spawn: unknown agent and
// unknown lead fail with nothing created. Each task then goes through spawn, whose
// own lead-clean guard means a dirty lead yields zero workers on the first task. A
// spawn failure mid-batch is not rolled back: the workers already created are real
// committed sessions, so FanOut stops, seals to what was actually spawned, and
// returns those names alongside the error. The seal counts the lead's preexisting
// children (from the guard snapshot) plus the workers spawned here.
func (c *coordinator) FanOut(ctx context.Context, req contracts.FanOutRequest) ([]string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return nil, fmt.Errorf("fanout: unknown agent %q", req.ToAgent)
	}
	sessions := c.sessions.SnapshotSessions()
	lead, ok := findByName(sessions, req.FromSession)
	if !ok {
		return nil, fmt.Errorf("fanout: lead %q not found", req.FromSession)
	}
	if len(req.Tasks) == 0 {
		return nil, fmt.Errorf("fanout: no tasks")
	}
	preexisting := 0
	for _, s := range sessions {
		if s.Parent == req.FromSession {
			preexisting++
		}
	}
	var spawned []string
	var spawnErr error
	for _, task := range req.Tasks {
		name, err := c.spawn(ctx, lead, req.ToAgent, task, req.FromSession)
		if err != nil {
			spawnErr = err
			break
		}
		spawned = append(spawned, name)
	}
	if len(spawned) > 0 {
		c.mu.Lock()
		c.expected[req.FromSession] = preexisting + len(spawned)
		c.mu.Unlock()
	}
	return spawned, spawnErr
}
```

- [ ] **Step 4 : Faire compiler les fakes host (ajouter `FanOut`)**

Dans `hub_test.go`, après `stubForgetCoord.Seal` (avant `forget`) :

```go
func (s *stubForgetCoord) FanOut(context.Context, contracts.FanOutRequest) ([]string, error) {
	return nil, nil
}
```

Dans `turnloop_test.go`, ajouter le champ `fanouts []contracts.FanOutRequest` à `recordingCoord` (après `seals`) et la méthode après `recordingCoord.Seal` :

```go
func (r *recordingCoord) FanOut(_ context.Context, req contracts.FanOutRequest) ([]string, error) {
	r.fanouts = append(r.fanouts, req)
	spawned := make([]string, len(req.Tasks))
	for i := range req.Tasks {
		spawned[i] = req.ToAgent + "-w"
	}
	return spawned, nil
}
```

Et sur `erroringCoord`, après son `Seal` :

```go
func (e *erroringCoord) FanOut(context.Context, contracts.FanOutRequest) ([]string, error) {
	return nil, e.err
}
```

- [ ] **Step 5 : Lancer les tests — passent**

Run: `cd /home/shan/dev/herrscher && go test -race ./core/host/ && gofmt -l core/host/ && go vet ./core/host/`
Expected: PASS (tous les tests host, dont les nouveaux FanOut), `gofmt -l` sans sortie, `go vet` propre.

- [ ] **Step 6 : Commit**

```bash
cd /home/shan/dev/herrscher && git add core/host/coordinator.go core/host/coordinator_test.go core/host/hub_test.go core/host/turnloop_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): coordinator.FanOut spawns cohort + auto-seals to real size"
```

---

### Task 3 : `parseFanOut` + dispatch (herrscher)

**Files:**
- Modify: `/home/shan/dev/herrscher/core/host/handoff.go`
- Modify: `/home/shan/dev/herrscher/core/host/turnloop.go`
- Test: `/home/shan/dev/herrscher/core/host/handoff_test.go`
- Test: `/home/shan/dev/herrscher/core/host/turnloop_test.go`

**Interfaces:**
- Consumes: `parseTrailer` (existant), `c.coordinator.FanOut` (Task 2), fakes `recordingCoord.fanouts` (Task 2).
- Produces: `parseFanOut(reply string) (agent string, tasks []string, ok bool)`, const `fanoutMarker`, helper `splitAgentTasks`.

- [ ] **Step 1 : Écrire les tests de parsing (échouent)**

Dans `handoff_test.go`, ajouter :

```go
func TestParseFanOut(t *testing.T) {
	cases := []struct {
		reply     string
		wantAgent string
		wantTasks []string
		wantOK    bool
	}{
		{"txt\n⟢ fanout: alpha — t1 ;; t2 ;; t3", "alpha", []string{"t1", "t2", "t3"}, true},
		{"txt\n⟢ fanout: alpha — seule tâche", "alpha", []string{"seule tâche"}, true},
		{"txt\n⟢ fanout: alpha —  t1  ;;  t2 ", "alpha", []string{"t1", "t2"}, true},
		{"txt\n⟢ fanout: alpha — t1 ;; ;; t2", "alpha", []string{"t1", "t2"}, true},
		{"txt\n⟢ fanout:  — t1", "", nil, false},   // agent vide
		{"txt\n⟢ fanout: alpha —", "", nil, false},  // aucune tâche
		{"txt\n⟢ fanout: alpha — ;; ;;", "", nil, false}, // tâches toutes vides
		{"txt\n⟢ fanout: sans separateur", "", nil, false}, // pas d'em-dash
		{"aucun trailer", "", nil, false},
	}
	for _, tc := range cases {
		agent, tasks, ok := parseFanOut(tc.reply)
		if ok != tc.wantOK {
			t.Fatalf("reply %q → ok=%v (voulu %v)", tc.reply, ok, tc.wantOK)
		}
		if !ok {
			continue
		}
		if agent != tc.wantAgent {
			t.Fatalf("reply %q → agent=%q (voulu %q)", tc.reply, agent, tc.wantAgent)
		}
		if len(tasks) != len(tc.wantTasks) {
			t.Fatalf("reply %q → %d tâches (voulu %d): %v", tc.reply, len(tasks), len(tc.wantTasks), tasks)
		}
		for i := range tasks {
			if tasks[i] != tc.wantTasks[i] {
				t.Fatalf("reply %q → tâche %d = %q (voulu %q)", tc.reply, i, tasks[i], tc.wantTasks[i])
			}
		}
	}
}
```

- [ ] **Step 2 : Lancer — échoue (compilation)**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestParseFanOut 2>&1 | head`
Expected: FAIL — `undefined: parseFanOut`.

- [ ] **Step 3 : Implémenter le parser**

Dans `handoff.go`, ajouter le marker dans le bloc const (après `mergeMarker`) :

```go
	fanoutMarker   = "⟢ fanout:"
```

Et, après `parseSeal`, le helper + le parser :

```go
// splitAgentTasks splits "<agent> — <task1> ;; <task2> ;; …" into the agent and
// its task list: the em-dash separates agent from tasks, ";;" separates tasks.
// Empty tasks (extra ";;") are dropped. ok=false when the agent is empty or no
// non-empty task remains.
func splitAgentTasks(body string) (agent string, tasks []string, ok bool) {
	parts := strings.SplitN(body, "—", 2)
	if len(parts) != 2 {
		return "", nil, false
	}
	agent = strings.TrimSpace(parts[0])
	if agent == "" {
		return "", nil, false
	}
	for _, raw := range strings.Split(parts[1], ";;") {
		if t := strings.TrimSpace(raw); t != "" {
			tasks = append(tasks, t)
		}
	}
	if len(tasks) == 0 {
		return "", nil, false
	}
	return agent, tasks, true
}

// parseFanOut extracts a batch fan-out intent:
// "⟢ fanout: <agent> — <task1> ;; <task2> ;; …". One agent, one or more tasks.
// Returns ok=false when absent or malformed (empty agent, no non-empty task,
// missing em-dash).
func parseFanOut(reply string) (agent string, tasks []string, ok bool) {
	body, ok := parseTrailer(reply, fanoutMarker)
	if !ok {
		return "", nil, false
	}
	return splitAgentTasks(body)
}
```

- [ ] **Step 4 : Lancer — le parsing passe**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestParseFanOut -v 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5 : Écrire le test de dispatch (échoue)**

Dans `turnloop_test.go`, ajouter :

```go
// TestDriverInvokesCoordinatorOnFanOutTrailer proves a completed turn whose reply
// carries a well-formed fanout trailer invokes FanOut with the driver's own name,
// the parsed agent, and the parsed task list, and fans a "cohorte lancée" status.
func TestDriverInvokesCoordinatorOnFanOutTrailer(t *testing.T) {
	from := make(chan contracts.Event, 2)
	d := newSessionDriver("lead", nil, make(chan contracts.Event, 1), from)
	rc := &recordingCoord{}
	d.coordinator = rc

	from <- contracts.Event{T: "reply", Done: true,
		Text: "je lance.\n⟢ fanout: scripter — a ;; b"}
	if ok := d.awaitTurn(context.Background()); !ok {
		t.Fatal("awaitTurn should complete on reply{done}")
	}
	if len(rc.fanouts) != 1 {
		t.Fatalf("expected 1 fanout, got %d", len(rc.fanouts))
	}
	got := rc.fanouts[0]
	if got.FromSession != "lead" || got.ToAgent != "scripter" || len(got.Tasks) != 2 ||
		got.Tasks[0] != "a" || got.Tasks[1] != "b" {
		t.Fatalf("bad fanout request: %+v", got)
	}
}
```

- [ ] **Step 6 : Lancer — échoue (le dispatch ne route pas encore fanout)**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestDriverInvokesCoordinatorOnFanOutTrailer 2>&1 | head`
Expected: FAIL — `expected 1 fanout, got 0`.

- [ ] **Step 7 : Câbler le dispatch**

Dans `turnloop.go`, dans `maybeCoordinate`, insérer le bloc **après** la branche `parseDelegate` et **avant** la branche `parseSeal` :

```go
	if toAgent, tasks, ok := parseFanOut(reply); ok {
		if spawned, err := d.coordinator.FanOut(ctx, contracts.FanOutRequest{
			FromSession: d.name, ToAgent: toAgent, Tasks: tasks,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status",
				Text: "fanout partiel: " + strconv.Itoa(len(spawned)) + " lancés puis " + err.Error()})
		} else {
			d.fanOut(ctx, contracts.Event{T: "status",
				Text: "cohorte lancée : " + strconv.Itoa(len(spawned)) + " workers (" + strings.Join(spawned, ", ") + ")"})
		}
		return
	}
```

Ajouter l'import `"strings"` au bloc d'import de `turnloop.go` (il a déjà `strconv`, pas `strings`).

Mettre à jour le commentaire de priorité de `maybeCoordinate` : `done wins over delegate over fanout over seal over merge over handoff.`

- [ ] **Step 8 : Lancer toute la suite host — passe**

Run: `cd /home/shan/dev/herrscher && go test -race ./core/host/ && gofmt -l core/host/ && go vet ./core/host/`
Expected: PASS, `gofmt -l` sans sortie, `go vet` propre.

- [ ] **Step 9 : Vérifier build complet + tidy des deux côtés**

Run: `cd /home/shan/dev/herrscher && go build ./... && go mod tidy && git diff --exit-code go.mod go.sum`
Expected: build OK, `go mod tidy` ne change rien (exit 0).

- [ ] **Step 10 : Commit**

```bash
cd /home/shan/dev/herrscher && git add core/host/handoff.go core/host/turnloop.go core/host/handoff_test.go core/host/turnloop_test.go && \
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): ⟢ fanout trailer parses + dispatches to coordinator.FanOut"
```

---

## Self-Review

- **Spec coverage** : port `FanOut`+`FanOutRequest` (T1) ✓ ; `coordinator.FanOut` gardes + boucle spawn + auto-seal préexistant+spawnés (T2) ✓ ; échec partiel non destructif (T2, `nthFailCreator`) ✓ ; `parseFanOut`/`splitAgentTasks`/marker + dispatch order done→delegate→fanout→seal→merge→handoff (T3) ✓ ; status succès/partiel (T3) ✓.
- **Placeholders** : aucun — tout le code est écrit.
- **Type consistency** : `FanOut(ctx, FanOutRequest) ([]string, error)` identique port (T1) / impl (T2) / fakes (T1,T2) / dispatch (T3). `splitAgentTasks`/`parseFanOut` signatures cohérentes entre def (T3 step 3) et test (T3 step 1).
- **Constraints** : générique OSS (worker/lead/cohorte) ✓ ; DRY (réutilise spawn + expected/mu) ✓ ; concurrence (expected sous mu) ✓.
</content>
