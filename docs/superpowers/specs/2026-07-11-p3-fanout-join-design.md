# P3 — Coordination multi-agents : fan-out + join (tranche verticale)

> Troisième tranche du chantier « coordination multi-agents » dans le châssis
> herrscher, par-dessus le socle `Coordinator` (handoff, puis delegate + result-back).
> Feature **générique OSS** (aucune connaissance métier Roblox/Neublox). Donne au lead
> la capacité de suivre une **cohorte** de workers parallèles jusqu'à complétion.
> Date : 2026-07-11 · Statut : livré (code mergé-ready, CI verte, 100 tests).

## Contexte vérifié (source)

Les deux premières tranches, mergées + poussées sur `origin/master` :

- **Handoff** (relais A→B, one-shot) — `contracts 4ef2f6c`, `herrscher a66b844`.
- **Delegate + result-back** (round-trip L↔W, 1 worker) — `contracts 306d3c4`,
  `herrscher` merge sur `master` (2026-07-11).

État actuel du code (`core/host/coordinator.go`), Modèle O (host-driven : l'agent
*signale* via un trailer en dernière ligne, le host valide, le `coordinator`
hub-level exécute) :

- `spawn(ctx, from, toAgent, task, parent)` — helper partagé : gardes (worktree
  non vide → propre → nom libre par probe) puis `Create` sur `Base=session/<from>`
  avec `Parent`, seed, rollback sur timeout.
- `Handoff` → `spawn(..., parent="")`. `Delegate` → `spawn(..., parent=lead)`.
- `Report(ctx, {FromSession, Summary})` — gardes (worker connu → a un parent →
  **commité** → parent vivant) puis seed au parent
  `"<W> a terminé sur session/<W> — <résumé>"`. **W reste vivant, pas de merge, pas
  de teardown.**
- `core/host/turnloop.go` `maybeCoordinate` dispatche `parseDone → parseDelegate →
  parseHandoff` (premier match gagne) ; une seule ligne trailer par tour.

## Ce que cette tranche fait — et ne fait pas

**Découverte structurante : le fan-out ne demande AUCUN code neuf.** Choisi
séquentiel, il *est* le `Delegate` déjà livré, émis N fois par le lead sur N tours.
Chaque worker démarre dès son spawn → parallélisme réel. Le lead sait combien il a
lancé. Le fan-out est donc entièrement une affaire de **join**.

Cette tranche livre **uniquement le join** : le suivi de progression de la cohorte,
côté lead, de façon déterministe (« 3/5 livrés », puis « tous les workers ont
livré »).

**Hors périmètre (délibérément, tranches suivantes) :**
- **Agrégation réelle** (merge des branches `session/<W>` des workers dans le lead) —
  c'est le job de l'agent-lead via git, ou une tranche « join+merge » ultérieure.
