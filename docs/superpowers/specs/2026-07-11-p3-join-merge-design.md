# P3 — Coordination multi-agents : join + merge (agrégation réelle)

> Quatrième tranche du chantier « coordination multi-agents » dans le châssis
> herrscher, par-dessus le socle `Coordinator` (handoff → delegate + result-back →
> fan-out + join). Feature **générique OSS** (aucune connaissance métier
> Roblox/Neublox). Donne au lead la capacité d'**agréger réellement** la branche
> d'un worker dans son propre worktree — ferme la boucle *déléguer → suivre →
> récupérer*.
> Date : 2026-07-11 · Statut : design validé.

## Contexte vérifié (source)

Les trois premières tranches, mergées + poussées sur `origin/master` :

- **Handoff** (relais A→B, one-shot) — `contracts 4ef2f6c`, `herrscher a66b844`.
- **Delegate + result-back** (round-trip L↔W, 1 worker) — `contracts 306d3c4`.
- **Fan-out + join** (suivi de cohorte `done/total`) — `herrscher` merge 2026-07-11,
  contracts inchangé (join = état host-interne).

État du code (`core/host/coordinator.go`), Modèle O (host-driven : l'agent
*signale* via un trailer en dernière ligne, le host valide, le `coordinator`
hub-level exécute) :

- `spawn(ctx, from, toAgent, task, parent)` — helper partagé (gardes → `Create`
  sur `Base=session/<from>` avec `Parent` → seed → rollback sur timeout).
- `Handoff` → `spawn(..., parent="")`. `Delegate` → `spawn(..., parent=lead)`.
- `Report(ctx, {FromSession, Summary})` — gardes puis seed au parent du compte
  `done/total`. **W reste vivant, pas de merge, pas de teardown.**
- `reported map[string]map[string]bool` + `mu` — état join en mémoire ; `forget`
  purge à la fermeture (`hub.goDead`).
- `core/host/turnloop.go` `maybeCoordinate` dispatche
  `parseDone → parseDelegate → parseHandoff` (premier match gagne).

Primitive git disponible (`core/internal/worktree/worktree.go`) : `Create`,
`IsCleanAt`, `BranchExistsAt`, `Remove`, `Branch`, `Path`. **Aucune primitive de
merge n'existe** — le join actuel ne livre qu'un *compteur*, jamais ne touche au
git du lead.

## Ce que cette tranche fait — et ne fait pas

Le join (tranche précédente) dit au lead *qui a livré* (`3/5`). Il ne récupère
rien : agréger les branches `session/<W>` reste, aujourd'hui, le travail manuel de
l'agent-lead via git. Cette tranche donne au châssis la primitive manquante :
**`Merge`, lead-initiée**, qui agrège un worker dans le worktree du lead via un vrai
`git merge`, abort propre sur conflit.

**Hors périmètre (délibérément, tranches suivantes) :**
- **Résolution de conflit assistée** — sur conflit, le châssis rend la main propre au
  lead avec un diagnostic ; la résolution fine reste le job de l'agent-lead.
- **Teardown du worker au merge** — W reste vivant (voir Décisions). Le cycle de vie
  reste piloté séparément (trailer `⟢ done`/close).
- **Merge multi-workers en une passe / octopus** — un worker par trailer.
- **Scellage de cohorte, superviseur→workers, routage LLM** — tranches ultérieures.

## Décisions (arbitrées en brainstorm)

1. **Merge piloté par le lead** (nouveau trailer `⟢ merge: <worker>`), pas
   automatique au Report. Respecte la séparation *suivre* (join) vs *récupérer*
   (merge) ; le lead garde le contrôle du *quand* et du *quoi*.
2. **Conflit → abort + diagnostic**, worktree lead toujours laissé propre. Pas de
   marqueurs de conflit laissés traîner (casserait les gardes `IsCleanAt` des autres
   primitives) ; pas de `--ff-only` (échouerait dès que le lead a commité après le
   delegate — cas courant).
