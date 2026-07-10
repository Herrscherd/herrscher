# P3 — Coordination multi-agents : delegate + result-back (tranche verticale)

> Deuxième tranche du chantier « coordination multi-agents » dans le châssis
> herrscher, par-dessus le socle `Coordinator` posé par le handoff. Feature
> **générique OSS** (aucune connaissance métier Roblox/Neublox). Introduit le
> premier round-trip lead↔worker : un lead délègue une sous-tâche et **reçoit** le
> résultat — la brique que superviseur→workers et fan-out réutiliseront.
> Date : 2026-07-10 · Statut : livré (branche `feat/p3-delegate-result-back`,
> CI-vert : gofmt/vet/`go test -race` OK sur herrscher et contracts).

## Contexte vérifié (source)

La première tranche (handoff, mergée sur `master` : herrscher `a66b844`, contracts
`ecd691f`) a posé le **Modèle O** et le port `Coordinator` :

- `herrscher-contracts/coordinator.go` — `Coordinator interface { Handoff(ctx, HandoffRequest) (session, err) }`
  + `CreateSession.Base` (branche B sur le tip commité de A).
- `core/host/coordinator.go` — implémentation hub-level : `Handoff` résout l'agent,
  garde « A commité », choisit un nom libre (`BranchExistsAt` + suffixe), crée B via
  `hub.Create` sur la branche de A, seed la tâche (`Seed`), rollback si le seed
  échoue. Petits ports : `sessionCreator`, `agentLookup`, `cleanBrancher`,
  `sessionLister`, `sessionCloser`.
- `core/host/turnloop.go` — `maybeHandoff` appelé après le tour (sur `reply` `Done`),
  parse le trailer, appelle le coordinator, remonte les erreurs en events `status`.
- `core/host/handoff.go` — `parseHandoff` pur (dernière ligne non vide, préfixe
  `⟢ handoff:`, séparateur em-dash `—`).

**Limite structurelle du handoff** (assumée, documentée hors-périmètre de la tranche
1) : c'est un **relais aveugle**. A crée B puis **se termine** ; A ne saura jamais que
B a fini, aucun résultat ne remonte. Le design D5 de la tranche 1 prévoyait
explicitement que superviseur→workers et fan-out soient des **politiques par-dessus le
même `Coordinator`**. Le chaînon manquant commun aux deux : un **canal de retour**
(un worker signale sa complétion et renvoie un résultat à un parent resté vivant).

Cette tranche livre ce canal via une primitive **distincte du handoff** : `delegate`.

## Objectif

Livrer le **round-trip 1:1 lead↔worker** de bout en bout :

1. Un lead L signale `⟢ delegate: <agent> — <task>`. Le host crée un worker W (profil
   `<agent>`) qui continue la branche commitée de L, **enregistre L comme parent de
   W**, et seede la tâche. **L reste vivant** (contrairement au handoff où A se
   termine).
2. W fait son travail, le commite, puis signale `⟢ done: <résumé>`. Le host remet à L
   un frame `{réf branche session/<W> + résumé}`. **W reste ouvert** (L décide de sa
   fermeture).

Le fan-out N-way, la concurrence parallèle et le join-all restent pour les tranches
suivantes, sur ce même round-trip.

## Décisions

