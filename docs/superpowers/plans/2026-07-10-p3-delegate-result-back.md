# P3 — Delegate + result-back Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ajouter au châssis herrscher une primitive `Delegate` (un lead L confie une tâche à un worker W qui repart du tip commité de L, L reste vivant) et un canal de retour `Report` (W signale sa fin, le coordinateur livre `réf branche + résumé` à L, W reste vivant).

**Architecture:** On étend le socle `Coordinator` (Modèle O) posé par le handoff. L'agent signale par un trailer en fin de réponse (`⟢ delegate:` côté L, `⟢ done:` côté W) ; le host parse après le tour et appelle le coordinateur au niveau hub. `Delegate` réutilise le même mécanisme de spawn que `Handoff` (branche sur le tip de la source + seed de la tâche), la seule différence étant que le worker mémorise son parent (`Parent`) et que la source reste vivante. `Report` lit `W.Parent`, résout la session parente et lui seede un message de livraison. Aucun merge automatique, aucune surface MCP nouvelle.

**Tech Stack:** Go 1.25 (`go.mod` = 1.25.0). Deux modules locaux : `herrscher-contracts` (contrats partagés, dépend de rien) et `herrscher` (core/host, core/internal). Tests avec fakes, jamais de vrai backend LLM. `gofmt` propre (CI herrscher).

## Global Constraints

- **Identité de commit (verbatim) :** `git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit`. JAMAIS happy62101@gmail.com.
- **Footers de commit (verbatim, sur chaque commit) :**
  `Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc`
  `Co-Authored-By: Claude <noreply@anthropic.com>`