- **Barrière dure** (le host bloque le lead jusqu'à N) — on a choisi les arrivées
  incrémentales, pas de gel.
- **Scellage de cohorte** (le lead déclare N attendu) — on assume le best-effort.
- **Superviseur→workers** (boucle d'orchestration au-dessus) et **routage par
  jugement LLM** — tranches ultérieures, même `Coordinator`.

## Architecture

Le join vit **entièrement dans le `coordinator`** (couche host, Modèle O). **Aucun
changement dans `herrscher-contracts`. Aucun changement de persistance dans
`state.go`.** Le coordinator gagne un état en mémoire, propre au processus :

```go
reported map[string]map[string]bool // parent → { worker → true }
mu       sync.Mutex                  // Report appelé en concurrence (N tours workers)
```

Modèle « hybride » retenu (cf. décisions) : **arrivées incrémentales** comme
transport (chaque `Report` seed le lead dès qu'il arrive) **+ comptage léger** côté
host pour un signal « tous livrés » déterministe — sans barrière qui cacherait les
résultats intermédiaires, et sans nouvelle API.

## Flux de données (le join enrichi)

Quand un worker W émet `⟢ done: <résumé>`, `Report` :

1. **Gardes inchangées** — worker connu, a un parent, worktree commité, parent
   vivant. Un échec de garde ne compte ni ne seed rien (comportement actuel).
2. Sous `mu` : `reported[P][W] = true` (P = `from.Parent`). Idempotent.
3. Depuis le snapshot d'état déjà pris par `Report` :
   `total = len({ s ∈ sessions : s.Parent == P })` — les frères de la cohorte.
4. Sous `mu` : `done = len(reported[P])`.
5. Message seedé au lead :
   - `"<W> a terminé sur session/<W> (<done>/<total>) — <résumé>"`
   - si `done >= total` : suffixe ` " — tous les workers ont livré"`.

**Best-effort assumé.** En fan-out séquentiel, si un worker livre avant que le lead
ait fini de distribuer toute la cohorte, `total` est momentanément petit et un « tous
livrés » prématuré peut apparaître. C'est un **indice** pour l'agent-lead, pas une
garantie du châssis ; un `delegate` ultérieur agrandit la cohorte et le `Report`
suivant relance le compte. Décision explicite : pas de scellage.

## Purge à la fermeture (`forget`)

Le coordinator expose `forget(name string)`, appelée quand une session meurt :
**`hub.goDead(name)`** (`core/host/hub.go:72`, vérifié — c'est le point de mort de la
boucle `RunSession`). Le hub tient son coordinator via le type du port
(`coordinator contracts.Coordinator`) ; comme `forget` n'appartient **pas** au port
(elle est host-interne), `goDead` la déclenche via une petite interface host-interne
`forgetter interface { forget(string) }` + type-assert — **le port `contracts.Coordinator`
reste intact**. Sous `mu`, deux effets :

- `delete(reported, name)` — si `name` était un lead, sa cohorte est jetée
  (anti-fuite mémoire sur daemon longue durée).
- pour chaque parent P, `delete(reported[P], name)` — si `name` était un worker, il
  sort des livrés.

**Le second effet est une correction de justesse, pas du simple ménage** : sans lui,
un worker qui livre puis se ferme resterait compté dans `done` alors que `total`
(frères vivants du snapshot) a baissé → `done > total` et un « tous livrés » faux.
Purger à la fermeture garde l'invariant `done ≤ total`.

`delete` sur une clé absente est un no-op en Go : `forget` d'un nom inconnu ne
nécessite aucune garde.

## Gestion d'erreur & cas limites

- **Report concurrents** (N workers finissent ensemble) → tout accès à `reported`
  sous `mu`. `total` est lu via `SnapshotSessions` (déjà atomique) hors verrou ;
  l'incohérence transitoire est bénigne (best-effort).
- **Double report d'un même worker** → `reported[P][W]=true` idempotent, `done`
  n'avance pas deux fois. Possible car W reste vivant après un report.
- **Worker sans parent / parent mort / worker sale** → gardes existantes, rien n'est
  compté ni seedé.
- **`total == 0`** impossible lors d'un `Report` valide : le worker appelant est
  lui-même un frère avec `Parent == P`, donc `total ≥ 1`.
- **`forget` d'un nom absent** → no-op, aucune garde.

## Fichiers touchés (herrscher uniquement)

- `core/host/coordinator.go` — champs `reported`/`mu` (init dans `newCoordinator`),
  `Report` enrichi (étapes 2–5), nouvelle méthode `forget`.
- `core/host/hub.go` — `goDead` appelle `forget(name)` via l'interface host-interne
  `forgetter` (type-assert sur `h.coordinator`). **Seul vrai travail d'intégration.**
- `core/host/coordinator_test.go` — tests ci-dessous.

## Tests (TDD)

- `TestReportCountsSiblingProgress` — 3 frères (même `Parent`), reports successifs →
  seeds `(1/3)`, `(2/3)`, `(3/3) — tous les workers ont livré`.
- `TestReportAllDoneSuffixOnlyOnLast` — le suffixe « tous les workers ont livré »
  n'apparaît qu'au dernier report, pas avant.
- `TestReportDoubleReportIdempotent` — W livre deux fois → `done` n'avance qu'une
  fois.
- `TestForgetPurgesLeadCohort` — `forget(lead)` vide `reported[lead]`.
- `TestForgetRemovesWorkerKeepsCountConsistent` — worker livre puis `forget(worker)`
  → `done` redescend, pas de faux « tous livrés » au report d'un frère suivant.
- Les tests `Report` existants (garde sale, sans parent, parent mort) restent verts.

## Contraintes globales

- **Générique OSS** : zéro connaissance métier Roblox/Neublox dans le code ou les
  messages (« worker »/« lead », pas de vocabulaire domaine).
- **Aucun changement `herrscher-contracts`** : le port `Coordinator` est inchangé,
  `Report` garde sa signature `(ctx, ReportRequest) (parent string, err error)`.
- **CI verte** : `gofmt -l` propre, `go vet ./...` propre, `go build ./...`,
  `go test -race ./...` verts, `go mod tidy` laisse `go.mod` inchangé.
- **Concurrence sûre** : `reported` n'est jamais touché hors `mu` (vérifiable au
  `-race`).
- **1 tranche = 1 commit** logique par unité testable (le champ+init, `Report`
  enrichi, `forget`, le point d'appel — regroupés selon l'atomicité de compilation
  Go par package).