### D1 — Primitive `delegate` distincte du handoff (Modèle O)
Le handoff reste le relais aveugle (A se termine). `delegate` est un **verbe séparé**
avec lien parent explicite : le lead garde le contrôle et reçoit le résultat. Comme le
handoff, l'agent **signale** (trailer) ; le host valide ; le `Coordinator` exécute.
Rejeté : étendre le handoff pour que A reste vivant (changerait sa sémantique « A se
termine » et mélangerait deux intentions) ; un champ `Parent` posé n'importe comment
sur `CreateSession` sans verbe dédié (moins lisible, pas de garde d'intention).

### D2 — Lien parent↔worker : champ `Parent` sur la session, posé par `delegate`
`Delegate` crée W avec `state.Session.Parent = L` (via un nouveau `CreateSession.Parent`,
défaut `""` = pas de parent, comportement inchangé). Le `done` de W lit `W.Parent` pour
savoir à qui remonter. Le champ est plombé exactement comme `Base` l'a été
(`CreateSession.Parent` → param CLI `parent` → `state.Session.Parent`).

### D3 — Création partagée `Handoff`/`Delegate` (DRY)
`Handoff` et `Delegate` ne diffèrent QUE par le champ `Parent` posé. On factorise un
helper interne `spawn(ctx, from, toAgent, task, parent)` portant la logique commune
(résolution d'agent → garde « L commité » → nom libre via `BranchExistsAt` → `Create`
sur la branche de L avec `Base`+`Parent` → `seedWithRetry` + rollback). `Handoff`
appelle `spawn(..., parent="")` ; `Delegate` appelle `spawn(..., parent=FromSession)`.
Le « A se termine » du handoff vit **hors** du coordinator (dans le tour de A) : ni
`Handoff` ni `Delegate` ne ferme L — c'est déjà le cas aujourd'hui.

### D4 — Result-back : `done` remonte réf branche + résumé
W signale `⟢ done: <résumé>`. `Report` :
- garde que W a un worktree **commité** (miroir du garde handoff ; sinon la branche
  `session/<W>` ne porte pas le travail) — W sale → refus « commite d'abord » ;
- lit `W.Parent` ; vide → erreur « la session n'a pas de parent à qui remonter » ;
- résout le parent (doit exister dans le snapshot) ; absent → erreur « parent parti » ;
- remet à L, via `Seed`, un frame texte portant **le nom de W, sa branche
  `session/<W>` (travail commité, inspectable/mergeable par L) et le résumé**. L décide
  quoi en faire (lire le diff, merger, redéléguer). Aucun merge automatique.

### D5 — W reste vivant après remise
`Report` est une **pure remise** : aucun effet de bord de cycle de vie sur W. W reste
ouvert ; L (ou l'opérateur) le ferme quand il veut. Permet des allers-retours multiples
avec le même W. Rejeté pour cette tranche : fermeture de W par le coordinator (ajoute un
teardown et interdit le multi-retour) ; auto-close par l'agent W (dépend de l'agent pour
ne pas laisser d'orphelin).

### D6 — Un seul hook de tour, parseurs purs séparés
`maybeHandoff` devient `maybeCoordinate` : après le tour, il essaie les parseurs dans un
ordre **déterministe** (`done` → `delegate` → `handoff`) ; le premier qui matche gagne,
sa méthode `Coordinator` est appelée, les erreurs remontent en events `status` (comme
aujourd'hui). Chaque trailer est un **verbe distinct** donc au plus un matche ; l'ordre
n'est qu'une précédence défensive. Parseurs purs `parseDelegate`/`parseDone` calqués sur
`parseHandoff`, avec un helper commun `parseTrailer(reply, prefix)` pour « dernière ligne
non vide + préfixe + (pour delegate) split em-dash ». `parseDone` n'a pas de séparateur :
tout après `⟢ done:` est le résumé.

## Architecture

```
tour du lead L ─▶ host (boucle de tour, après reply Done)
                    │  maybeCoordinate(reply):
                    │    parseDone?     → Coordinator.Report   (si W)
                    │    parseDelegate? → Coordinator.Delegate (si L)
                    │    parseHandoff?  → Coordinator.Handoff  (inchangé)
                    ▼
   Delegate(ctx, {FromSession:L, ToAgent, Task})            (NOUVEAU)
        └─ spawn(L, ToAgent, Task, parent=L)  ── partagé avec Handoff (parent="")
             a. résout ToAgent ; inconnu → erreur
             b. garde L commité ; sale → refus
             c. nom libre session/<W> (BranchExistsAt + suffixe)
             d. hub.Create{Agent:ToAgent, Project:L.Project, Base:session/<L>, Parent:L}
             e. seedWithRetry(W, Task)  (rollback si timeout)
             → renvoie W ; L reste vivant

tour du worker W ─▶ host
   Report(ctx, {FromSession:W, Summary})                    (NOUVEAU)
        a. résout W ; introuvable → erreur
        b. garde W commité ; sale → refus « commite d'abord »
        c. lit W.Parent ; vide → erreur « pas de parent »
        d. résout le parent L ; absent → erreur « parent parti »
        e. Seed(L, "<W> a terminé sur session/<W> — <résumé>")
        → renvoie L ; W reste ouvert
```

### Composants (unités isolées)
- **`herrscher-contracts`** — `CreateSession.Parent` ; `DelegateRequest{FromSession,
  ToAgent, Task}` ; `ReportRequest{FromSession, Summary}` ; port `Coordinator` étendu de
  `Delegate` + `Report`. *Dépend de : rien.*
- **`spawn` + `Delegate`** (`core/host/coordinator.go`) — factorisation de la création
  partagée avec `Handoff`, `Delegate` pose `Parent`. *Dépend de : les ports existants du
  coordinator.*
- **`Report`** (`core/host/coordinator.go`) — résout parent, gardes, remise via `Seed`.
  *Dépend de : `sessionLister`, `cleanBrancher`, `seed`.*
- **Plomberie `Parent`** (`core/internal/manager`, `core/host/hub.go`,
  `core/internal/state`) — param `parent` sur `session create` → `state.Session.Parent`.
  *Miroir de `Base`.*
- **Parseurs + dispatch** (`core/host/handoff.go` + `core/host/turnloop.go`) —
  `parseTrailer` helper, `parseDelegate`, `parseDone`, `maybeCoordinate`. *Purs et
  testables seuls sauf le dispatch.*

## Flux de données
1. L répond ; sa réponse finit par `⟢ delegate: scripter — écris le module X`.
2. `maybeCoordinate` → `parseDelegate` matche → `Coordinator.Delegate(L, scripter, …)`.
3. `spawn` crée `session/<L>-scripter` sur la branche de L, `Parent=L`, seede la tâche.
   L reste vivant, idle jusqu'au retour.
4. W travaille, commite, répond `⟢ done: module X écrit et testé`.
5. `maybeCoordinate` → `parseDone` matche → `Coordinator.Report(W, "module X écrit…")`.
6. Gardes OK → `Seed(L, "<W> a terminé sur session/<W> — module X écrit et testé")`.
7. L reprend au tour suivant avec le résultat ; W reste ouvert.

## Gestion d'erreur
- **Delegate** — agent inconnu → erreur remontée dans le canal de L, rien créé ; L sale
  → refus « commite d'abord » ; `hub.Create` échoue → erreur, rien de partiel ; seed
  timeout → rollback de W (comme le handoff).
- **Report** — W introuvable → erreur ; W sale → refus « commite d'abord » ; W sans
  parent → erreur loggée ; parent absent (fermé) → erreur loggée ; le seed du parent
  utilise `seedWithRetry` (le parent est vivant, donc succès rapide attendu).
- **Trailer malformé** → ignoré **et loggé** (jamais d'intention devinée). Tous les
  gardes avant tout effet de bord.

## Tests (fakes, pas de vrai backend LLM)
- `parseDelegate` : `⟢ delegate: a — t` → `(a, t, true)` ; absent/malformé/espaces →
  `(_, _, false)`. `parseDone` : `⟢ done: résumé` → `(résumé, true)` ; sans séparateur,
  tout le reste est le résumé ; absent → `(_, false)`.
- `spawn`/`Delegate` : crée W avec `Base == session/<L>` **et `Parent == L`**, seede la
  tâche ; agent inconnu / L sale refusés, rien créé.
- `Handoff` régression : passe toujours `Parent == ""` (le partage ne casse pas le
  fire-and-forget).
- `Report` : résout le parent depuis `W.Parent`, seede le parent avec un texte contenant
  `session/<W>` et le résumé ; W sale → refus ; `W.Parent` vide → erreur ; parent absent
  du snapshot → erreur. W jamais fermé (aucun `Close` appelé).
- Plomberie : `session create` avec `parent=foo` → `state.Session.Parent == "foo"`
  (miroir du test `base`).
- Intégration boucle-de-tour : reply avec trailer `delegate` → un worker parenté est créé
  et la tâche seedée ; reply `done` sur un worker parenté → le parent est seedé ; reply
  sans trailer → rien ; `done` sur une session sans parent → event `status` d'erreur, rien
  de créé/seedé.

## Critères d'acceptation
- `⟢ delegate: <agent> — <task>` crée W branché sur le tip commité de `session/<L>`, avec
  `Parent == L`, seedé avec la tâche ; **L reste ouvert**.
- `⟢ done: <résumé>` d'un worker parenté commité remet à L un frame contenant
  `session/<W>` et le résumé, via le canal d'entrée ; **W reste ouvert**.
- L sale / W sale → refus clair ; agent inconnu → erreur ; W sans parent / parent parti →
  erreur loggée ; trailer malformé → ignoré + loggé.
- `Handoff` inchangé fonctionnellement (`Parent` reste `""`).
- Aucune surface MCP ajoutée ; contrat de la factory plugin `Orchestrator` inchangé.
- `go test ./...` vert sur les deux repos touchés ; `gofmt` propre (CI herrscher).
- `Delegate`/`Report` réutilisables tels quels par superviseur→workers et fan-out.

## Hors périmètre (YAGNI)
- Fan-out N-way (un lead qui émet N `delegate`) et la **barrière join-all** (attendre
  que tous les workers finissent) — tranche suivante, même round-trip.
- Agrégation de plusieurs résultats côté lead (ici : remise 1:1, L agrège lui-même).
- Merge automatique de la branche de W par L (L décide manuellement).
- Fermeture automatique de W (choisi : W reste vivant).
- Routage à **jugement LLM** côté coordinateur (le signal vient de l'agent).
- Worktree ① (transport du non-commité) et profils d'agents spécialistes **Roblox**
  (contenu Neublox) — déjà hors-périmètre en tranche 1, inchangé.