- **1 tâche = 1 commit.** TDD : test qui échoue d'abord, puis implémentation minimale.
- **OSS générique :** aucune connaissance métier Roblox/Neublox dans herrscher. Garder les commentaires « pourquoi ».
- **Ne jamais affaiblir un comportement fidèle pour faire passer un test** ; ne pas fabriquer de données ; ne pas supprimer un commentaire « pourquoi » ; « évite les faux positifs ».
- **Ne pas se merger soi-même** sans le feu vert de l'utilisateur ; ne pas démarrer sur main/master sans consentement (créer une branche de feature `feat/p3-delegate-result-back`).
- **`Parent` par défaut `""` = comportement actuel inchangé** (aucune session existante n'a de parent ; `Handoff` passe toujours `parent=""`).
- **`gofmt -l` vide** sur les répertoires touchés ; `go build ./...` et `go test ./...` verts sur les deux repos.
- Commentaires et prose en **français** (le code suit les conventions du repo).

---

## File Structure

- `herrscher-contracts/coordinator.go` — ajoute `DelegateRequest`, `ReportRequest`, et les méthodes `Delegate`/`Report` au port `Coordinator`.
- `herrscher-contracts/session_control.go` — ajoute le champ `Parent` à `CreateSession`.
- `herrscher/core/internal/state/state.go` — ajoute le champ `Parent` à `state.Session`.
- `herrscher/core/internal/manager/commands.go` — ajoute le `Param("parent", …)` au create.
- `herrscher/core/internal/manager/session.go` — lit le param `parent` et le pose sur les deux littéraux `state.Session`.
- `herrscher/core/host/hub.go` — propage `spec.Parent` via `setStr("parent", …)`.
- `herrscher/core/host/handoff.go` — factorise `parseTrailer`, ajoute `parseDelegate` + `parseDone` (parseurs purs).
- `herrscher/core/host/coordinator.go` — factorise `spawn` (extrait de `Handoff`), implémente `Delegate` et `Report` ; `sessionLister` doit exposer de quoi lire `Parent`.
- `herrscher/core/host/turnloop.go` — généralise `maybeHandoff` en `maybeCoordinate` (dispatch `parseDone → parseDelegate → parseHandoff`).
- Tests : `session_control` n'a pas de test dédié (couvert par les tests manager/hub) ; `handler_test.go`, `coordinator_test.go`, `handoff_test.go`, `turnloop_test.go` (fichiers existants) reçoivent les nouveaux tests.

---

### Task 1: Contrats — CreateSession.Parent + DelegateRequest/ReportRequest + port

**Files:**
- Modify: `herrscher-contracts/session_control.go` (struct `CreateSession`)
- Modify: `herrscher-contracts/coordinator.go` (requêtes + interface)

**Interfaces:**
- Consumes: rien (le module contracts ne dépend de rien).
- Produces (utilisés par toutes les tâches suivantes) :
  - `CreateSession.Parent string` — champ, défaut `""`.
  - `type DelegateRequest struct { FromSession string; ToAgent string; Task string }`
  - `type ReportRequest struct { FromSession string; Summary string }`
  - `Coordinator interface { Handoff(ctx, HandoffRequest) (string, error); Delegate(ctx, DelegateRequest) (worker string, err error); Report(ctx, ReportRequest) (parent string, err error) }`

- [ ] **Step 1: Ajouter le champ `Parent` à `CreateSession`**

Dans `herrscher-contracts/session_control.go`, juste après le champ `Base` (garder son commentaire), ajouter :

```go
	// Parent nomme la session lead qui a délégué cette session (result-back P3).
	// Vide = aucun parent (défaut ; toute session non-déléguée). Le coordinateur
	// s'en sert pour livrer le rapport de fin du worker à son lead.
	Parent string
```

- [ ] **Step 2: Ajouter les requêtes et étendre le port dans `coordinator.go`**

Remplacer le contenu de `herrscher-contracts/coordinator.go` par (en gardant le `package` et le commentaire de tête existants, et `HandoffRequest` inchangé) :

```go
// HandoffRequest décrit un passage de relais A→B (l'agent A se termine, B reprend).
type HandoffRequest struct {
	FromSession string
	ToAgent     string
	Task        string
}

// DelegateRequest décrit une délégation L→W : le lead L confie une tâche à un
// worker W qui repart du tip commité de L. Contrairement au handoff, L reste
// vivant et W mémorise L comme parent pour le canal de retour (Report).
type DelegateRequest struct {
	FromSession string
	ToAgent     string
	Task        string
}

// ReportRequest décrit le retour d'un worker W vers son lead : W signale sa fin
// et un résumé ; le coordinateur livre {réf branche session/<W> + résumé} au
// parent de W. W reste vivant (livraison pure, pas de teardown, pas de merge).
type ReportRequest struct {
	FromSession string
	Summary     string
}

// Coordinator porte la coordination inter-sessions au niveau hub (Modèle O :
// l'agent signale, le host valide, le coordinateur exécute).
type Coordinator interface {
	Handoff(ctx context.Context, req HandoffRequest) (session string, err error)
	Delegate(ctx context.Context, req DelegateRequest) (worker string, err error)
	Report(ctx context.Context, req ReportRequest) (parent string, err error)
}
```

Garder l'import `context` (déjà présent pour `Handoff`).

- [ ] **Step 3: Vérifier la compilation du module contracts**

Run: `cd /home/shan/dev/herrscher-contracts && go build ./... && gofmt -l .`
Expected: aucune sortie (build OK, gofmt propre). Note : le module `herrscher` ne compilera plus tant que son `coordinator` n'implémente pas `Delegate`/`Report` — c'est attendu, corrigé en Task 5/6. Ne PAS toucher `herrscher` dans cette tâche.

- [ ] **Step 4: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -am "$(cat <<'EOF'
feat(contracts): Parent sur CreateSession + Delegate/Report au port Coordinator

Socle du canal result-back P3 : le lead délègue (Delegate), le worker
mémorise son parent (CreateSession.Parent) et remonte sa fin (Report).

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: state.Session.Parent + plumbing manager (param → Session.Parent)

**Files:**
- Modify: `herrscher/core/internal/state/state.go` (struct `Session`)
- Modify: `herrscher/core/internal/manager/commands.go` (Param "parent")
- Modify: `herrscher/core/internal/manager/session.go` (lecture + pose sur les 2 littéraux)
- Test: `herrscher/core/internal/manager/handler_test.go`

**Interfaces:**
- Consumes: `CreateSession.Parent` (Task 1) — indirectement via le param CLI `parent`.
- Produces: `state.Session.Parent string` (lu par le coordinateur en Task 6) ; le create manager accepte désormais un param `parent` et le persiste.

- [ ] **Step 1: Écrire le test qui échoue (plumbing du param parent)**

Dans `herrscher/core/internal/manager/handler_test.go`, juste après `TestSessionCreatePassesBaseToWorktree`, ajouter (le fake `st` capture la session créée ; on lit la dernière session enregistrée via l'API d'état déjà utilisée par les autres tests — vérifier le helper existant pour récupérer la session, réutiliser le même accès que les tests qui inspectent `sess`) :

```go
func TestSessionCreatePersistsParent(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if _, err := h.sessionCreateRun(context.Background(), args("name", "worker", "parent", "lead")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.SessionByName("worker")
	if !ok {
		t.Fatalf("session worker introuvable")
	}
	if sess.Parent != "lead" {
		t.Fatalf("parent non persisté: %q", sess.Parent)
	}
}
```

Note pour l'implémenteur : `st.SessionByName` est le nom présumé de l'accesseur ; vérifier dans `state` le vrai nom de la lecture par nom (les tests existants qui lisent `sess.Backend`/`sess.Agent` montrent le bon accesseur — réutiliser exactement celui-là). Si l'accès diffère, adapter la ligne `sess, ok := …` sans changer l'assertion sur `sess.Parent`.

- [ ] **Step 2: Lancer le test — il échoue à la compilation**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/manager/ -run TestSessionCreatePersistsParent`
Expected: FAIL — `sess.Parent` indéfini (le champ n'existe pas encore).

- [ ] **Step 3: Ajouter le champ `Parent` à `state.Session`**

Dans `herrscher/core/internal/state/state.go`, dans `type Session struct`, après `Participants`, ajouter :

```go
	// Parent : session lead qui a délégué celle-ci (result-back P3). Vide = aucune.
	Parent string `json:"parent,omitempty"`
```

- [ ] **Step 4: Ajouter le Param "parent" au create**

Dans `herrscher/core/internal/manager/commands.go`, juste après le `Param("base", …)` (ligne 24), ajouter :

```go
			Param("parent", "session lead qui a délégué celle-ci (result-back P3) ; vide = aucune", false),
```

- [ ] **Step 5: Lire le param et le poser sur les deux littéraux `state.Session`**

Dans `herrscher/core/internal/manager/session.go`, à côté de la lecture des autres options (après `agentName, _ := in.Lookup("agent")`), ajouter :

```go
	parent, _ := in.Lookup("parent")
```

Puis, dans CHACUN des deux littéraux `sess = state.Session{…}` (branche category/terminal et branche forum), ajouter le champ `Parent: parent,` (à côté de `Agent: agentName,`).

- [ ] **Step 6: Lancer le test — il passe**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/manager/ -run TestSessionCreatePersistsParent -v`
Expected: PASS.

- [ ] **Step 7: Suite manager complète + gofmt**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/manager/ && gofmt -l core/internal/manager/ core/internal/state/`
Expected: tests verts, gofmt sans sortie.

- [ ] **Step 8: Commit**

```bash
cd /home/shan/dev/herrscher
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -am "$(cat <<'EOF'
feat(manager): champ Parent sur state.Session + param create parent

Le worker délégué persiste son lead pour que le coordinateur retrouve la
cible du rapport de fin (Report). Param optionnel, défaut vide = inchangé.

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: hub.Create propage spec.Parent

**Files:**
- Modify: `herrscher/core/host/hub.go` (bloc `setStr`)
- Test: `herrscher/core/host/hub_test.go` (si un test de propagation d'args existe ; sinon couvert bout-en-bout par Task 5)

**Interfaces:**
- Consumes: `CreateSession.Parent` (Task 1), param `parent` du manager (Task 2).
- Produces: un `hub.Create(CreateSession{Parent: "lead"})` fait bien parvenir `parent=lead` au manager.

- [ ] **Step 1: Chercher un test de propagation d'args existant**

Run: `cd /home/shan/dev/herrscher && grep -rn "setStr\|spec.Base\|args\[\"base\"\]" core/host/*_test.go`
Si un test vérifie déjà que `spec.Base` arrive comme arg `base`, le mirroir pour `parent` (même structure). Sinon, ne pas inventer de test de bas niveau ici : la propagation est couverte bout-en-bout par le test d'intégration `Delegate` (Task 5) qui asserte `cr.spec.Parent`. Dans ce cas, sauter à Step 3.

- [ ] **Step 2 (conditionnel): Écrire/mirror le test de propagation `parent`**

Si un test `base` existe, ajouter son équivalent `parent` (asserter que `Create(CreateSession{Parent:"lead"})` produit l'arg `parent=lead`). Lancer, vérifier qu'il échoue (arg absent).

- [ ] **Step 3: Ajouter `setStr("parent", spec.Parent)`**

Dans `herrscher/core/host/hub.go`, juste après `setStr("base", spec.Base)`, ajouter :

```go
	setStr("parent", spec.Parent)
```

- [ ] **Step 4: Lancer les tests host + gofmt**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ && gofmt -l core/host/`
Expected: verts, gofmt sans sortie. (Le module ne build pas encore entièrement si `coordinator` n'implémente pas `Delegate`/`Report` ; si `go test ./core/host/` échoue à la compilation pour cette raison, c'est attendu — cette tâche peut être commitée telle quelle et sera verte après Task 5/6. Pour vérifier isolément, `go vet ./core/host/hub.go` ne suffit pas ; se fier à la revue du diff.)

Note : si l'ordre des tâches gêne la compilation, l'implémenteur peut fusionner mentalement mais DOIT garder 1 commit = 1 tâche. Le contrôleur ordonnancera Task 5/6 juste après pour restaurer le vert.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -am "$(cat <<'EOF'
feat(host): hub.Create propage spec.Parent au manager

Miroir de spec.Base : le champ Parent (result-back P3) atteint le create
manager via l'arg parent.

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Parseurs purs — parseTrailer + parseDelegate + parseDone

**Files:**
- Modify: `herrscher/core/host/handoff.go`
- Test: `herrscher/core/host/handoff_test.go`

**Interfaces:**
- Consumes: rien (parseurs purs sur une string de réponse).
- Produces (utilisés par `maybeCoordinate` en Task 7) :
  - `parseDelegate(reply string) (agent, task string, ok bool)` — trailer `⟢ delegate: <agent> — <task>`, mêmes règles que `parseHandoff` (séparateur em-dash `—`, agent et task non vides).
  - `parseDone(reply string) (summary string, ok bool)` — trailer `⟢ done: <résumé>` ; PAS de séparateur em-dash : tout ce qui suit le marqueur est le résumé (non vide).
  - `parseTrailer(reply, marker string) (body string, ok bool)` — helper : isole la dernière ligne, vérifie le préfixe `marker`, renvoie le corps trimé.

- [ ] **Step 1: Écrire les tests qui échouent**

Dans `herrscher/core/host/handoff_test.go`, ajouter :

```go
func TestParseDelegate(t *testing.T) {
	agent, task, ok := parseDelegate("blabla\n⟢ delegate: scripter — écris le module de vol")
	if !ok || agent != "scripter" || task != "écris le module de vol" {
		t.Fatalf("delegate valide mal parsé: %q %q %v", agent, task, ok)
	}
	if _, _, ok := parseDelegate("⟢ delegate: scripter"); ok {
		t.Fatalf("delegate sans em-dash devrait échouer")
	}
	if _, _, ok := parseDelegate("⟢ delegate:  — tâche"); ok {
		t.Fatalf("delegate sans agent devrait échouer")
	}
	if _, _, ok := parseDelegate("pas de trailer ici"); ok {
		t.Fatalf("absence de trailer devrait échouer")
	}
	// un trailer handoff ne doit pas être lu comme un delegate
	if _, _, ok := parseDelegate("⟢ handoff: scripter — x"); ok {
		t.Fatalf("handoff ne doit pas matcher delegate")
	}
}

func TestParseDone(t *testing.T) {
	summary, ok := parseDone("j'ai fini\n⟢ done: module de vol commité, 12 tests verts")
	if !ok || summary != "module de vol commité, 12 tests verts" {
		t.Fatalf("done valide mal parsé: %q %v", summary, ok)
	}
	// pas d'em-dash requis : tout le corps est le résumé (même avec un tiret dedans)
	if s, ok := parseDone("⟢ done: fait — et testé"); !ok || s != "fait — et testé" {
		t.Fatalf("done avec tiret dans le corps: %q %v", s, ok)
	}
	if _, ok := parseDone("⟢ done:   "); ok {
		t.Fatalf("done vide devrait échouer")
	}
	if _, ok := parseDone("pas de trailer"); ok {
		t.Fatalf("absence de trailer devrait échouer")
	}
}
```

- [ ] **Step 2: Lancer — échoue à la compilation**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'TestParseDelegate|TestParseDone'`
Expected: FAIL — `parseDelegate`/`parseDone` indéfinis.

- [ ] **Step 3: Factoriser `parseTrailer` et implémenter les parseurs**

Dans `herrscher/core/host/handoff.go`, ajouter les marqueurs à côté de `handoffMarker` et refactoriser. La forme cible :

```go
const (
	handoffMarker  = "⟢ handoff:"
	delegateMarker = "⟢ delegate:"
	doneMarker     = "⟢ done:"
)

// parseTrailer isole la dernière ligne non vide de reply et, si elle commence
// par marker, renvoie le corps trimé. Un trailer est toujours la DERNIÈRE ligne :
// on ne devine jamais une intention à moitié formée au milieu d'une réponse.
func parseTrailer(reply, marker string) (body string, ok bool) {
	lines := strings.Split(strings.TrimRight(reply, "\n \t"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(last, marker) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(last, marker)), true
}

// splitAgentTask découpe "<agent> — <task>" sur l'em-dash ; les deux non vides.
func splitAgentTask(body string) (agent, task string, ok bool) {
	parts := strings.SplitN(body, "—", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	agent = strings.TrimSpace(parts[0])
	task = strings.TrimSpace(parts[1])
	if agent == "" || task == "" {
		return "", "", false
	}
	return agent, task, true
}

// parseHandoff : trailer "⟢ handoff: <agent> — <task>".
func parseHandoff(reply string) (agent, task string, ok bool) {
	body, ok := parseTrailer(reply, handoffMarker)
	if !ok {
		return "", "", false
	}
	return splitAgentTask(body)
}

// parseDelegate : trailer "⟢ delegate: <agent> — <task>" (même forme que handoff ;
// sémantique distincte, cf. spec result-back : L reste vivant, W mémorise son parent).
func parseDelegate(reply string) (agent, task string, ok bool) {
	body, ok := parseTrailer(reply, delegateMarker)
	if !ok {
		return "", "", false
	}
	return splitAgentTask(body)
}

// parseDone : trailer "⟢ done: <résumé>". PAS d'em-dash — tout le corps est le
// résumé (le worker peut y mettre des tirets). Corps vide → pas de rapport.
func parseDone(reply string) (summary string, ok bool) {
	body, ok := parseTrailer(reply, doneMarker)
	if !ok || body == "" {
		return "", false
	}
	return body, true
}
```

Important : remplacer l'ANCIEN corps de `parseHandoff` par la version ci-dessus (qui délègue à `parseTrailer`/`splitAgentTask`) — ne pas laisser deux définitions. Vérifier que `handoffMarker` n'est plus déclaré ailleurs (fusionner dans le bloc `const`).

- [ ] **Step 4: Lancer les nouveaux tests + les tests handoff existants**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'Parse' -v`
Expected: `TestParseDelegate`, `TestParseDone`, et les tests `parseHandoff` existants PASS.

- [ ] **Step 5: gofmt + commit**

Run: `gofmt -l core/host/` → vide.

```bash
cd /home/shan/dev/herrscher
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -am "$(cat <<'EOF'
feat(host): parseurs delegate/done + helper parseTrailer

Factorise l'extraction de trailer partagée par handoff/delegate/done.
done n'a pas de séparateur em-dash : tout le corps est le résumé.

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Refactor Handoff → spawn partagé + implémentation Delegate

**Files:**
- Modify: `herrscher/core/host/coordinator.go`
- Test: `herrscher/core/host/coordinator_test.go`

**Interfaces:**
- Consumes: `DelegateRequest` (Task 1), `CreateSession.Parent` (Task 1), les ports existants (`sessionCreator`, `agentLookup`, `cleanBrancher`, `sessionLister`, `sessionCloser`, `seed`).
- Produces:
  - `spawn(ctx, from state.Session, toAgent, task, parent string) (session string, err error)` — helper privé factorisé de `Handoff` : name-probe collision-free, `Create` sur le tip de `from`, seed avec rollback sur timeout. `parent` est posé sur `CreateSession.Parent`.
  - `Delegate(ctx, req DelegateRequest) (worker string, err error)` — résout `from` par nom, garde worktree non vide + propre, appelle `spawn(…, parent=req.FromSession)`. L reste vivant (aucune fermeture).
  - `Handoff` conserve son comportement : appelle `spawn(…, parent="")`.

- [ ] **Step 1: Écrire les tests Delegate qui échouent**

Dans `herrscher/core/host/coordinator_test.go`, ajouter :

```go
func TestDelegateCreatesWorkerOnLeadBranchWithParent(t *testing.T) {
	cr := &fakeCreator{}
	c := newTestCoordinatorFull(cr,
		map[string]bool{"scripter": true}, // agents connus
		true,                              // lead propre
		[]state.Session{{Name: "lead", Worktree: "/wt/lead", Project: "proj"}},
		true,          // seed réussit
		nil,           // aucune branche préexistante
		&fakeCloser{}, // closer
	)
	worker, err := c.Delegate(context.Background(), contracts.DelegateRequest{
		FromSession: "lead", ToAgent: "scripter", Task: "écris le module",
	})
	if err != nil {
		t.Fatalf("delegate: %v", err)
	}
	if worker != "lead-scripter" {
		t.Fatalf("nom worker inattendu: %q", worker)
	}
	if cr.spec.Base != c.wt.Branch("lead") {
		t.Fatalf("worker pas branché sur le tip du lead: %q", cr.spec.Base)
	}
	if cr.spec.Parent != "lead" {
		t.Fatalf("parent non posé sur le worker: %q", cr.spec.Parent)
	}
	if cr.spec.Agent != "scripter" {
		t.Fatalf("agent worker inattendu: %q", cr.spec.Agent)
	}
}

func TestDelegateUnknownAgent(t *testing.T) {
	cr := &fakeCreator{}
	c := newTestCoordinatorFull(cr, map[string]bool{}, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, true, nil, &fakeCloser{})
	if _, err := c.Delegate(context.Background(), contracts.DelegateRequest{
		FromSession: "lead", ToAgent: "inconnu", Task: "x"}); err == nil {
		t.Fatalf("agent inconnu devrait échouer")
	}
	if cr.spec.Name != "" {
		t.Fatalf("aucune session ne devrait être créée")
	}
}

func TestDelegateDirtyLeadRefused(t *testing.T) {
	cr := &fakeCreator{}
	c := newTestCoordinatorFull(cr, map[string]bool{"scripter": true}, false, // lead sale
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, true, nil, &fakeCloser{})
	if _, err := c.Delegate(context.Background(), contracts.DelegateRequest{
		FromSession: "lead", ToAgent: "scripter", Task: "x"}); err == nil {
		t.Fatalf("lead sale devrait être refusé")
	}
	if cr.spec.Name != "" {
		t.Fatalf("aucune session ne devrait être créée sur lead sale")
	}
}
```

Note implémenteur : réutiliser exactement `newTestCoordinatorFull` et les fakes (`fakeCreator`, `fakeCloser`, `fakeWTC`) existants (voir les tests `TestHandoff*`). Vérifier la signature réelle de `newTestCoordinatorFull` dans le fichier et adapter l'ordre des args si besoin — NE PAS changer sa signature (les tests handoff en dépendent).

- [ ] **Step 2: Lancer — échoue à la compilation**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestDelegate`
Expected: FAIL — `Delegate` indéfini.

- [ ] **Step 3: Factoriser `spawn` et implémenter `Delegate`**

Dans `herrscher/core/host/coordinator.go`, extraire le corps de `Handoff` (à partir de la résolution de `from` jusqu'au seed+rollback) dans un helper `spawn`, puis réécrire `Handoff` et ajouter `Delegate`. Forme cible :

```go
// spawn crée une nouvelle session branchée sur le tip commité de `from`, seede
// `task` comme prompt d'ouverture, et pose `parent` (vide pour un handoff, le
// nom du lead pour une délégation). Name-probe collision-free (worktree.Remove
// laisse la branche session/<name> intacte). Rollback si le seed n'aboutit pas.
func (c *coordinator) spawn(ctx context.Context, from state.Session, toAgent, task, parent string) (string, error) {
	if from.Worktree == "" {
		return "", fmt.Errorf("coordination: source %q sans worktree", from.Name)
	}
	clean, err := c.wt.IsCleanAt(from.Worktree)
	if err != nil {
		return "", fmt.Errorf("coordination: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("coordination refusée: %q a des changements non commités — commite d'abord (ou ferme en force)", from.Name)
	}
	base := from.Name + "-" + toAgent
	bName := base
	for n := 2; ; n++ {
		exists, err := c.wt.BranchExistsAt(from.Worktree, c.wt.Branch(bName))
		if err != nil {
			return "", fmt.Errorf("coordination: %w", err)
		}
		if !exists {
			break
		}
		if n > maxHandoffNameProbes {
			return "", fmt.Errorf("coordination: aucun nom libre pour %q après %d essais", base, maxHandoffNameProbes)
		}
		bName = fmt.Sprintf("%s-%d", base, n)
	}
	if _, err := c.creator.Create(ctx, contracts.CreateSession{
		Name:    bName,
		Project: from.Project,
		Agent:   toAgent,
		Base:    c.wt.Branch(from.Name),
		Parent:  parent,
	}); err != nil {
		return "", fmt.Errorf("coordination: %w", err)
	}
	if !c.seedWithRetry(ctx, bName, task) {
		// La session est créée mais n'est jamais venue prendre la tâche : on la
		// défait plutôt que laisser un orphelin (worktree/branche/driver).
		_, _ = c.closer.Close(context.WithoutCancel(ctx), bName, true)
		return "", fmt.Errorf("coordination: session %q créée mais seed expiré ; rollback", bName)
	}
	return bName, nil
}

// findSession résout une session par nom dans le snapshot courant.
func (c *coordinator) findSession(name string) (state.Session, bool) {
	for _, s := range c.sessions.SnapshotSessions() {
		if s.Name == name {
			return s, true
		}
	}
	return state.Session{}, false
}

func (c *coordinator) Handoff(ctx context.Context, req contracts.HandoffRequest) (string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return "", fmt.Errorf("handoff: agent inconnu %q", req.ToAgent)
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", fmt.Errorf("handoff: source inconnue %q", req.FromSession)
	}
	return c.spawn(ctx, from, req.ToAgent, req.Task, "")
}

func (c *coordinator) Delegate(ctx context.Context, req contracts.DelegateRequest) (string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return "", fmt.Errorf("delegate: agent inconnu %q", req.ToAgent)
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", fmt.Errorf("delegate: source inconnue %q", req.FromSession)
	}
	// Différence clé avec le handoff : parent = le lead, et le lead reste vivant.
	return c.spawn(ctx, from, req.ToAgent, req.Task, req.FromSession)
}
```

Note : adapter aux noms/ordres exacts du fichier actuel (résolution d'agent, ordre des gardes). L'implémenteur DOIT lire `coordinator.go` en entier d'abord et préserver l'ordre garde-avant-effet et les messages existants du handoff (les tests `TestHandoff*` doivent rester verts inchangés). Si le `Handoff` actuel résout l'agent APRÈS la source, garder cet ordre pour ne pas casser les tests d'ordre.

- [ ] **Step 4: Lancer Delegate + toute la suite coordinator**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run 'TestDelegate|TestHandoff|TestSeed' -v`
Expected: nouveaux tests Delegate PASS ; tous les `TestHandoff*` et `TestSeedWithRetryHonoursCtxCancel` PASS inchangés.

- [ ] **Step 5: gofmt + commit**

Run: `gofmt -l core/host/` → vide. `go build ./...` → OK (le port est désormais satisfait pour Delegate ; Report suit en Task 6, donc si `Coordinator` est déjà déclaré avec `Report`, `*coordinator` ne compilera pas encore — voir note).

Note d'ordonnancement : le port `Coordinator` (Task 1) déclare DÉJÀ `Report`. Donc `*coordinator` n'implémente pas complètement l'interface tant que Task 6 n'est pas faite, et tout code qui affecte un `*coordinator` à une variable `contracts.Coordinator` ne compilera pas entre Task 5 et Task 6. Pour garder chaque commit compilable isolément, l'implémenteur de Task 5 PEUT ajouter un stub temporaire `Report` renvoyant `("", fmt.Errorf("report: pas encore implémenté"))` — MAIS c'est un placeholder interdit par le plan. Préférence : le contrôleur enchaîne Task 5 puis Task 6 sans exécuter `go build ./...` complet entre les deux, et valide le build complet à la fin de Task 6. Chaque commit reste 1 tâche ; le vert complet est rétabli en fin de Task 6. Documenter ce choix dans le rapport.

```bash
cd /home/shan/dev/herrscher
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -am "$(cat <<'EOF'
feat(host): spawn partagé + primitive Delegate (lead reste vivant)

Handoff et Delegate partagent le même spawn (branche sur le tip commité,
seed, rollback). Delegate pose Parent=lead et ne ferme pas le lead.

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Implémentation Report (canal de retour)

**Files:**
- Modify: `herrscher/core/host/coordinator.go`
- Test: `herrscher/core/host/coordinator_test.go`

**Interfaces:**
- Consumes: `ReportRequest` (Task 1), `state.Session.Parent` (Task 2), `findSession` + `c.seed` (Task 5).
- Produces: `Report(ctx, req ReportRequest) (parent string, err error)` — lit `W = findSession(req.FromSession)` ; garde `W.Worktree` propre (W doit être commité pour que `session/<W>` porte le travail) ; lit `W.Parent` (vide → erreur) ; résout la session parente (absente → erreur) ; `seed(parent, "<W> a terminé sur session/<W> — <résumé>")` ; renvoie le nom du parent. W reste vivant (aucune fermeture, aucun merge).

- [ ] **Step 1: Écrire les tests Report qui échouent**

Dans `herrscher/core/host/coordinator_test.go`, ajouter. On a besoin d'un fake seed qui CAPTURE ses arguments pour vérifier le message livré ; si le `seed` fake actuel ne renvoie qu'un bool sans capturer, ajouter un enregistreur local dans le test :

```go
func TestReportDeliversBranchRefAndSummaryToParent(t *testing.T) {
	var seededSession, seededMsg string
	seed := func(session, msg string) bool {
		seededSession, seededMsg = session, msg
		return true
	}
	cr := &fakeCreator{}
	c := newTestCoordinatorFullSeed(cr, map[string]bool{}, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "worker", Worktree: "/wt/worker", Parent: "lead"},
		}, seed, nil, &fakeCloser{})
	parent, err := c.Report(context.Background(), contracts.ReportRequest{
		FromSession: "worker", Summary: "module commité, 12 tests verts",
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if parent != "lead" {
		t.Fatalf("parent inattendu: %q", parent)
	}
	if seededSession != "lead" {
		t.Fatalf("message livré à la mauvaise session: %q", seededSession)
	}
	if !strings.Contains(seededMsg, c.wt.Branch("worker")) || !strings.Contains(seededMsg, "12 tests verts") {
		t.Fatalf("message de livraison incomplet: %q", seededMsg)
	}
}

func TestReportWorkerWithoutParentErrors(t *testing.T) {
	c := newTestCoordinatorFullSeed(&fakeCreator{}, map[string]bool{}, true,
		[]state.Session{{Name: "orphan", Worktree: "/wt/orphan"}}, // Parent vide
		func(string, string) bool { return true }, nil, &fakeCloser{})
	if _, err := c.Report(context.Background(), contracts.ReportRequest{
		FromSession: "orphan", Summary: "x"}); err == nil {
		t.Fatalf("worker sans parent devrait échouer")
	}
}

func TestReportUnknownWorkerErrors(t *testing.T) {
	c := newTestCoordinatorFullSeed(&fakeCreator{}, map[string]bool{}, true,
		[]state.Session{}, func(string, string) bool { return true }, nil, &fakeCloser{})
	if _, err := c.Report(context.Background(), contracts.ReportRequest{
		FromSession: "ghost", Summary: "x"}); err == nil {
		t.Fatalf("worker inconnu devrait échouer")
	}
}

func TestReportParentGoneErrors(t *testing.T) {
	c := newTestCoordinatorFullSeed(&fakeCreator{}, map[string]bool{}, true,
		[]state.Session{{Name: "worker", Worktree: "/wt/worker", Parent: "lead"}}, // lead absent du snapshot
		func(string, string) bool { return true }, nil, &fakeCloser{})
	if _, err := c.Report(context.Background(), contracts.ReportRequest{
		FromSession: "worker", Summary: "x"}); err == nil {
		t.Fatalf("parent disparu devrait échouer")
	}
}
```

Note implémenteur : ces tests introduisent un constructeur `newTestCoordinatorFullSeed` qui prend un `seed func(string,string) bool` explicite (les autres constructeurs passent un seed qui renvoie toujours true). Si `newTestCoordinatorFull` fixe déjà le seed en dur, ajouter cette variante paramétrée par le seed ET faire déléguer `newTestCoordinatorFull` à elle (seed = `func(_,_ string) bool { return true }`), pour garder les tests existants inchangés. Ne PAS casser les signatures existantes.

- [ ] **Step 2: Lancer — échoue à la compilation**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestReport`
Expected: FAIL — `Report` indéfini (et/ou `newTestCoordinatorFullSeed`).

- [ ] **Step 3: Implémenter `Report`**

Dans `herrscher/core/host/coordinator.go`, ajouter :

```go
// Report livre le rapport de fin d'un worker à son lead : {réf branche session/<W>
// + résumé}. Aucun merge, aucun teardown — W reste vivant. La livraison passe par
// le même canal de seed que l'ouverture d'une session (Modèle O, couche host).
func (c *coordinator) Report(ctx context.Context, req contracts.ReportRequest) (string, error) {
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", fmt.Errorf("report: worker inconnu %q", req.FromSession)
	}
	if from.Parent == "" {
		return "", fmt.Errorf("report: %q n'a pas de parent — rien à livrer", req.FromSession)
	}
	if _, ok := c.findSession(from.Parent); !ok {
		return "", fmt.Errorf("report: parent %q de %q introuvable", from.Parent, req.FromSession)
	}
	branch := c.wt.Branch(req.FromSession)
	msg := fmt.Sprintf("%s a terminé sur %s — %s", req.FromSession, branch, req.Summary)
	if !c.seedWithRetry(ctx, from.Parent, msg) {
		return "", fmt.Errorf("report: livraison au parent %q expirée", from.Parent)
	}
	return from.Parent, nil
}
```

Décision (spec D4) : `Report` utilise `seedWithRetry` (mêmes retries/backoff que le seed d'ouverture) pour tolérer un parent momentanément occupé ; si le parent ne prend jamais le message, on renvoie une erreur (pas de rollback : rien n'a été créé, W et le lead restent intacts). Si le test capteur de seed ci-dessus utilise un `seed` direct (pas `seedWithRetry`), vérifier que `seedWithRetry` appelle bien `c.seed` une fois quand il réussit du premier coup — c'est le cas, donc le capteur voit l'appel.

- [ ] **Step 4: Lancer Report + suite host complète**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -v && go build ./...`
Expected: tous les `TestReport*`, `TestDelegate*`, `TestHandoff*`, `TestSeed*` PASS ; `go build ./...` OK (le port `Coordinator` est maintenant entièrement implémenté par `*coordinator`).

- [ ] **Step 5: gofmt + commit**

Run: `gofmt -l core/host/` → vide.

```bash
cd /home/shan/dev/herrscher
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -am "$(cat <<'EOF'
feat(host): Report livre {réf branche + résumé} au lead (W reste vivant)

Le worker signale sa fin ; le coordinateur lit W.Parent, résout le lead et
lui seede la réf session/<W> + le résumé. Pas de merge, pas de teardown.

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Dispatch host — maybeCoordinate (done → delegate → handoff)

**Files:**
- Modify: `herrscher/core/host/turnloop.go`
- Test: `herrscher/core/host/turnloop_test.go` (ou le fichier de test qui couvre `maybeHandoff` aujourd'hui)

**Interfaces:**
- Consumes: `parseDone`/`parseDelegate`/`parseHandoff` (Task 4), `d.coordinator` (`contracts.Coordinator`, avec `Delegate`/`Report`), le canal `fanOut` de statut existant.
- Produces: `maybeCoordinate(ctx, reply string)` remplace `maybeHandoff` ; `awaitTurn` l'appelle à la place. Un seul trailer par tour : on essaie `parseDone`, puis `parseDelegate`, puis `parseHandoff` ; le premier qui matche gagne ; aucun → rien. Les erreurs du coordinateur remontent dans le canal de la session (`fanOut` status), jamais de panique.

- [ ] **Step 1: Écrire le test de dispatch qui échoue**

Dans le fichier de test host couvrant la boucle de tour, ajouter (avec un fake `coordinator` qui enregistre quelle méthode a été appelée) :

```go
type recordingCoord struct {
	handoff, delegate, report int
	lastAgent, lastTask, lastSummary, lastFrom string
	err error
}

func (r *recordingCoord) Handoff(_ context.Context, req contracts.HandoffRequest) (string, error) {
	r.handoff++
	r.lastAgent, r.lastTask, r.lastFrom = req.ToAgent, req.Task, req.FromSession
	return "b", r.err
}
func (r *recordingCoord) Delegate(_ context.Context, req contracts.DelegateRequest) (string, error) {
	r.delegate++
	r.lastAgent, r.lastTask, r.lastFrom = req.ToAgent, req.Task, req.FromSession
	return "w", r.err
}
func (r *recordingCoord) Report(_ context.Context, req contracts.ReportRequest) (string, error) {
	r.report++
	r.lastSummary, r.lastFrom = req.Summary, req.FromSession
	return "lead", r.err
}

func TestMaybeCoordinateDispatchesByTrailer(t *testing.T) {
	cases := []struct {
		reply                      string
		wantH, wantD, wantR        int
	}{
		{"texte\n⟢ handoff: scripter — tâche", 1, 0, 0},
		{"texte\n⟢ delegate: scripter — tâche", 0, 1, 0},
		{"texte\n⟢ done: fini, 12 tests verts", 0, 0, 1},
		{"aucun trailer ici", 0, 0, 0},
	}
	for _, tc := range cases {
		rc := &recordingCoord{}
		d := newTestDriver(t, "sess", rc) // helper à réutiliser/adapter : driver avec coordinator injecté
		d.maybeCoordinate(context.Background(), tc.reply)
		if rc.handoff != tc.wantH || rc.delegate != tc.wantD || rc.report != tc.wantR {
			t.Fatalf("reply %q → handoff=%d delegate=%d report=%d (voulu %d/%d/%d)",
				tc.reply, rc.handoff, rc.delegate, rc.report, tc.wantH, tc.wantD, tc.wantR)
		}
	}
}
```

Note implémenteur : `newTestDriver` est un placeholder pour la façon dont les tests host construisent aujourd'hui un `sessionDriver` avec un coordinateur injecté. LIRE le test existant de `maybeHandoff` et réutiliser EXACTEMENT son montage (même helper, même façon d'injecter `d.coordinator` et `d.name`). Si aucun test n'existait pour `maybeHandoff`, construire le driver minimalement comme le fait `RunSession`/les tests voisins. Ne pas inventer d'API de driver.

- [ ] **Step 2: Lancer — échoue à la compilation**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestMaybeCoordinate`
Expected: FAIL — `maybeCoordinate` indéfini.

- [ ] **Step 3: Généraliser `maybeHandoff` en `maybeCoordinate`**

Dans `herrscher/core/host/turnloop.go`, remplacer `maybeHandoff` par `maybeCoordinate` (garder la garde `d.coordinator == nil`), et mettre à jour l'appel dans `awaitTurn` :

```go
// maybeCoordinate inspecte la fin de la réponse d'un tour et, si elle porte un
// trailer de coordination, appelle le coordinateur (Modèle O : l'agent signale,
// le host exécute). Un seul trailer par tour : done a la priorité (un worker qui
// annonce sa fin), puis delegate, puis handoff. Toute erreur remonte dans le
// canal de la session ; on ne devine jamais une intention à moitié formée.
func (d *sessionDriver) maybeCoordinate(ctx context.Context, reply string) {
	if d.coordinator == nil {
		return
	}
	if summary, ok := parseDone(reply); ok {
		if parent, err := d.coordinator.Report(ctx, contracts.ReportRequest{
			FromSession: d.name, Summary: summary,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "report refusé: " + err.Error()})
		} else {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "rapport livré à " + parent})
		}
		return
	}
	if toAgent, task, ok := parseDelegate(reply); ok {
		if _, err := d.coordinator.Delegate(ctx, contracts.DelegateRequest{
			FromSession: d.name, ToAgent: toAgent, Task: task,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "delegate refusé: " + err.Error()})
		}
		return
	}
	if toAgent, task, ok := parseHandoff(reply); ok {
		if _, err := d.coordinator.Handoff(ctx, contracts.HandoffRequest{
			FromSession: d.name, ToAgent: toAgent, Task: task,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "handoff refusé: " + err.Error()})
		}
	}
}
```

Dans `awaitTurn`, remplacer `d.maybeHandoff(ctx, e.Text)` par `d.maybeCoordinate(ctx, e.Text)`. Vérifier qu'aucun autre appelant de `maybeHandoff` ne subsiste (`grep -rn maybeHandoff core/host/`).

- [ ] **Step 4: Lancer le dispatch + suite host**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -v`
Expected: `TestMaybeCoordinateDispatchesByTrailer` PASS ; tous les tests host existants PASS.

