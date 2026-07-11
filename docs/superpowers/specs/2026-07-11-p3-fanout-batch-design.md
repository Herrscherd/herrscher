# P3 — Coordination multi-agents : fan-out par lot (le superviseur déclare sa flotte)

> Sixième tranche du chantier « coordination multi-agents » dans le châssis
> herrscher, par-dessus le socle `Coordinator` (handoff → delegate + result-back →
> fan-out + join → join+merge → scellage). Feature **générique OSS** (aucune
> connaissance métier Roblox/Neublox). Donne au lead la capacité de **spawner
> toute une cohorte en un seul signal**, au lieu d'un worker par tour.
> Date : 2026-07-11 · Statut : design validé.

## Contexte vérifié (source)

Les cinq premières tranches (mergées/poussées ou merge-ready sur
`feat/p3-join-merge`) :

- **Handoff** (relais A→B, one-shot) — `contracts 4ef2f6c`, `herrscher a66b844`.
- **Delegate + result-back** (round-trip L↔W, 1 worker) — `contracts 306d3c4`.
- **Fan-out + join** (suivi `done/total`) — `herrscher` merge 2026-07-11.
- **Join+merge** (agrégation réelle) — `contracts 195587d`, `feat/p3-join-merge`.
- **Scellage** (barrière déterministe) — `contracts 03c4996`, `feat/p3-join-merge`.

État du code, Modèle O (host-driven : l'agent *signale* via un trailer en
dernière ligne, le host valide, le `coordinator` hub-level exécute) :

- `coordinator` (`core/host/coordinator.go`) : `reported` + `expected` sous `mu`,
  méthodes `Handoff`/`Delegate`/`Report`/`Merge`/`Seal`, helper `spawn` (gardes →
  probe de nom → `Create` → `seedWithRetry`, rollback sur timeout), `forget` (purge).
- `spawn(ctx, from, toAgent, task, parent)` (`coordinator.go:91`) crée UN worker
  enfant de `from` sur `session/<from>`, seed la tâche, parent enregistré.
- `Delegate` (`coordinator.go:159`) = `Get(agent)` + `findSession(lead)` + `spawn`
  avec `parent = lead`. **Un seul worker par appel.**
- `maybeCoordinate` (`core/host/turnloop.go:288`) dispatche
  `parseDone → parseDelegate → parseSeal → parseMerge → parseHandoff`, **un seul
  trailer par tour** (premier match gagne).
- Trailers (`core/host/handoff.go`) : `⟢ done:`, `⟢ delegate:`, `⟢ seal:`,
  `⟢ merge:`, `⟢ handoff:`. `parseTrailer` isole la dernière ligne ;
  `splitAgentTask` splitte `<agent> — <task>` sur **l'em-dash `—`** (les deux non
  vides).

## Le problème (fan-out manuel, un-à-un)

Le *join* suit une cohorte, mais le *fan-out* est resté manuel : `⟢ delegate`
spawne **un** worker, et `maybeCoordinate` ne traite **qu'un trailer par tour**.
Pour bâtir une cohorte de 5, le lead émet 5 `⟢ delegate` sur 5 tours — lent et
verbeux. Il manque la primitive symétrique du join : **déclarer toute la flotte en
un seul signal**.

## Ce que cette tranche fait — et ne fait pas

Le lead émet `⟢ fanout: <agent> — <tâche1> ;; <tâche2> ;; …`. Le coordinator
spawne **un worker par tâche** (tous enfants du lead, sur son tip committé), en une
opération, et **scelle la cohorte à la taille réelle** post-spawn — le superviseur
qui déclare sa flotte obtient la barrière déterministe (tranche 5) gratuitement.

**Hors périmètre (délibérément, tranche suivante) :**
- **Routage / décision LLM** — *quoi* déléguer et *à qui* reste composé à la main
  par le lead dans le trailer ; aucune inférence. C'est la tranche 7.
- **Tâches multi-agents dans un même lot** — un `fanout` = un seul agent, N tâches
  (garde le parsing et la garde `Get` triviaux). Un lead voulant des agents
  distincts émet plusieurs `fanout` (chacun re-scelle, last-wins).
- **Rollback transactionnel du lot** — un échec partiel laisse les workers déjà
  spawnés vivants (voir cas limites) ; on ne détruit pas du travail réel.

## Décisions (arbitrées en brainstorm)

1. **`FanOut` est une méthode de port** (touche `herrscher-contracts`). Chaque
   trailer déclenché par l'agent mappe une méthode du port `Coordinator`
   (`done→Report`, `delegate→Delegate`, `seal→Seal`, `merge→Merge`) ; `fanout→FanOut`
   suit la règle. Réutilise `spawn` en interne (pas de duplication).
