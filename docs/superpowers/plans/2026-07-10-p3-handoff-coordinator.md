# P3 — Handoff Coordinator — Plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Livrer le handoff A→B de bout en bout : un agent A signale un relais par un
trailer, le host (couche orchestration) le valide et un `Coordinator` porté par le hub crée
B **branchée sur le tip commité de A** (continuation sans merge) puis seede la tâche comme
premier tour de B.

**Architecture:** Modèle O (piloté par la couche orchestration, pas par l'agent). Le driver
host-side (`core/host/turnloop.go`) inspecte la réponse à `reply{done}`, la parse
(`parseHandoff`, pur), et invoque le port `contracts.Coordinator`. L'impl du `Coordinator`
(`core/host/coordinator.go`) résout le profil cible (Store), garde « A commité »
(worktreer), crée B via `SessionControl.Create` avec un nouveau champ `Base` (→ `git worktree
add -b session/<B> session/<A>`), et seede la tâche via un `Seed(session, task)` package-level
calqué sur le `Pick` existant.

**Tech Stack:** Go ; `go test ./...` + `gofmt` sur les deux repos touchés
(`herrscher-contracts`, `herrscher`). Spec :
`docs/superpowers/specs/2026-07-10-p3-handoff-coordinator-design.md`.

## Global Constraints

- Commits signés **`Akayashuu <sauvageleo1@gmail.com>`** :
  `git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit …` (jamais
  happy62101@gmail.com). Footer `Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc`.
- **1 tâche = 1 commit.** Ne pas auto-merger sans feu vert utilisateur.
- Feature **générique OSS** : aucune connaissance métier Roblox/Neublox dans le code herrscher.
- **Aucune surface MCP ajoutée** ; le contrat de la factory plugin `Orchestrator` reste inchangé.
- `Base` par défaut `""` = **comportement actuel strictement inchangé** (aucune régression des
  sessions non-handoff).
- `gofmt` propre ; `go test ./...` vert sur chaque repo touché.
- Ne DRY-fie que ce que la tranche exige ; pas de refactor hors périmètre.
- Travailler sur la branche `feat/p3-handoff-coordinator` (déjà créée, porte le spec). Le repo
  `herrscher-contracts` est sur `master` : y créer une branche `feat/p3-coordinator-port`.

**Note inter-repos :** `herrscher` voit `herrscher-contracts` via le `go.work` local
(`use ../herrscher-contracts`), donc le champ/port ajouté en Task 1 est visible immédiatement
sans tag. Le bump de version/tag de contracts est l'étape de release habituelle, **hors
périmètre de ce plan**.

---

### Task 1 : contracts — champ `Base` + port `Coordinator` + `HandoffRequest`

**Files:**
- Modify: `herrscher-contracts/session_control.go` (struct `CreateSession`, ajouter `Base`)
- Create: `herrscher-contracts/coordinator.go`
- Test: `herrscher-contracts/coordinator_test.go`

**Interfaces:**
- Produces : `CreateSession.Base string` ; `type HandoffRequest struct { FromSession, ToAgent,
  Task string }` ; `type Coordinator interface { Handoff(ctx context.Context, req
  HandoffRequest) (session string, err error) }`.

- [ ] **Step 1 : Écrire le test qui échoue**

`herrscher-contracts/coordinator_test.go` :

```go
package contracts

import (
	"context"
	"testing"
)

// fakeCoordinator locks the port signature at compile time and exercises it.
type fakeCoordinator struct{ got HandoffRequest }

func (f *fakeCoordinator) Handoff(_ context.Context, req HandoffRequest) (string, error) {
	f.got = req
	return req.ToAgent + "-session", nil
}

func TestCoordinatorPortRoundTrip(t *testing.T) {
	var c Coordinator = &fakeCoordinator{}
	name, err := c.Handoff(context.Background(), HandoffRequest{
		FromSession: "alpha", ToAgent: "scripter", Task: "finir le module",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "scripter-session" {
		t.Fatalf("got %q", name)
	}
}

func TestCreateSessionHasBase(t *testing.T) {
	// Base is the ref a new session's worktree branches off (empty = current behaviour).
	spec := CreateSession{Name: "b", Base: "session/alpha"}
	if spec.Base != "session/alpha" {
		t.Fatalf("Base not set: %q", spec.Base)
	}
}
```

- [ ] **Step 2 : Lancer le test — échec attendu**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... 2>&1 | tail`
Expected: échec de compilation (`Coordinator`, `HandoffRequest`, `CreateSession.Base` inexistants).

- [ ] **Step 3 : Écrire l'implémentation minimale**

`herrscher-contracts/coordinator.go` :

```go
package contracts

import "context"

// HandoffRequest is the typed intent for a relay A→B: FromSession finishes and B
// (a ToAgent profile) continues the same committed work. Task seeds B's opening
// turn. It is the shared source of truth for every coordination policy built on
// top of Coordinator (handoff now, supervisor→workers and fan-out later).
type HandoffRequest struct {
	FromSession string // the source session handing off (its branch is B's base)
	ToAgent     string // durable agent profile B is provisioned from
	Task        string // seeds B's opening turn
}

// Coordinator is the inter-session coordination port. It lives at the layer that
// sees every session and drives the hub (the host), NOT the per-session
// Orchestrator plugin (which only sees its own turns and holds no hub handle).
// The agent only signals intent; the host validates and the Coordinator executes.
type Coordinator interface {
	// Handoff creates B continuing FromSession's committed work and returns B's
	// session name. It errors on unknown agent, a missing/dirty source worktree,
	// or a failed create — leaving nothing partial.
	Handoff(ctx context.Context, req HandoffRequest) (session string, err error)
}
```

Dans `herrscher-contracts/session_control.go`, ajouter le champ à `CreateSession` (après
`ConsolidateEvery`), avec un commentaire :

```go
	ConsolidateEvery int
	// Base is the git ref the new session's worktree branches off (empty = the
	// default fresh branch). A handoff sets it to the source's session/<A> so B
	// continues A's committed tip without a merge.
	Base string
```

- [ ] **Step 4 : Lancer le test — succès attendu**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5 : gofmt + commit**

```bash
cd /home/shan/dev/herrscher-contracts
git checkout -b feat/p3-coordinator-port
gofmt -w coordinator.go session_control.go coordinator_test.go
git add coordinator.go coordinator_test.go session_control.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat: Coordinator port + HandoffRequest + CreateSession.Base

Co-Authored-By: Claude <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc"
```

---

### Task 2 : worktree — `base` sur `Create` + extraction `IsCleanAt`

**Files:**
- Modify: `herrscher/core/internal/worktree/worktree.go` (`Create`, `Remove`, nouvelle `IsCleanAt`)
- Test: `herrscher/core/internal/worktree/worktree_test.go` (créer si absent)

**Interfaces:**
- Produces : `func (w *Worktreer) Create(repo, name, base string) (string, error)` (base `""` =
  actuel) ; `func (w *Worktreer) IsCleanAt(worktreePath string) (bool, error)`.
- Consumes : `Branch`, `Path`, `isGitRepo` existants.

- [ ] **Step 1 : Écrire les tests qui échouent**

Le test invoque un vrai `git` dans un repo temporaire (herrscher teste déjà git ainsi ; pas de
mock d'`exec`). Ajouter/compléter `worktree_test.go` :

```go
package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-qm", "root"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	return repo
}