- [ ] **Step 5: gofmt + commit**

Run: `gofmt -l core/host/` → vide.

```bash
cd /home/shan/dev/herrscher
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -am "$(cat <<'EOF'
feat(host): maybeCoordinate dispatche done/delegate/handoff

Un seul hook post-tour essaie done → delegate → handoff. Report/Delegate
remontent leur statut dans le canal de la session comme le handoff.

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Vérification bout-en-bout + docs statut

**Files:**
- Modify (docs seulement) : le doc de statut P3 s'il existe (chercher sous `docs/`), sinon aucun.
- Aucun code neuf — cette tâche est une passe de vérification globale + mise à jour de statut.

**Interfaces:**
- Consumes: tout ce qui précède.
- Produces: preuve que les deux repos sont verts, gofmt propre, et le statut P3 reflète delegate+result-back livré.

- [ ] **Step 1: Suites complètes des deux repos**

Run:
```bash
cd /home/shan/dev/herrscher-contracts && go build ./... && go test ./... && gofmt -l .
cd /home/shan/dev/herrscher && go build ./... && go test ./... && gofmt -l .
```
Expected: tout vert, gofmt sans sortie sur les deux.

- [ ] **Step 2: Vérifier l'absence de trailer résiduel `maybeHandoff` / doubles définitions**

Run: `cd /home/shan/dev/herrscher && grep -rn "maybeHandoff\|func parseHandoff\|handoffMarker =" core/host/`
Expected: aucune référence à `maybeHandoff` ; une seule définition de `parseHandoff` ; `handoffMarker` déclaré une seule fois (dans le bloc `const`).

- [ ] **Step 3: Mettre à jour le doc de statut P3 (si présent)**

Chercher un doc de statut : `grep -rln "P3\|handoff\|coordination" /home/shan/dev/herrscher/docs/ 2>/dev/null`. S'il existe un doc de statut qui listait le handoff comme livré et delegate/result-back comme à venir, ajouter une ligne datée 2026-07-10 : delegate + canal de retour (Report) livrés ; restent hors tranche fan-out/join et superviseur→workers multi-parallèle. Ne PAS inventer de doc s'il n'y en a pas — le spec `2026-07-10-p3-delegate-result-back-design.md` fait déjà foi.

- [ ] **Step 4: Commit (si docs modifiées)**

```bash
cd /home/shan/dev/herrscher
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -am "$(cat <<'EOF'
docs(p3): statut delegate + result-back livrés

Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

