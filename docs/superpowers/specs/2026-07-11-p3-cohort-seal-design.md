# P3 — Coordination multi-agents : scellage de cohorte (barrière déterministe)

> Cinquième tranche du chantier « coordination multi-agents » dans le châssis
> herrscher, par-dessus le socle `Coordinator` (handoff → delegate + result-back →
> fan-out + join → join+merge). Feature **générique OSS** (aucune connaissance
> métier Roblox/Neublox). Donne au lead la capacité de **déclarer N attendu**, ce
> qui transforme le « tous livrés » best-effort du join en **barrière
> déterministe**.
> Date : 2026-07-11 · Statut : design validé.

## Contexte vérifié (source)

Les quatre premières tranches, mergées + poussées (join+merge en attente sur
`feat/p3-join-merge`, merge-ready) :

- **Handoff** (relais A→B, one-shot) — `contracts 4ef2f6c`, `herrscher a66b844`.
- **Delegate + result-back** (round-trip L↔W, 1 worker) — `contracts 306d3c4`.
- **Fan-out + join** (suivi de cohorte `done/total`) — `herrscher` merge 2026-07-11.
- **Join+merge** (agrégation réelle) — `contracts 195587d`, `herrscher`
  `feat/p3-join-merge` (merge-ready, revue finale opus « Yes »).

État du code (`core/host/coordinator.go`), Modèle O (host-driven : l'agent
*signale* via un trailer en dernière ligne, le host valide, le `coordinator`
hub-level exécute) :

- `reported map[string]map[string]bool` + `mu` — état join en mémoire ; `forget`
  purge à la fermeture.
- `Report(ctx, {FromSession, Summary})` (`coordinator.go:174-221`) — sous `mu`,
  `reported[P][W]=true`, `done = len(reported[P])`, puis
  **`total = nombre de frères vivants avec Parent==P`** (boucle sur le snapshot,
  lignes 205-210). Message `"<W> a terminé sur session/<W> (done/total) — <résumé>"`,
  suffixe `" — tous les workers ont livré"` si `done >= total`.
- `maybeCoordinate` (`core/host/turnloop.go`) dispatche
  `parseDone → parseDelegate → parseMerge → parseHandoff` (premier match gagne).
- Trailers (`core/host/handoff.go`) : `⟢ done:`, `⟢ delegate:`, `⟢ merge:`,
  `⟢ handoff:`. `parseTrailer` isole la dernière ligne ; `parseDone`/`parseMerge`
  prennent le corps entier comme un seul token.

## Le problème (best-effort)

`total` est le compte **momentané** des frères vivants au moment du Report. En
fan-out séquentiel, si un worker livre avant que le lead ait fini de distribuer
toute la cohorte, `total` est transitoirement petit → un
`done >= total` **prématuré** déclenche un « tous les workers ont livré » **faux**.
La tranche fan-out+join l'a assumé explicitement : le châssis rend un *indice*, pas
une *garantie*, et a renvoyé le scellage à une tranche ultérieure. C'est celle-ci.

## Ce que cette tranche fait — et ne fait pas

Le lead **déclare N attendu** via un nouveau trailer `⟢ seal: <N>`. Une fois la
cohorte scellée, `total` devient **déterministe** (N figé) et le
« cohorte complète » ne se déclenche qu'à `done >= N` — jamais prématurément. Le
comportement **non scellé reste best-effort, inchangé** : additif, rétro-compatible.

