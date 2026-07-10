# P3 — Coordination multi-agents : le handoff (tranche verticale)

> Chantier « coordination multi-agents » dans le châssis herrscher. Feature
> **générique OSS** (aucune connaissance métier Roblox/Neublox). Première tranche
> verticale d'un socle réutilisé ensuite par superviseur→workers et fan-out.
> Date : 2026-07-10 · Statut : design validé.

## Contexte vérifié (source)

Le port `Orchestrator` (`herrscher-contracts/orchestrator.go`) se décrit lui-même
comme propriétaire de la stratégie de curation *« a rolling transcript, summarisation,
**multi-agent routing** »*. Mais deux faits structurels l'empêchent aujourd'hui de
tenir ce rôle de coordinateur :

1. Il est **par-session** (« the host builds one per session ») : via `Context`/
   `Observe` il ne voit **que ses propres tours**, pas les autres sessions.
2. Sa factory plugin ne reçoit que `(ctx, cfg, mem)` — **aucun handle vers le hub** :
   il ne peut ni créer ni cibler une autre session.

La coordination inter-sessions doit donc vivre à la **couche qui voit toutes les
sessions et pilote le hub** : le host / hub, pas le plugin `Orchestrator` par-session.
C'est le **Modèle O** (piloté par la couche orchestration, *pas* par l'agent) : l'agent
ne fait que **signaler** une intention de relais ; le host la valide et le coordinateur
l'**exécute**.

État actuel confirmé (Explore, 2026-07-10) : aucun mécanisme de handoff / délégation /
routage parallèle n'existe dans le code. Les sessions sont indépendantes ; le hub
sérialise `Create`/`Close`/`Sessions`/`Dispatch` mais ne coordonne pas les agents entre
eux. Détail de cycle de vie clé (`core/internal/worktree/worktree.go`) : `Remove` retire
le worktree mais **laisse toujours la branche `session/<name>` intacte** — c'est ce qui
rend la continuation de branche (§ Décisions, worktree ②) peu coûteuse.

## Objectif

Livrer le **handoff** (passage de relais A→B) de bout en bout comme première tranche :
l'agent A termine sa part, signale un relais vers un agent B (un autre profil), et B
reprend **le même travail** en continuant la branche de A. Cette tranche pose le socle
`Coordinator` que superviseur→workers et fan-out réutiliseront.

## Décisions

### D1 — Modèle O : coordinateur au niveau hub, décision côté orchestration
L'agent **signale** (marqueur en fin de réponse). Le **host** détecte le signal après le
tour (là où il appelle déjà `orch.Observe`), le valide, et appelle le `Coordinator`. La
**décision + exécution** appartiennent à la couche orchestration, jamais à l'agent. On
n'ajoute **aucune surface MCP** et on ne change **pas** le contrat de la factory plugin
`Orchestrator`.

### D2 — Worktree : continuation de branche (option ②)
B **continue le travail de A sans merge** en partant du **tip commité de `session/<A>`**.
Réalisation (contrainte du worktreer, vérifiée) : `worktree.Create` fait toujours
`git worktree add -b session/<name>` et ne sait pas réutiliser une branche existante ;
et l'invariant « une session possède sa branche `session/<name>` » (Branch/banner/cleanup)
doit tenir. Donc B obtient **sa propre branche `session/<B>` branchée sur `session/<A>`**
(`git worktree add -b session/<B> session/<A>`), pas la branche de A littéralement. Même
intention : B repart de tout le travail *commité* de A, sans merge, A et B coexistent, la
branche de A survit. Cela suppose un nouveau champ **`Base`** sur `CreateSession` (défaut
vide = comportement actuel) propagé jusqu'à `worktree.Create(repo, name, base)`.

Contrainte assumée : le handoff **exige que A soit commité** (sinon le tip de `session/<A>`
ne porte pas le travail). Si l'arbre de A est sale au moment du relais, le `Coordinator`
**refuse** avec un message clair (miroir du garde existant « commit, or close with
force:true » de `sessionCloseRun`). Rejeté : réutilisation *littérale* de `session/<A>`
(imposerait de fermer A d'abord + nom≠branche, casse l'invariant) ; ① transporter le
worktree vivant (non commité) — chirurgie du cycle de vie session↔worktree, YAGNI pour
cette tranche ; ③ swap de persona sur la même session — salit le scoping mémoire P1
(`agents/<agent>` change en cours de session) + le journal.

### D3 — Contexte transféré : rien de neuf
Le contexte durable circule **déjà** par le scope projet partagé de P1
(`projects/<name>`, `RecordShared`/`RecallScoped`). La **tâche** (`Task`) seede le
**prompt d'ouverture** de B via le canal d'entrée existant : après `Create`, le host
enfile un frame `{T:"input", Who:"handoff", Text: Task}` dans la queue du `sessionDriver`
de B (`core/host/turnloop.go`, même chemin que les messages de gateway). Aucun nouveau
canal de contexte.

### D4 — Marqueur de handoff : trailer unique validable
L'agent exprime l'intention par **une ligne-trailer** en fin de réponse :

```
⟢ handoff: <agent> — <description de la tâche>
```

Le host la parse et la valide. **Malformé / agent inconnu / arbre sale → aucun handoff
silencieux** : soit refus explicite remonté dans le canal de A, soit ignoré **et loggé**.
On ne devine jamais une intention à moitié formée.