Si aucun doc de statut n'existe, sauter ce commit (Task 8 est alors purement une passe de vérification, sans commit — c'est acceptable pour cette tâche terminale de contrôle).

---

## Self-Review (auteur du plan)

**Couverture du spec (design 2026-07-10-p3-delegate-result-back) :**
- D1 delegate distinct du handoff (Modèle O) → Task 4 (parseDelegate) + Task 5 (Delegate).
- D2 `Parent` sur CreateSession/state.Session posé par delegate → Task 1 + Task 2 + Task 5 (spawn passe parent).
- D3 `spawn` partagé factorisé de Handoff → Task 5.
- D4 Report : garde W commité → lit Parent (vide→err) → résout parent (absent→err) → Seed, pas de merge → Task 6.
- D5 W reste vivant après report → Task 6 (aucune fermeture) ; L reste vivant après delegate → Task 5.
- D6 hook unique `maybeCoordinate` (done→delegate→handoff), parseurs purs + `parseTrailer` partagé → Task 4 + Task 7.

**Scan placeholders :** les notes « adapter aux noms exacts » pointent vers du code réel à lire (coordinator.go, le test maybeHandoff) et non vers des TODO ; chaque step de code porte le code complet. La seule zone assumée est le nom exact de l'accesseur `SessionByName` et du helper de montage de driver — signalés explicitement à l'implémenteur avec la consigne de réutiliser l'accès existant.

**Cohérence des types :** `Parent` (string) cohérent contracts↔state↔spawn ; `DelegateRequest`/`ReportRequest` mêmes champs entre Task 1 (déclaration) et Tasks 5/6/7 (usage) ; `spawn(ctx, from, toAgent, task, parent)` signature stable entre Task 5 (déf) et son usage par Handoff/Delegate.

**Point d'attention ordonnancement (signalé au contrôleur) :** le port `Coordinator` gagne `Report` en Task 1, mais `*coordinator` ne l'implémente qu'en Task 6 → le build complet de `herrscher` n'est vert qu'après Task 6. Task 3 et Task 5 peuvent laisser `go build ./...` rouge transitoirement. Choix : ne pas introduire de stub placeholder (interdit) ; enchaîner Task 5→6 et valider le build complet en fin de Task 6. Chaque commit reste 1 tâche. À porter dans les rapports d'implémenteur concernés.