**Hors périmètre (délibérément, tranches suivantes) :**
- **Barrière dure** (le host gèle le lead jusqu'à N) — on garde les arrivées
  incrémentales ; le scellage rend le *signal* fiable, il ne bloque pas le lead.
- **Teardown / merge automatique à complétion** — le cycle de vie et l'agrégation
  restent pilotés séparément (`⟢ done`, `⟢ merge`, close).
- **Superviseur→workers, routage LLM** — tranches ultérieures, même `Coordinator`.

## Décisions (arbitrées en brainstorm)

1. **`Seal` est une méthode de port** (touche `herrscher-contracts`). Chaque trailer
   déclenché par l'agent mappe une méthode du port `Coordinator`
   (`done→Report`, `delegate→Delegate`, `merge→Merge`) ; `seal→Seal` suit la même
   règle. `forget` reste host-interne car il n'est **pas** déclenché par l'agent
   (cycle de vie).
2. **Garde `N >= cohorte courante`** : `Seal` refuse un N inférieur au nombre de
   workers déjà délégués par ce lead (empêche le sous-scellage à la source). Combiné
   à `N > 0`.
3. **Wording différencié** : scellé & complet → `" — cohorte complète"` (garantie) ;
   non scellé & complet → `" — tous les workers ont livré"` (best-effort, inchangé).
   Le mot dit au lead le **niveau de confiance** du signal.
4. **`Seal` ne seed pas dans le tour du lead** : il enregistre l'état + émet un
   status `"cohorte scellée à N"` (cohérent avec `Delegate`, qui change l'état sans
   self-seed). La barrière se manifeste ensuite dans les messages `Report`.
5. **Re-seal autorisé (last-wins)** : le lead peut corriger son N. Pas de nouvel état
   « scellé figé/immuable » ce tour-ci (YAGNI).

## Architecture

Le scellage vit dans le `coordinator` (couche host, Modèle O), avec une extension
du port.

Port (`herrscher-contracts`, `Coordinator`) — ajout :

```go
type SealRequest struct {
    FromSession string // le lead qui déclare
    Expected    int    // N attendu (> 0)
}
// méthode ajoutée à l'interface Coordinator :
Seal(ctx context.Context, req SealRequest) (lead string, err error)
```

Coordinator (`core/host/coordinator.go`) — ajout :

```go
expected map[string]int // parent → N attendu (sous mu)
```

initialisé dans `newCoordinator`, purgé par `forget`.

## Flux de données

Le lead termine un tour par `⟢ seal: <N>`. Nouveau parser `parseSeal` inséré dans
`maybeCoordinate` : `parseDone → parseDelegate → parseSeal → parseMerge →
parseHandoff` (markers distincts, l'ordre ne crée aucune collision).

`Seal(ctx, {FromSession, Expected})` — gardes, **toutes avant tout effet de bord**,
sur un snapshot atomique :

1. **Lead connu** — `FromSession` existe. Sinon `seal: lead %q not found`.
2. **N positif** — `Expected > 0`. Sinon `seal refused: expected must be > 0`.
3. **N >= cohorte courante** — `Expected >= count({s : s.Parent == FromSession})`.
   Sinon `seal refused: expected %d below current cohort size %d`.

Puis, sous `mu` : `expected[FromSession] = Expected`. Status fan-out
`"cohorte scellée à %d"`. Retour `(FromSession, nil)`.

`Report` — **une seule ligne change** (le calcul de `total`) :

```
sous mu : n, sealed := c.expected[P]
si sealed : total = n
sinon      : total = nombre de frères vivants avec Parent==P   (actuel)
```

Le suffixe se différencie :

- scellé & `done >= total` → `" — cohorte complète"`.
- non scellé & `done >= total` → `" — tous les workers ont livré"` (inchangé).

Le corps `"<W> a terminé sur session/<W> (done/total) — <résumé>"` est inchangé.

## Gestion d'erreur & cas limites

- **Sur-livraison après scellage** : un `delegate` post-`seal` fait grimper les
  frères au-delà de N. Un worker supplémentaire livre → `done` peut dépasser N →
  affichage `6/5 — cohorte complète`. Sémantiquement : le lead a rompu son propre
  sceau en déléguant après coup ; à re-sceller. On garde `expected` figé et on
  documente (philosophie best-effort conservée). Aucune corruption d'état.
- **Re-seal** : `expected[P] = N` écrase l'ancien N (last-wins). Idempotent si même N.
- **Report d'une cohorte non scellée** : chemin best-effort actuel, bit pour bit.
- **`forget(lead)`** : `delete(expected, lead)` — la cohorte scellée est jetée avec
  son `reported`. `delete` sur clé absente = no-op, aucune garde.
- **Concurrence** : `expected` n'est jamais touché hors `mu` (comme `reported`),
  vérifiable au `-race`. Le `total` sealed est lu sous `mu` (le lookup `expected`),
  puis le compte non scellé reste hors verrou sur le snapshot atomique (bénin,
  best-effort).
- **`Seal` avant tout `delegate`** : garde 3 passe (cohorte courante = 0 <= N).
  Le lead peut sceller d'abord puis distribuer.

## Fichiers touchés

**herrscher-contracts :**
- Type `SealRequest` + méthode `Seal` sur l'interface `Coordinator` + son test de
  compilation/port.

**herrscher :**
- `core/host/coordinator.go` — champ `expected` (init dans `newCoordinator`),
  méthode `Seal` (gardes + enregistrement), `Report` enrichi (calcul `total` +
  wording), `forget` purge `expected`.
- `core/host/handoff.go` — `parseSeal` + const `sealMarker = "⟢ seal:"`.
- `core/host/turnloop.go` — `maybeCoordinate` insère `parseSeal` dans le dispatch.
- Tests : `coordinator_test.go`, `handoff_test.go`, contracts `coordinator_test.go`.

## Tests (TDD)

**contracts :**
- Port : `SealRequest` + `Seal` dans l'assertion de surface du `Coordinator`.

**parsing (`handoff_test.go`) :**
- `TestParseSeal` — `⟢ seal: 5` → `5, true` ; non-entier / `0` / négatif / corps
  vide / marker absent → `_, false`.

**coordinator (`coordinator_test.go`, fakes existants) :**
- `TestSealRecordsExpected` — lead connu, N valide → `expected[lead]=N`, status seedé.
- `TestSealRefusesNonPositive` — `N<=0` → refus, rien enregistré.
- `TestSealRefusesBelowCurrentCohort` — 3 workers délégués, `seal: 2` → refus.
- `TestSealUnknownLead` — lead inconnu → refus.
- `TestReportUsesSealedTotal` — scellé à 3, 3 frères ; reports montrent `(1/3)`,
  `(2/3)`, `(3/3) — cohorte complète` de façon déterministe.
- `TestReportSealedCompleteWording` — le suffixe scellé est « cohorte complète »,
  pas « tous les workers ont livré ».
- `TestReportUnsealedStaysBestEffort` — régression : sans seal, wording et `total`
  best-effort actuels inchangés.
- `TestForgetPurgesSeal` — `forget(lead)` vide `expected[lead]`.
- Les tests `Report`/`Delegate`/`Handoff`/`Merge`/join existants restent verts.

## Contraintes globales

- **Générique OSS** : zéro connaissance métier Roblox/Neublox dans le code ou les
  messages (« worker »/« lead »/« cohorte », pas de vocabulaire domaine).
- **`herrscher-contracts` touché** (assumé) : nouvelle méthode `Seal` +
  `SealRequest` ; les autres signatures du port `Coordinator` restent inchangées.
- **CI verte des deux côtés** : `gofmt -l` propre, `go vet ./...` propre,
  `go build ./...`, `go test -race ./...` verts, `go mod tidy` laisse `go.mod`
  inchangé (herrscher ET contracts).
- **Concurrence sûre** : `expected` n'est jamais touché hors `mu` (vérifiable
  au `-race`).
- **1 tranche = 1 commit** logique par unité testable (port contracts, `parseSeal`,
  `Seal`+`Report`+dispatch — regroupés selon l'atomicité de compilation Go).