func TestCreateWithBaseBranchesOffIt(t *testing.T) {
	w := NewWorktreer(context.Background(), "")
	repo := gitInit(t)
	// Base worktree "alpha" gets a commit so its tip carries work.
	pa, err := w.Create(repo, "alpha", "")
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pa, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "work"}} {
		if out, err := exec.Command("git", append([]string{"-C", pa}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	// B branches off session/alpha and must inherit f.txt.
	pb, err := w.Create(repo, "beta", "session/alpha")
	if err != nil {
		t.Fatalf("create beta on base: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pb, "f.txt")); err != nil {
		t.Fatalf("beta did not inherit alpha's committed work: %v", err)
	}
}

func TestIsCleanAt(t *testing.T) {
	w := NewWorktreer(context.Background(), "")
	repo := gitInit(t)
	p, err := w.Create(repo, "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	clean, err := w.IsCleanAt(p)
	if err != nil || !clean {
		t.Fatalf("fresh worktree should be clean: clean=%v err=%v", clean, err)
	}
	if err := os.WriteFile(filepath.Join(p, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, err = w.IsCleanAt(p)
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Fatal("worktree with an untracked file should be dirty")
	}
}
```

- [ ] **Step 2 : Lancer — échec attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/worktree/ 2>&1 | tail`
Expected: échec de compilation (`Create` prend 2 args, `IsCleanAt` inexistant).

- [ ] **Step 3 : Écrire l'implémentation**

Remplacer `Create` et ajouter `IsCleanAt` ; faire réutiliser le check par `Remove`.

```go
// Create adds a worktree on branch session/<name> inside repo. When base is
// non-empty, the new branch starts at that ref (e.g. "session/<A>") so the
// worktree continues that tip without a merge; empty base keeps the default
// (branch from HEAD). Returns ("", nil) when repo is not a git repo (caller
// falls back to a shared session).
func (w *Worktreer) Create(repo, name, base string) (string, error) {
	if !w.isGitRepo(repo) {
		return "", nil
	}
	p := w.Path(repo, name)
	args := []string{"-C", repo, "worktree", "add", p, "-b", w.Branch(name)}
	if base != "" {
		args = append(args, base)
	}
	out, err := exec.CommandContext(w.ctx, "git", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("worktree add: %s", strings.TrimSpace(string(out)))
	}
	return p, nil
}

// IsCleanAt reports whether the worktree at path has no uncommitted changes. An
// error means the state can't be verified (missing dir, not a git repo) — the
// caller must treat that as "not safe", never as clean.
func (w *Worktreer) IsCleanAt(path string) (bool, error) {
	out, err := exec.CommandContext(w.ctx, "git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("cannot verify clean state of %q: %w", path, err)
	}
	return strings.TrimSpace(string(out)) == "", nil
}
```

Faire réutiliser le check par `Remove` (DRY, comportement identique) :

```go
func (w *Worktreer) Remove(repo, name string, force bool) error {
	p := w.Path(repo, name)
	if !force {
		clean, err := w.IsCleanAt(p)
		if err != nil {
			// Can't verify the tree is clean: refuse rather than risk discarding
			// work. force: bypasses.
			return fmt.Errorf("worktree %q: %v — close with force:true to remove anyway", name, err)
		}
		if !clean {
			return fmt.Errorf("worktree %q has uncommitted changes", name)
		}
	}
	args := []string{"-C", repo, "worktree", "remove", p}
	if force {
		args = append(args, "--force")
	}
	out, err := exec.CommandContext(w.ctx, "git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("worktree remove: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
```

- [ ] **Step 4 : Lancer — succès attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/worktree/ 2>&1 | tail`
Expected: PASS (les deux nouveaux + éventuels existants).

- [ ] **Step 5 : Corriger les appelants de `Create` (compile-break volontaire)**

Le changement de signature casse `core/internal/manager/session.go:120`. La correction réelle
est en Task 3 ; ici, faire juste compiler l'unité worktree. NE PAS committer tant que le repo
ne build pas. Enchaîner directement sur Task 3 avant tout commit (Task 2 et 3 partagent le même
build vert). Le commit de Task 2 se fait à la fin de Task 3.

---

### Task 3 : `session create` — plumber `base` jusqu'à `worktree.Create`

**Files:**
- Modify: `herrscher/core/internal/manager/commands.go` (Param `base`)
- Modify: `herrscher/core/internal/manager/session.go` (Lookup `base`, passer à `wt.Create`)
- Modify: `herrscher/core/internal/manager/ports.go` (interface `worktrees`)
- Modify: `herrscher/core/internal/manager/handler_test.go` (fake `fakeWT.Create`)

**Interfaces:**
- Consumes : `Worktreer.Create(repo, name, base string)` (Task 2).
- Produces : le param `base` de `session create` ; `worktrees.Create(repo, name, base string)`.

- [ ] **Step 1 : Adapter l'interface + le fake (rendre le build cohérent)**

`ports.go` :

```go
type worktrees interface {
	Create(repo, name, base string) (path string, err error)
	Branch(name string) string
	Remove(repo, name string, force bool) error
}
```

`handler_test.go` (`fakeWT.Create`) — capter `base` pour l'asserer en Step 3 :

```go
type fakeWT struct {
	createdBase string // last base passed to Create
	// … champs existants conservés …
}

func (f *fakeWT) Create(repo, name, base string) (string, error) {
	f.createdBase = base
	return "/wt/" + name, nil
}
```
(Adapter au corps réel existant du fake — conserver son comportement de retour ; n'ajouter que
le param `base` et sa capture.)

- [ ] **Step 2 : Déclarer le param + le passer**

`commands.go`, dans le bloc `session create`, après le Param `consolidate_every` :

```go
			Param("base", "existing ref the new worktree branches off (e.g. session/<A>); empty = fresh branch", false).
```

`session.go`, remplacer l'appel `h.wt.Create(repo, name)` (ligne ~120) :

```go
		base := ""
		if b, ok := in.Lookup("base"); ok {
			base = b
		}
		path, err := h.wt.Create(repo, name, base)
```

- [ ] **Step 3 : Écrire le test qui prouve le plumbing**

Dans `handler_test.go`, ajouter un test qui crée une session avec `base` et vérifie qu'il
atteint le worktreer (suivre le style des tests de create existants pour l'assemblage du
handler + input) :

```go
func TestSessionCreatePassesBaseToWorktree(t *testing.T) {
	h, wt := newTestHandler(t) // helper existant renvoyant *Handler + *fakeWT ; sinon l'inliner
	_, err := runCreate(t, h, map[string]string{
		"name": "beta", "project": "game", "base": "session/alpha",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if wt.createdBase != "session/alpha" {
		t.Fatalf("base not plumbed to worktree: %q", wt.createdBase)
	}
}
```
Si `newTestHandler`/`runCreate` n'existent pas sous ces noms, réutiliser le harnais du test de
create déjà présent dans `handler_test.go` (même construction du handler + `contracts.Input`).

- [ ] **Step 4 : Lancer — succès attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/internal/worktree/ ./core/internal/manager/ 2>&1 | tail`
Expected: PASS (worktree Task 2 + manager Task 3).

- [ ] **Step 5 : gofmt + commit (couvre Task 2 + Task 3)**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/internal/worktree/*.go core/internal/manager/*.go
git add core/internal/worktree/ core/internal/manager/
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(worktree): base branch on Create + IsCleanAt; plumb session create base

Co-Authored-By: Claude <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc"
```

---

### Task 4 : hub.Create — mapper `spec.Base` → `args["base"]`

**Files:**
- Modify: `herrscher/core/host/hub.go` (`Create`)
- Test: `herrscher/core/host/hub_test.go` (créer/compléter)

**Interfaces:**
- Consumes : `contracts.CreateSession.Base` (Task 1) ; le param `base` du command (Task 3).

- [ ] **Step 1 : Écrire le test qui échoue**

Le test vérifie que `Create` propage `Base` dans l'argv du command via le seam registry. Suivre
le style de test du hub s'il existe ; sinon vérifier au minimum que `Base` non vide produit
`args["base"]`. Si le hub est difficile à tester en isolation, cette assertion est couverte par
l'intégration Task 8 — dans ce cas, réduire ce test à un stub de `reg` capturant l'`Input` :

```go
func TestHubCreatePlumbsBase(t *testing.T) {
	var captured contracts.Input
	h := newHubWithCapture(t, func(in contracts.Input) { captured = in }) // helper local sur un reg fake
	_, _ = h.Create(context.Background(), contracts.CreateSession{Name: "b", Base: "session/a"})
	if captured.Args["base"] != "session/a" {
		t.Fatalf("base not mapped: %q", captured.Args["base"])
	}
}
```
Si aucun seam de capture n'est raisonnablement isolable, **omettre ce test** et documenter que
le plumbing `Base` est couvert par l'intégration Task 8 (préférer la vérité à un test factice).

- [ ] **Step 2 : Lancer — échec attendu (ou passer directement à Step 3 si test omis)**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ 2>&1 | tail`

- [ ] **Step 3 : Implémenter**

Dans `hub.Create`, après `setStr("journal", spec.Journal)` :

```go
	setStr("base", spec.Base)
```

- [ ] **Step 4 : Lancer — succès attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5 : gofmt + commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/host/hub.go core/host/hub_test.go 2>/dev/null || gofmt -w core/host/hub.go
git add core/host/hub.go core/host/hub_test.go 2>/dev/null; git add core/host/hub.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): hub.Create maps CreateSession.Base to the session-create arg

Co-Authored-By: Claude <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc"
```

---

### Task 5 : `Seed(session, task)` — injecter le tour d'ouverture (calqué sur `Pick`)

**Files:**
- Modify: `herrscher/core/host/turnloop.go` (méthode `(*sessionDriver).Seed` + func package-level `Seed`)
- Test: `herrscher/core/host/turnloop_test.go` (créer/compléter)

**Interfaces:**
- Produces : `func Seed(session, task string) bool` (package `host`) — enfile un frame
  `{T:"input", Who:"handoff", Text: task}` dans la queue du driver nommé ; `false` si aucun
  driver vivant ne porte ce nom (comme `Pick`).
- Consumes : `sessionRegistry`, `registerDriver` existants.

- [ ] **Step 1 : Écrire le test qui échoue**

```go
func TestSeedEnqueuesInput(t *testing.T) {
	d := newSessionDriver("beta", nil, make(chan contracts.Event, 1), make(chan contracts.Event, 1))
	registerDriver("beta", d)
	defer unregisterDriver("beta")

	if !Seed("beta", "finir le module") {
		t.Fatal("Seed should return true for a live session")
	}
	select {
	case ev := <-d.queue:
		if ev.T != "input" || ev.Text != "finir le module" || ev.Who != "handoff" {
			t.Fatalf("unexpected frame: %+v", ev)
		}
	default:
		t.Fatal("no frame enqueued")
	}
	if Seed("ghost", "x") {
		t.Fatal("Seed should return false for an unknown session")
	}
}
```

- [ ] **Step 2 : Lancer — échec attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestSeed 2>&1 | tail`
Expected: échec de compilation (`Seed` inexistant).

- [ ] **Step 3 : Implémenter (juste après `Pick`/`(*sessionDriver).Pick` dans turnloop.go)**

```go
// Seed injects an opening input turn into this session's FIFO. A handoff uses it
// to hand B its task the same way a human message would arrive.
func (d *sessionDriver) Seed(task string) {
	d.queue <- contracts.Event{T: "input", Who: "handoff", Text: task}
}

// Seed routes an opening task to the named session's driver, returning false when
// no live session by that name is driving (mirror of Pick).
func Seed(session, task string) bool {
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return false
	}
	d.Seed(task)
	return true
}
```

- [ ] **Step 4 : Lancer — succès attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestSeed 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5 : gofmt + commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/host/turnloop.go core/host/turnloop_test.go
git add core/host/turnloop.go core/host/turnloop_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): Seed(session, task) enqueues a session's opening turn

Co-Authored-By: Claude <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc"
```

---

### Task 6 : `parseHandoff` — parseur pur du trailer

**Files:**
- Create: `herrscher/core/host/handoff.go`
- Test: `herrscher/core/host/handoff_test.go`

**Interfaces:**
- Produces : `func parseHandoff(reply string) (agent, task string, ok bool)`. Reconnaît une
  **ligne-trailer** `⟢ handoff: <agent> — <task>`. `ok=false` si absente/malformée. Tolère
  espaces autour ; l'agent et la tâche doivent être non vides. Le séparateur est le tiret cadratin
  `—` (U+2014).

- [ ] **Step 1 : Écrire les tests qui échouent**

```go
package host

import "testing"

func TestParseHandoff(t *testing.T) {
	cases := []struct {
		name         string
		reply        string
		agent, task  string
		ok           bool
	}{
		{"valid", "Voilà.\n⟢ handoff: scripter — finir le module", "scripter", "finir le module", true},
		{"trailing spaces", "x\n  ⟢ handoff:  scripter  —  tâche  ", "scripter", "tâche", true},
		{"not last line ignored", "⟢ handoff: a — b\nplus de texte après", "", "", false},
		{"no marker", "juste une réponse normale", "", "", false},
		{"empty agent", "⟢ handoff:  — tâche", "", "", false},
		{"empty task", "⟢ handoff: scripter — ", "", "", false},
		{"missing separator", "⟢ handoff: scripter finir", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, tk, ok := parseHandoff(c.reply)
			if ok != c.ok || a != c.agent || tk != c.task {
				t.Fatalf("parseHandoff(%q) = (%q,%q,%v), want (%q,%q,%v)",
					c.reply, a, tk, ok, c.agent, c.task, c.ok)
			}
		})
	}
}
```

- [ ] **Step 2 : Lancer — échec attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestParseHandoff 2>&1 | tail`
Expected: échec de compilation (`parseHandoff` inexistant).

- [ ] **Step 3 : Implémenter**

`core/host/handoff.go` :

```go
package host

import "strings"

// handoffMarker prefixes the single trailer line an agent uses to signal a relay.
const handoffMarker = "⟢ handoff:"

// parseHandoff extracts a handoff intent from an agent reply. The signal is a
// single trailer line, "⟢ handoff: <agent> — <task>", that MUST be the reply's
// last non-empty line — a half-formed or mid-reply marker is never guessed at.
// Returns ok=false when absent or malformed (empty agent/task, missing separator).
func parseHandoff(reply string) (agent, task string, ok bool) {
	lines := strings.Split(strings.TrimRight(reply, "\n \t"), "\n")
	if len(lines) == 0 {
		return "", "", false
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(last, handoffMarker) {
		return "", "", false
	}
	body := strings.TrimSpace(strings.TrimPrefix(last, handoffMarker))
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
```

- [ ] **Step 4 : Lancer — succès attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestParseHandoff 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5 : gofmt + commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/host/handoff.go core/host/handoff_test.go
git add core/host/handoff.go core/host/handoff_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): parseHandoff pure trailer parser

Co-Authored-By: Claude <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc"
```

---

### Task 7 : `Coordinator` impl — résolution profil + garde commité + create + seed

**Files:**
- Create: `herrscher/core/host/coordinator.go`
- Test: `herrscher/core/host/coordinator_test.go`

**Interfaces:**
- Consumes : `contracts.Coordinator`, `contracts.HandoffRequest`, `contracts.CreateSession`
  (Task 1) ; `agent.Agent` (`core/internal/agent`) ; `state.Session`
  (`core/internal/state`).
- Produces : `type coordinator struct{…}` implémentant `contracts.Coordinator` ;
  `func newCoordinator(creator sessionCreator, agents agentLookup, wt cleanBrancher, sessions
  sessionLister, seed func(string, string) bool) *coordinator`. Petits ports internes :
  - `type sessionCreator interface { Create(context.Context, contracts.CreateSession) (string, error) }` (satisfait par `*hub`)
  - `type agentLookup interface { Get(name string) (agent.Agent, bool) }` (satisfait par `*agent.Store`)
  - `type cleanBrancher interface { IsCleanAt(path string) (bool, error); Branch(name string) string }` (satisfait par `*worktree.Worktreer`)
  - `type sessionLister interface { SnapshotSessions() []state.Session }` (satisfait par `*state.State`)

- [ ] **Step 1 : Écrire les tests qui échouent (fakes, aucun git réel)**

```go
package host

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type fakeCreator struct {
	spec contracts.CreateSession
	err  error
}

func (f *fakeCreator) Create(_ context.Context, s contracts.CreateSession) (string, error) {
	f.spec = s
	return s.Name, f.err
}

type fakeAgents struct{ known map[string]bool }

func (f fakeAgents) Get(name string) (agent.Agent, bool) {
	return agent.Agent{}, f.known[name]
}

type fakeWTC struct{ clean bool; err error }

func (f fakeWTC) IsCleanAt(string) (bool, error) { return f.clean, f.err }
func (f fakeWTC) Branch(name string) string      { return "session/" + name }

type fakeSessions struct{ list []state.Session }

func (f fakeSessions) SnapshotSessions() []state.Session { return f.list }

func newTestCoordinator(cr *fakeCreator, known []string, clean bool, sessions []state.Session, seeded *[]string) *coordinator {
	km := map[string]bool{}
	for _, k := range known {
		km[k] = true
	}
	seed := func(sess, task string) bool { *seeded = append(*seeded, sess+"|"+task); return true }
	return newCoordinator(cr, fakeAgents{known: km}, fakeWTC{clean: clean}, fakeSessions{list: sessions}, seed)
}

func TestHandoffCreatesBOnABranchAndSeeds(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true,
		[]state.Session{{Name: "alpha", Project: "game", Worktree: "/wt/alpha"}}, &seeded)

	name, err := c.Handoff(context.Background(), contracts.HandoffRequest{
		FromSession: "alpha", ToAgent: "scripter", Task: "finir le module",
	})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if cr.spec.Base != "session/alpha" {
		t.Fatalf("B not based on A's branch: %q", cr.spec.Base)
	}
	if cr.spec.Agent != "scripter" || cr.spec.Project != "game" {
		t.Fatalf("bad spec: %+v", cr.spec)
	}
	if len(seeded) != 1 || !strings.HasSuffix(seeded[0], "|finir le module") {
		t.Fatalf("task not seeded: %v", seeded)
	}
	if seeded[0] != name+"|finir le module" {
		t.Fatalf("seed targeted wrong session: %v (name=%s)", seeded, name)
	}
}

func TestHandoffUnknownAgent(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, nil, true,
		[]state.Session{{Name: "alpha", Worktree: "/wt/alpha"}}, &seeded)
	if _, err := c.Handoff(context.Background(), contracts.HandoffRequest{
		FromSession: "alpha", ToAgent: "ghost", Task: "x",
	}); err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if cr.spec.Name != "" {
		t.Fatal("no session should have been created")
	}
}

func TestHandoffDirtySourceRefused(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, false, // dirty
		[]state.Session{{Name: "alpha", Worktree: "/wt/alpha"}}, &seeded)
	if _, err := c.Handoff(context.Background(), contracts.HandoffRequest{
		FromSession: "alpha", ToAgent: "scripter", Task: "x",
	}); err == nil {
		t.Fatal("expected refusal for dirty source worktree")
	}
	if cr.spec.Name != "" {
		t.Fatal("no session should have been created")
	}
}

func TestHandoffUnknownSource(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true, nil, &seeded)
	if _, err := c.Handoff(context.Background(), contracts.HandoffRequest{
		FromSession: "ghost", ToAgent: "scripter", Task: "x",
	}); err == nil {
		t.Fatal("expected error for missing source session")
	}
}
```

- [ ] **Step 2 : Lancer — échec attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestHandoff 2>&1 | tail`
Expected: échec de compilation (`coordinator`/`newCoordinator` inexistants).

- [ ] **Step 3 : Implémenter**

`core/host/coordinator.go` :

```go
package host

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// Small ports the coordinator depends on, each satisfied by an existing host
// component (*hub, *agent.Store, *worktree.Worktreer, *state.State). Kept tiny so
// Handoff is testable with fakes and no real git/session machinery.
type sessionCreator interface {
	Create(context.Context, contracts.CreateSession) (string, error)
}
type agentLookup interface {
	Get(name string) (agent.Agent, bool)
}
type cleanBrancher interface {
	IsCleanAt(path string) (bool, error)
	Branch(name string) string
}
type sessionLister interface {
	SnapshotSessions() []state.Session
}

// coordinator implements contracts.Coordinator at the host layer: it sees every
// session (sessionLister) and drives the hub (sessionCreator), which the
// per-session Orchestrator plugin cannot. Model O: the agent only signals; here
// is where the decision and execution live.
type coordinator struct {
	creator  sessionCreator
	agents   agentLookup
	wt       cleanBrancher
	sessions sessionLister
	seed     func(session, task string) bool
}

func newCoordinator(creator sessionCreator, agents agentLookup, wt cleanBrancher, sessions sessionLister, seed func(string, string) bool) *coordinator {
	return &coordinator{creator: creator, agents: agents, wt: wt, sessions: sessions, seed: seed}
}

// Handoff creates B continuing FromSession's committed work, seeds Task as B's
// opening turn, and returns B's name. Order matters: every guard runs before any
// side effect, so a rejected handoff leaves nothing partial.
func (c *coordinator) Handoff(ctx context.Context, req contracts.HandoffRequest) (string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return "", fmt.Errorf("handoff: unknown agent %q", req.ToAgent)
	}
	var from *state.Session
	for i := range c.sessions.SnapshotSessions() {
		if s := c.sessions.SnapshotSessions()[i]; s.Name == req.FromSession {
			from = &s
			break
		}
	}
	if from == nil {
		return "", fmt.Errorf("handoff: source session %q not found", req.FromSession)
	}
	if from.Worktree == "" {
		return "", fmt.Errorf("handoff: source session %q has no isolated worktree to continue", req.FromSession)
	}
	clean, err := c.wt.IsCleanAt(from.Worktree)
	if err != nil {
		return "", fmt.Errorf("handoff: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("handoff refused: session %q has uncommitted changes — commit first", req.FromSession)
	}

	bName := req.FromSession + "-" + req.ToAgent
	if _, err := c.creator.Create(ctx, contracts.CreateSession{
		Name:    bName,
		Project: from.Project,
		Agent:   req.ToAgent,
		Base:    c.wt.Branch(req.FromSession),
	}); err != nil {
		return "", fmt.Errorf("handoff: create %q: %w", bName, err)
	}
	if !c.seedWithRetry(bName, req.Task) {
		return bName, fmt.Errorf("handoff: session %q created but seeding timed out", bName)
	}
	return bName, nil
}
```

Ajouter `seedWithRetry` dans le même fichier (le driver de B se `registerDriver` de façon
asynchrone après `Create` → attente bornée) :

```go
// seedWithRetry waits for B's driver to register (goLive starts RunSession in a
// goroutine) before enqueuing the task, bounded so a never-arriving session
// surfaces as a timeout instead of hanging.
func (c *coordinator) seedWithRetry(session, task string) bool {
	for i := 0; i < seedAttempts; i++ {
		if c.seed(session, task) {
			return true
		}
		time.Sleep(seedBackoff)
	}
	return false
}
```

Ajouter en tête du fichier les constantes + l'import `time` :

```go
const (
	seedAttempts = 50
	seedBackoff  = 100 * time.Millisecond
)
```
(les tests injectent un `seed` qui renvoie `true` du premier coup → aucun sleep réel.)

- [ ] **Step 4 : Lancer — succès attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestHandoff 2>&1 | tail`
Expected: PASS (les 4 cas).

- [ ] **Step 5 : gofmt + commit**

```bash
cd /home/shan/dev/herrscher
gofmt -w core/host/coordinator.go core/host/coordinator_test.go
git add core/host/coordinator.go core/host/coordinator_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): Coordinator.Handoff — resolve/guard/create-on-branch/seed

Co-Authored-By: Claude <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc"
```

---

### Task 8 : Câblage — construire le Coordinator au boot, l'accrocher dans le driver

**Files:**
- Modify: `herrscher/core/host/cli.go` (`buildRegistry` expose wt + agents)
- Modify: `herrscher/core/host/serve.go` (construire le coordinator, le passer au hub)
- Modify: `herrscher/core/host/hub.go` (champ `coordinator`, passer à `RunSession` dans `goLive`)
- Modify: `herrscher/core/host/turnloop.go` (`sessionDriver.coordinator` + `name` ; hook dans `awaitTurn` ; `RunSession`/`newSessionDriver` params)
- Test: `herrscher/core/host/turnloop_test.go` (intégration reply→handoff)

**Interfaces:**
- Consumes : `newCoordinator` (Task 7), `parseHandoff` (Task 6), `contracts.Coordinator` (Task 1).

- [ ] **Step 1 : Écrire le test d'intégration qui échoue**

Le test pilote un `sessionDriver` avec un fake `contracts.Coordinator` et vérifie qu'une réponse
portant le trailer déclenche `Handoff` (avec `FromSession` = nom du driver), et qu'une réponse
sans trailer ne déclenche rien. On appelle le hook via `awaitTurn` en poussant des events sur le
canal `from`.

```go
type recordingCoord struct{ reqs []contracts.HandoffRequest }

func (r *recordingCoord) Handoff(_ context.Context, req contracts.HandoffRequest) (string, error) {
	r.reqs = append(r.reqs, req)
	return req.ToAgent + "-s", nil
}

func TestDriverInvokesCoordinatorOnHandoffTrailer(t *testing.T) {
	from := make(chan contracts.Event, 2)
	d := newSessionDriver("alpha", nil, make(chan contracts.Event, 1), from)
	rc := &recordingCoord{}
	d.coordinator = rc

	from <- contracts.Event{T: "reply", Done: true,
		Text: "fait.\n⟢ handoff: scripter — finir le module"}
	if ok := d.awaitTurn(context.Background()); !ok {
		t.Fatal("awaitTurn should complete on reply{done}")
	}
	if len(rc.reqs) != 1 {
		t.Fatalf("expected 1 handoff, got %d", len(rc.reqs))
	}
	got := rc.reqs[0]
	if got.FromSession != "alpha" || got.ToAgent != "scripter" || got.Task != "finir le module" {
		t.Fatalf("bad handoff request: %+v", got)
	}
}

func TestDriverNoHandoffWithoutTrailer(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("alpha", nil, make(chan contracts.Event, 1), from)
	rc := &recordingCoord{}
	d.coordinator = rc
	from <- contracts.Event{T: "reply", Done: true, Text: "réponse normale"}
	_ = d.awaitTurn(context.Background())
	if len(rc.reqs) != 0 {
		t.Fatalf("no handoff expected, got %d", len(rc.reqs))
	}
}
```
(Si `newSessionDriver` ne stocke pas déjà `name`, l'ajouter comme champ dans le même Step 3.)

- [ ] **Step 2 : Lancer — échec attendu**

Run: `cd /home/shan/dev/herrscher && go test ./core/host/ -run TestDriver 2>&1 | tail`
Expected: échec de compilation (`sessionDriver.coordinator`/`name` inexistants).

- [ ] **Step 3 : Accrocher le hook dans le driver**

Dans `turnloop.go` : ajouter les champs `name string` et `coordinator contracts.Coordinator`
au struct `sessionDriver` ; les initialiser dans `newSessionDriver` (le `name` est déjà un
paramètre ; ajouter un paramètre `coord contracts.Coordinator` en dernière position et le
stocker). Puis, dans `awaitTurn`, au point `if e.T == "reply" && e.Done {` :

```go
			if e.T == "reply" && e.Done {
				d.metrics.TurnCompleted()
				d.maybeHandoff(ctx, e.Text)
				return true
			}
```

Ajouter la méthode (même fichier ou `handoff.go`) :

```go
// maybeHandoff runs the Model-O signal check after a completed turn: parse the
// reply's trailer and, on a valid marker, hand the decision to the Coordinator.
// A malformed marker is ignored; a coordinator refusal (unknown agent, dirty
// source, create failure) is surfaced back into A's channel as a status event —
// never a silent half-handoff.
func (d *sessionDriver) maybeHandoff(ctx context.Context, reply string) {
	if d.coordinator == nil {
		return
	}
	toAgent, task, ok := parseHandoff(reply)
	if !ok {
		return
	}
	if _, err := d.coordinator.Handoff(ctx, contracts.HandoffRequest{
		FromSession: d.name, ToAgent: toAgent, Task: task,
	}); err != nil {
		d.fanOut(ctx, contracts.Event{T: "status", Text: "handoff refusé: " + err.Error()})
	}
}
```

Mettre à jour la signature de `RunSession` pour accepter `coord contracts.Coordinator` et le
passer à `newSessionDriver`. Adapter l'appel dans `hub.goLive`.

- [ ] **Step 4 : Câbler la construction au boot**

`cli.go` — faire remonter `wt` et `agents` hors de `buildRegistry`. Le plus simple sans casser
`NewRegistry` : renvoyer un petit struct de deps. Modifier `buildRegistry` pour retourner
`(*cli.Registry, hostDeps, error)` où `type hostDeps struct { wt *worktree.Worktreer; agents
*agent.Store }`, et adapter les deux appelants (`NewRegistry` ignore les deps avec `_`).

`serve.go` — après `hb := newHub(...)`, construire le coordinator et le donner au hub :

```go
	reg, deps, err := buildRegistry(ctx, Deps{Admin: adminForHome(gws, st.Home)}, o, st, sup, instID)
	if err != nil {
		return fmt.Errorf("build command registry: %w", err)
	}
	hb := newHub(ctx, st, sup, gws, partDir, reg, h.Metrics())
	hb.coordinator = newCoordinator(hb, deps.agents, deps.wt, st, Seed)
```

`hub.go` — ajouter le champ `coordinator contracts.Coordinator` au struct `hub`, et dans
`goLive`, passer `h.coordinator` en dernier argument de `RunSession` :

```go
	go RunSession(sctx, sess.Name, sess.ChannelID, bound, acc, state.ParticipantsPath(h.partDir, sess.Name), h.metrics, h.coordinator)
```
(`hb.coordinator` est posé en `serve.go` avant la boucle `goLive`, donc non-nil au wiring ;
il reste `nil` pour l'opérateur CLI court-vécu, où `maybeHandoff` no-op proprement.)

- [ ] **Step 5 : Lancer toute la suite du repo**

Run: `cd /home/shan/dev/herrscher && gofmt -l core/ && go build ./... && go test ./... 2>&1 | tail -20`
Expected: `gofmt -l` ne liste rien ; build OK ; tous les tests verts.

- [ ] **Step 6 : Commit**

```bash
cd /home/shan/dev/herrscher
git add core/host/cli.go core/host/serve.go core/host/hub.go core/host/turnloop.go core/host/handoff.go core/host/turnloop_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(host): wire Coordinator into the turn loop (Model O handoff hook)

Co-Authored-By: Claude <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JNTgW9tZrbKqwETThxEZzc"
```

---

## Self-Review

- **Couverture spec :**
  - D1 (Modèle O, hook après le tour, pas de MCP, factory Orchestrator inchangée) → Tasks 6/8 ✓
  - D2 (continuation de branche via `Base`, `session/<B> ⊂ session/<A>`, garde commité) → Tasks 1/2/3/4/7 ✓
  - D3 (Task seede le tour d'ouverture via la queue du driver) → Tasks 5/7 ✓
  - D4 (trailer unique, malformé → ignoré ; inconnu/sale → refus explicite remonté) → Tasks 6/7/8 ✓
  - D5 (`HandoffRequest` + port `Coordinator` réutilisables) → Task 1 ✓
  - Gestion d'erreur (agent inconnu / arbre sale / create échoue → rien de partiel) → Task 7 ✓
  - Tests sur fakes, pas de backend LLM → Tasks 6/7/8 ✓
- **Placeholders :** aucun — code complet à chaque step ; les seuls points « adapter au harnais
  existant » (fake WT réel, helper de test create, seam de capture du hub) sont explicités avec
  repli honnête (omettre le test si non isolable, couvert par l'intégration Task 8).
- **Cohérence des types :** `Coordinator.Handoff(ctx, HandoffRequest) (string, error)`,
  `CreateSession.Base string`, `worktrees.Create(repo,name,base)`,
  `Worktreer.IsCleanAt(path)`, `Seed(session,task) bool`, `parseHandoff(reply)
  (agent,task string, ok bool)`, `newCoordinator(creator, agents, wt, sessions, seed)` —
  identiques entre définition (Tasks 1/2/5/6/7) et usage (Tasks 3/4/7/8).
- **Ordre / dépendances :** Task 1 (contracts) d'abord ; Task 2+3 partagent un build vert
  (commit unique en fin de Task 3) ; 4/5/6 indépendantes ; 7 dépend de 1 ; 8 assemble 1/5/6/7.
```