3. **W reste vivant après merge**, intact. Le merge est agrégation pure (comme le
   Report est delivery pure). Autorise un re-merge après nouvelle livraison.
4. **Zéro effet sur `reported`/join.** Le compteur suit la *livraison*, pas le
   *merge* ; ce sont deux préoccupations orthogonales. Un worker peut être
   livré-mais-pas-mergé. Pas de nouvel état « merged » ce tour-ci (YAGNI).

## Architecture

Le merge vit dans le `coordinator` (couche host, Modèle O). Contrairement au join,
**cette tranche touche `herrscher-contracts`** : `Merge` est une capacité *exposée*
(nouvelle méthode de port + type `MergeRequest`), pas un détail host-interne comme
`forget`. Plus une nouvelle méthode git `worktree.MergeInto`.

Port (`herrscher-contracts`, `Coordinator`) — ajout :

```go
type MergeRequest struct {
    FromSession string // le lead qui déclenche
    Worker      string // le worker dont session/<Worker> est agrégé
}
// méthode ajoutée à l'interface Coordinator :
Merge(ctx context.Context, req MergeRequest) (lead string, err error)
```

Git (`core/internal/worktree`) — ajout :

```go
// MergeInto tente `git -C leadPath merge --no-edit <branch>`. Retour :
//   (merged=true,  nil,      nil)  — merge réussi (commit créé) ou déjà à jour
//   (merged=false, conflicts, nil) — conflit : `git merge --abort` exécuté,
//                                     worktree leadPath restauré propre
//   (false, nil, err)               — autre échec git
func (w *Worktreer) MergeInto(leadPath, branch string) (merged bool, conflicts []string, err error)
```

## Flux de données

Le lead agent termine un tour par `⟢ merge: <worker>` (un seul worker par trailer,
cohérent avec « une ligne trailer par tour »). Nouveau parser `parseMerge` inséré
dans `maybeCoordinate` : `parseDone → parseDelegate → parseMerge → parseHandoff`
(premier match gagne).

`Merge(ctx, {FromSession, Worker})` — gardes, **toutes avant tout effet de bord**,
sur **un seul snapshot atomique** (`SnapshotSessions`, via `findByName`) :

1. **Lead connu** — `FromSession` existe. Sinon `merge: lead %q not found`.
2. **Worker connu** — `Worker` existe. Sinon `merge: worker %q not found`.
3. **Worker enfant de ce lead** — `worker.Parent == FromSession`. Sinon
   `merge refused: %q is not a worker of %q`. On ne merge que ses propres workers.
4. **Worker commité** — `IsCleanAt(worker.Worktree)`. Le tip `session/<W>` est ce
   qu'on agrège ; du non-commité ne serait pas dans le merge. Refus si sale.
5. **Lead commité** — `IsCleanAt(lead.Worktree)`. Indispensable pour l'abort propre :
   sans ça, un `git merge --abort` restaurerait un état écrasant le non-commité du
   lead. Refus si sale.

Puis `MergeInto(lead.Worktree, c.wt.Branch(Worker))` :

- **succès** → merge commit dans le worktree du lead. Seed au lead :
  `"branche de <W> mergée dans <lead>"`.
- **déjà à jour** (`merged=true`, aucun nouveau commit) → status neutre
  `"<W> déjà à jour dans <lead>"`. Idempotent.
- **conflit** → `git merge --abort` exécuté (worktree lead propre), seed au lead :
  `"merge de <W> refusé : conflit sur <fichiers> — résous manuellement"`. Les
  fichiers sont lus via `git diff --name-only --diff-filter=U` avant l'abort.
- **autre échec git** → erreur remontée telle quelle.

**W reste vivant. Zéro écriture sur `reported`/`mu`.** Le merge git s'exécute hors
verrou (aucun état partagé touché).

## Gestion d'erreur & cas limites

- **Lead sale / worker sale** → gardes 4-5, rien n'est tenté, message actionnable.
- **Worker pas enfant du lead** (`Parent != FromSession`, y compris `Parent==""`) →
  refus explicite. Empêche de merger une session arbitraire.