### D5 — Socle réutilisable
`HandoffRequest` + le port `Coordinator` sont pensés pour que superviseur→workers (une
session lead qui émet N handoffs et attend) et fan-out (N handoffs parallèles + join)
soient des **politiques par-dessus le même mécanisme**, pas de nouvelles machines.

## Architecture

```
tour d'agent A ─▶ host (bridge/gatewayhub, boucle de tour)
                    │  1. orch.Observe(prompt, reply)            (inchangé)
                    │  2. parseHandoff(reply) ──▶ HandoffRequest?  (NOUVEAU)
                    ▼
              Coordinator.Handoff(ctx, req)          (NOUVEAU, porté par le hub)
                    │  a. résout le profil ToAgent (Store) ; inconnu → erreur
                    │  b. vérifie que le worktree de A est commité ; sale → refus
                    │  c. hub.Create(CreateSession{Agent:ToAgent, Project,
                    │        Base: "session/<A>"}) → B sur session/<B> ⊂ session/<A>
                    │  d. enfile {T:"input", Text: Task} dans la queue de B (seed)
                    ▼
              renvoie le nom de session de B ; A commite + ferme
```

### Composants (unités isolées)
- **`herrscher-contracts`** — `HandoffRequest{FromSession, ToAgent, Task string}` +
  port `Coordinator interface { Handoff(ctx, HandoffRequest) (string, error) }`.
  Source de vérité partagée, comme `CreateSession`. *Dépend de : rien.*
- **Hub `Coordinator`** (`core/host`) — implémente `Handoff` : résolution de profil →
  garde « commité » → `hub.Create` sur la branche de A + seed du prompt. *Dépend de :
  `Store` (profils), le worktreer (état commité), `hub.Create`.* Réutilise l'existant ;
  peu de code neuf.
- **Parse + hook host** (`core/bridge` / `core/host/gatewayhub`) — `parseHandoff(reply)
  (*HandoffRequest, bool)` pur + l'appel au `Coordinator` après `Observe`. *Dépend de :
  le `Coordinator` (port), rien d'autre.* Le parseur est pur et testable seul.

## Flux de données
1. A répond ; sa réponse contient le trailer `⟢ handoff: scripter — …`.
2. Le host appelle `orch.Observe` (inchangé) puis `parseHandoff(reply)`.
3. Marqueur valide → `Coordinator.Handoff(from=A, to=scripter, task=…)`.
4. Le hub résout `scripter` (Store) ; vérifie que le worktree de A est commité ;
   `hub.Create` B avec `Base: "session/<A>"` (→ `session/<B>` branchée sur le tip de A) ;
   enfile `task` comme frame d'entrée d'ouverture de B.
5. B démarre sur `session/<B>` (continuation de A) ; A commite + ferme (branche conservée).

## Gestion d'erreur
- **Agent inconnu** → `Handoff` renvoie une erreur ; remontée dans le canal de A, pas de
  session créée.
- **Worktree de A sale** → refus explicite « commite d'abord (ou force) », aucune création.
- **Marqueur malformé** → ignoré **et loggé** (jamais de handoff deviné).
- **`hub.Create` échoue** → erreur remontée ; A reste ouverte (rien de partiel laissé).

## Tests (fakes, pas de vrai backend LLM)
- `parseHandoff` : trailer valide → `HandoffRequest` ; absent/malformé → `(_, false)` ;
  variations d'espaces/casse cadrées.
- `Coordinator.Handoff` : appelle `Create` avec `Base == "session/<A>"` et enfile la tâche
  (worktreer/Store/hub fakes) ; renvoie le nom de B.
- `worktree.Create` avec `base != ""` : la commande git porte `-b session/<B> session/<A>`
  (fake exec/enregistreur d'args) ; `base == ""` → comportement actuel inchangé.
- Garde « commité » : worktree de A sale → refus, aucune création.
- Agent inconnu → erreur, aucune création.
- Intégration boucle-de-tour : réponse avec trailer → une session B est créée avec la
  tâche enfilée ; réponse sans trailer → aucune session créée.

## Critères d'acceptation
- Une réponse d'agent portant le trailer de handoff crée une session B **branchée sur le
  tip commité de `session/<A>`** (`session/<B> ⊂ session/<A>`), seedée avec la tâche, sans merge.
- A sale → refus clair ; agent inconnu → erreur ; marqueur malformé → ignoré + loggé.
- Aucune surface MCP ajoutée ; contrat de la factory plugin `Orchestrator` inchangé.
- `go test ./...` vert sur les repos touchés ; `gofmt` propre (CI herrscher).
- Le port `Coordinator` est réutilisable tel quel par superviseur→workers et fan-out.

## Hors périmètre (YAGNI)
- Superviseur→workers et fan-out (tranches suivantes, même `Coordinator`).
- Worktree ① (transport du non-commité).
- Profils d'agents spécialistes **Roblox** (contenu Neublox, chantier séparé).
- Attente/agrégation de résultats (le handoff est un relais : A se termine, pas de join).
- Toute décision de routage à **jugement LLM** côté coordinateur (ici le signal vient de
  l'agent ; un coordinateur à politique LLM est une évolution possible, hors tranche).