2. **Gardes en amont, une seule fois** : agent connu (un seul `Get` — toutes les
   tâches partagent l'agent), lead connu. La garde lead-clean vit déjà dans `spawn`
   (première itération la vérifie) ; on ne la duplique pas — mais comme un lead sale
   fait échouer le tout premier `spawn`, aucun worker n'est créé (échec propre à 0).
3. **Auto-scellage à la flotte réelle** : après le lot, `FanOut` pose
   `expected[lead] = (cohorte préexistante au snapshot initial) + len(spawned)` sous
   le verrou `mu` de `Seal` (même map `expected`). Sur échec partiel, scelle au
   nombre **réellement** spawné (cohérence barrière = réalité). Re-seal last-wins
   gère les `fanout` successifs.
4. **Échec partiel non destructif** : si un `spawn` échoue en cours de lot (ex. seed
   timeout, plus de nom libre), on **arrête, scelle au spawné, et retourne les noms
   déjà spawnés + l'erreur**. Pas de rollback : ces sessions sont réelles et
   committées. Le lead est informé via status.
5. **Format `;;`** comme séparateur de tâches (improbable en prose ; l'em-dash reste
   le séparateur agent/tâches). `parseFanOut` : split premier `—` → agent + reste ;
   split reste sur `;;` → tâches (trim, drop vides) ; exige agent non vide + ≥1 tâche.

## Architecture

Le fan-out vit dans le `coordinator` (couche host, Modèle O), avec une extension
du port. Aucune nouvelle donnée d'état : réutilise `reported`/`expected`/`spawn`.

Port (`herrscher-contracts`, `Coordinator`) — ajout :

```go
type FanOutRequest struct {
    FromSession string   // le lead
    ToAgent     string   // l'agent commun à toutes les tâches
    Tasks       []string // une tâche par worker (≥ 1)
}
// méthode ajoutée à l'interface Coordinator :
FanOut(ctx context.Context, req FanOutRequest) (spawned []string, err error)
```

Coordinator (`core/host/coordinator.go`) — méthode `FanOut`, zéro nouveau champ.

## Flux de données

Le lead termine un tour par `⟢ fanout: <agent> — <t1> ;; <t2> ;; …`. Nouveau
parser `parseFanOut` inséré dans `maybeCoordinate` :
`parseDone → parseDelegate → parseFanOut → parseSeal → parseMerge → parseHandoff`
(markers distincts, ordre sans collision ; fanout avant seal/merge, symétrique de
delegate).

`FanOut(ctx, {FromSession, ToAgent, Tasks})` :

1. **Agent connu** — `agents.Get(ToAgent)`. Sinon `fanout: unknown agent %q`.
2. **Lead connu** — `findSession(FromSession)`. Sinon `fanout: lead %q not found`.
3. **≥ 1 tâche** — `len(Tasks) > 0`. Sinon `fanout: no tasks`. (Le parser garantit
   déjà ≥1, mais le port se défend seul.)
4. Pour chaque tâche : `spawn(ctx, lead, ToAgent, task, lead.Name)`. Accumule le nom
   spawné. Au **premier** `spawn` en erreur : arrêter la boucle, mémoriser l'erreur.
5. **Sceller** : `expected[FromSession] = (enfants du lead dans le snapshot initial)
   + len(spawned)` sous `mu` — compter la cohorte préexistante sur le **snapshot pris
   à l'étape 2** (pas un re-snapshot, qui dépendrait de la propagation de `Create`),
   plus les workers réellement spawnés. Sauf si zéro spawné (rien à sceller ;
   l'erreur remonte telle quelle).
6. Retour `(spawned, err)` — `err` non nil si un spawn a échoué (partiel).

Dispatch (`turnloop.go`), status fan-out :
- succès → `"cohorte lancée : <N> workers (<n1>, <n2>, …)"`.
- partiel/échec → `"fanout partiel: <spawnés> lancés puis <err>"` (les noms spawnés
  restent utiles au lead).

## Gestion d'erreur & cas limites

- **Lead sale** : le premier `spawn` échoue sur sa garde `IsCleanAt` interne → 0
  worker, aucun scellage, erreur `"…uncommitted changes — commit first"` remontée.
  Pas de garde clean dupliquée dans `FanOut` (DRY ; `spawn` la porte déjà).
- **Échec partiel (un `spawn` échoue sur la tâche k, ex. `Create` en erreur)** :
  workers 1..k-1 vivants, scellés à leur compte réel, k et suivants non tentés. Le
  lead voit `"fanout partiel"` et décide (re-fanout des restantes, merge, etc.).
  Aucune corruption.
- **Auto-seal vs cohorte préexistante** : si le lead avait déjà des enfants (delegate
  antérieur), le compte post-spawn les inclut → sceller au total est correct
  (garde `Seal` `N >= cohorte` trivialement satisfaite : on scelle À la cohorte).
- **Re-fanout** : deuxième `fanout` ajoute des enfants et re-scelle au nouveau total
  (last-wins). Idempotent si mêmes tâches ? Non — `spawn` probe des noms neufs, donc
  un re-fanout des mêmes tâches crée de NOUVEAUX workers. C'est au lead de ne pas
  re-émettre (même contrat que delegate). Documenté, non corrigé (YAGNI).
- **Tâche unique** : `fanout` à une tâche = un `delegate` + auto-seal à 1. Toléré.
- **Parser** : `⟢ fanout: agent —` (aucune tâche après l'em-dash) ou `⟢ fanout: — t`
  (agent vide) ou `⟢ fanout: agent — ;; ;;` (tâches toutes vides) → `ok=false`,
  ignoré comme tout trailer malformé.
- **Concurrence** : `FanOut` n'ajoute aucun état ; le scellage réutilise le verrou
  `mu` de `Seal`. Les `spawn` séquentiels (pas de goroutine) évitent toute course
  sur la création de sessions. Vérifiable au `-race`.

## Fichiers touchés

**herrscher-contracts :**
- Type `FanOutRequest` + méthode `FanOut` sur l'interface `Coordinator` + son test
  de compilation/port.

**herrscher :**
- `core/host/coordinator.go` — méthode `FanOut` (gardes + boucle `spawn` + auto-seal).
- `core/host/handoff.go` — `parseFanOut` + const `fanoutMarker = "⟢ fanout:"` +
  helper `splitAgentTasks` (em-dash puis `;;`).
- `core/host/turnloop.go` — `maybeCoordinate` insère `parseFanOut` dans le dispatch.
- Tests : `coordinator_test.go`, `handoff_test.go`, contracts `coordinator_test.go`.

## Tests (TDD)

**contracts :**
- Port : `FanOutRequest` + `FanOut` dans l'assertion de surface du `Coordinator`.

**parsing (`handoff_test.go`) :**
- `TestParseFanOut` — `alpha — t1 ;; t2 ;; t3` → `("alpha", ["t1","t2","t3"], true)` ;
  une tâche → `("alpha", ["t1"], true)` ; espaces autour de `;;` trimés ;
  tâches vides droppées (`a — t1 ;; ;; t2` → `["t1","t2"]`) ; agent vide → false ;
  aucune tâche (`a —` / `a — ;;`) → false ; marker absent → false.

**coordinator (`coordinator_test.go`, fakes existants) :**
- `TestFanOutSpawnsCohort` — 3 tâches, agent+lead connus → 3 workers créés enfants
  du lead, 3 tâches seedées, retour des 3 noms.
- `TestFanOutSealsToCohortSize` — après un fanout de 3 → `expected[lead] == 3` ;
  un Report suivant montre `(1/3)` déterministe.
- `TestFanOutUnknownAgent` — agent inconnu → erreur, 0 worker, 0 scellage.
- `TestFanOutLeadNotFound` — lead inconnu → erreur, 0 worker.
- `TestFanOutDirtyLeadSpawnsNone` — lead sale → premier spawn échoue, 0 worker,
  0 scellage, erreur clean remontée.
- `TestFanOutPartialSealsToSpawned` — le 2ᵉ spawn échoue (fake `Create` renvoie une
  erreur au 2ᵉ appel) → 1 worker vivant, `expected[lead] == 1`, erreur non nil,
  1 nom retourné.
- `TestFanOutIncludesPreexistingCohort` — 1 delegate préalable puis fanout de 2 →
  `expected[lead] == 3`.
- Les tests `Delegate`/`Seal`/`Report`/`Merge` existants restent verts.

**turnloop (`turnloop_test.go`) :**
- `TestDriverInvokesCoordinatorOnFanOutTrailer` — reply finissant par
  `⟢ fanout: alpha — a ;; b` → `coordinator.FanOut` appelé avec
  `{lead, "alpha", ["a","b"]}`, status `"cohorte lancée"`.

## Contraintes globales

- **Générique OSS** : zéro connaissance métier Roblox/Neublox dans le code ou les
  messages (« worker »/« lead »/« cohorte », pas de vocabulaire domaine).
- **`herrscher-contracts` touché** (assumé) : nouvelle méthode `FanOut` +
  `FanOutRequest` ; les autres signatures du port `Coordinator` inchangées.
- **CI verte des deux côtés** : `gofmt -l` propre, `go vet ./...` propre,
  `go build ./...`, `go test -race ./...` verts, `go mod tidy` laisse `go.mod`
  inchangé (herrscher ET contracts).
- **DRY** : `FanOut` réutilise `spawn` (création+seed+parent) et la logique de
  scellage de `Seal` (champ `expected`, verrou `mu`) — aucune duplication.
- **Concurrence sûre** : aucun nouvel état ; `expected` touché sous `mu` seulement.
- **1 tranche = 1 commit** logique par unité testable (port contracts, `parseFanOut`,
  `FanOut`+dispatch — regroupés selon l'atomicité de compilation Go).
</content>
</invoke>