- **Conflit** → abort, worktree lead propre, diagnostic listant les fichiers.
- **Worker déjà mergé sans nouveau commit** → `git merge` « Already up to date » →
  succès no-op, status neutre. Idempotent.
- **Repo non-git / worktree vide** → `IsCleanAt` échoue en garde, remonté comme
  erreur (jamais confondu avec « propre »).
- **`Merge` d'un worker qui n'a jamais fait Report** → autorisé : le merge ne dépend
  pas de l'état join, seulement du commit `session/<W>`. Un lead peut agréger un
  worker avant même son `⟢ done`.

## Fichiers touchés

**herrscher-contracts :**
- Type `MergeRequest` + méthode `Merge` sur l'interface `Coordinator` + son test de
  compilation/port.

**herrscher :**
- `core/internal/worktree/worktree.go` — méthode `MergeInto` (+ tests vrai-git).
- `core/host/coordinator.go` — méthode `Merge` (gardes + appel `MergeInto` + seed).
  L'interface host-interne `cleanBrancher` gagne `MergeInto` (satisfaite par
  `*Worktreer`, mockée par les fakes de test).
- `core/host/merge.go` (ou dans `turnloop.go`/`handoff.go` selon le placement des
  parsers existants) — `parseMerge`, réutilisant le helper `parseTrailer`.
- `core/host/turnloop.go` — `maybeCoordinate` insère `parseMerge` dans le dispatch.
- Tests : `coordinator_test.go`, `worktree_test.go`, le test de parsing.

## Tests (TDD)

**coordinator (fakes, `fakeMerger` injecté via `cleanBrancher`) :**
- `TestMergeDeliversWorkerBranchToLead` — worker enfant commité → `MergeInto` appelé
  avec `(leadPath, session/<W>)`, status succès seedé au lead.
- `TestMergeRefusesNonChildWorker` — `Parent != lead` → refus, `MergeInto` jamais
  appelé.
- `TestMergeRefusesDirtyWorker` / `TestMergeRefusesDirtyLead` — gardes de propreté.
- `TestMergeConflictAbortsAndReports` — `fakeMerger` renvoie conflit → status
  « refusé : conflit sur … ».
- `TestMergeAlreadyUpToDate` — status neutre idempotent.
- `TestMergeUnknownWorker` / `TestMergeUnknownLead` — gardes d'existence.

**parsing :**
- `TestParseMerge` — `⟢ merge: worker-x` parsé ; non-match sur `done`/`delegate`/
  `handoff`.

**worktree (vrai git, comme les tests existants) :**
- `TestMergeIntoCleanMerge` — branche divergente non conflictuelle → merge commit,
  worktree propre.
- `TestMergeIntoConflictAborts` — deux branches modifiant la même ligne → conflit
  détecté, `--abort` exécuté, worktree restauré propre, fichiers listés.

**Régression :** les tests `Report`/`Delegate`/`Handoff`/join existants restent
verts.

## Contraintes globales

- **Générique OSS** : zéro connaissance métier Roblox/Neublox dans le code ou les
  messages (« worker »/« lead », pas de vocabulaire domaine).
- **`herrscher-contracts` touché** (assumé) : nouvelle méthode `Merge` +
  `MergeRequest` ; les autres signatures du port `Coordinator` restent inchangées.
- **CI verte des deux côtés** : `gofmt -l` propre, `go vet ./...` propre,
  `go build ./...`, `go test -race ./...` verts, `go mod tidy` laisse `go.mod`
  inchangé (herrscher ET contracts).
- **Worktree lead toujours propre** : succès (merge commit) ou échec (abort) laissent
  le worktree du lead sans changement non-commité — invariant partagé avec toutes les
  autres primitives.
- **1 tranche = 1 commit** logique par unité testable (port contracts, `MergeInto`,
  `parseMerge`, `Merge`+dispatch — regroupés selon l'atomicité de compilation Go).
