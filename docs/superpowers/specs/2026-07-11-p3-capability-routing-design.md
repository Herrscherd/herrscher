# Tranche P3 — Routage déterministe par capacités (design)

**Date :** 2026-07-11
**Statut :** design approuvé (fork d'architecture tranché par l'utilisateur)
**Branche :** `feat/p3-join-merge` (accumule les 4 tranches P3)

## Problème

Les tranches 1–3 donnent au *lead* les primitives pour distribuer du travail :
`⟢ delegate` (un worker), `⟢ fanout` (un lot de workers vers **un** agent nommé),
`⟢ seal`, `⟢ merge`. Dans tous les cas le lead **nomme lui-même** l'agent
destinataire. Il manque le dernier maillon de coordination : « quel agent est le
mieux placé pour *cette* tâche ? » — le routage.

## Fork d'architecture (tranché)

« Routage par jugement LLM » suggérait de faire *juger* le host. Le code a tranché
la faisabilité :

- `agent.Agent` n'expose que `Name` + `Home` — aucune métadonnée sémantique.
- Le `coordinator` est **délibérément sans LLM** (invariant Model O : le host est
  déterministe ; seuls les agents, adossés à un LLM, jugent).

Mettre un appel LLM dans le host pour « juger » **casserait Model O**, le socle de
tout le châssis de coordination. L'utilisateur a donc choisi le **match
déterministe (sans LLM)** : les agents *déclarent* leurs capacités, le host les
*score* contre la tâche par une règle pure et reproductible. Le « jugement » reste
entièrement dans la couche agent (les tags authoriés par l'agent + la formulation
de la tâche par le lead) ; le host ne fait qu'appliquer un barème déterministe.

## Décisions de design

### D1 — Déclaration des capacités : fichier `TAGS` dans le home de l'agent

Le home d'un agent est déjà la source de vérité durable (`SOUL.md`, `mcp.json`,
`settings.json`). On y ajoute un **fichier optionnel `TAGS`** : des tokens de
capacité séparés par des espaces, virgules ou retours-ligne. Absent ou vide →
l'agent n'a aucun tag et n'est jamais auto-sélectionné (sauf à être le seul, cf.
D4 — non : score 0 ne gagne jamais). C'est cohérent avec le modèle domain-neutral :
le profil appelant (p.ex. Roblox de Neublox) fournit les tags, le paquet `agent`
ne fait que les stocker et les lire.

- Format : tokens séparés par `[\s,]+`, chacun *lowercasé* et dé-dupliqué.
- Pas de commentaires, pas de pondération (YAGNI).
- `Materialize` **ne** copie **pas** `TAGS` dans le worktree : c'est une métadonnée
  d'orchestration côté host, pas un fichier que Claude Code lit.

### D2 — `agent.Agent` gagne `Tags []string`

`Store.Get` et `Store.List` peuplent `Tags` en lisant `TAGS` (best-effort : absent
ou illisible → `nil`, jamais une erreur — un agent sans tags reste un agent
valide). L'`Agent` devient auto-descriptif ; le coordinator lit le roster via
`agents.List()` sans I/O supplémentaire de son côté.

### D3 — Nouveau trailer `⟢ route: <task>` → méthode de port `Route`

Règle établie : chaque trailer déclenché par un agent mappe une méthode de port.

- `⟢ route: <task>` — **aucun agent nommé** (c'est tout l'intérêt : le host choisit).
  Le corps entier est la tâche (comme `⟢ done`, pas de split em-dash). Corps vide →
  pas un route.
- Port : `Route(ctx, RouteRequest{FromSession, Task}) (agent, session string, err error)`.
  Retourne l'agent choisi **et** la session créée — pour que le statut soit
  transparent (« routé vers X »).

### D4 — Barème de sélection : `pickAgent(roster, task) (name string, ok bool)` pur

Fonction pure, déterministe, testable sans fake :

1. Tokeniser la tâche : lowercase, découpe sur tout ce qui n'est pas `[a-z0-9]`,
   en un **ensemble** de tokens.
2. Score d'un agent = nombre de ses tags présents comme token dans cet ensemble
   (un tag est un token unique ; un tag multi-mots ne matcherait aucun token —
   documenté, à éviter).
3. Meilleur agent = score **max strictement > 0**. Le roster de `List()` est trié
   par nom ; on garde le premier atteignant le max → égalité tranchée par le nom
   lexicographiquement le plus petit (déterministe).
4. Tous les scores à 0 → `ok=false`. **Aucun fallback** vers un agent par défaut :
   ce serait un jugement caché. Le host refuse explicitement.

### D5 — Après sélection : sémantique `Delegate`

`Route` réutilise `spawn(ctx, lead, chosen, task, lead)` : le worker a le lead pour
parent (result-back via `Report`), le lead reste vivant. Route n'est *que*
« Delegate avec choix host-side de l'agent ». Les gardes de `spawn` s'appliquent
(lead avec worktree, lead propre). Le roster est énuméré **avant** le spawn ; un
lead introuvable ou une tâche vide échoue sans rien créer.

### D6 — Port `agentLookup` étendu avec `List`

Le coordinator dépend de `agentLookup` (aujourd'hui `Get`). On y ajoute
`List() ([]agent.Agent, error)`, déjà satisfait par `*agent.Store`. Les fakes de
test l'implémentent (inerte là où le routage n'est pas exercé).

### D7 — Ordre de dispatch

`maybeCoordinate` : `done → delegate → fanout → route → seal → merge → handoff`
(un seul trailer par tour, premier match gagne). `route` se place avec les
intents spawn-like (après `fanout`, avant `seal`). Commentaires de priorité mis à
jour dans `handoff.go` **et** `turnloop.go`.

- Succès → status `"routé vers <agent> : <session>"`.
- Refus (tâche vide, lead absent, aucun agent, spawn échoué) → `"route refusé: <err>"`.

## Invariant préservé

Le host reste 100 % déterministe. Aucune inférence LLM n'entre dans le
`coordinator`. Le seul « jugement » est (a) les tags que l'agent s'attribue dans
son propre home et (b) la formulation de la tâche par le lead — deux artefacts de
la couche agent. Model O intact.

## Hors périmètre (YAGNI)

- Pondération des tags, synonymes, stemming, correspondance floue.
- Fallback vers un agent par défaut (jugement caché — explicitement refusé).
- Routage multi-agents en un tour (`route` choisit **un** agent, **une** tâche ;
  le fan-out reste `⟢ fanout` vers un agent nommé).
- Édition des tags via CLI (les tags vivent dans `TAGS` sur disque, comme le reste
  du home).

## Testabilité

- `pickAgent` : tests table-driven purs (aucun fake) — égalités, score 0, casse,
  ponctuation, tags multiples.
- `readTags` / `Store.Get`/`List` : tmpdir avec/sans `TAGS`, séparateurs mixtes.
- `Route` (host) : fakes existants + roster fake — agent choisi, tâche vide, lead
  absent, aucun match, lead sale (zéro spawn).
- `parseRoute` : cases marker/vide/whitespace.
- Dispatch : `⟢ route:` invoque bien `Route` sur le coordinator (recordingCoord).
