# Spec 03 — Audit continuous-improvement du host (herrscher core)

## Contexte
Tout l'écosystème périphérique a été audité (contracts, orchestrator, discord-gateway,
obsidian-memory, claude/codex/cursor backends, dctl, transport, llm-extractor). Le host
`herrscher` lui-même — son cœur `core/` (service, supervisor, state, worktree, obs, …),
`manage/`, `plugins/terminal` — n'a PAS encore été passé au crible. C'est le plus gros
morceau (21 paquets de test) et le candidat naturel pour la prochaine passe.

## Repo cible
`/home/shan/dev/herrscher`, périmètre : le code du host lui-même (`core/`, `manage/`,
`plugins/terminal/`, fichiers racine `*.go`). NE PAS auditer les dépendances externes
(elles ont leur propre cycle).

## Axes (mêmes que les passes précédentes — PAS de nouvelles features)
security · performance · code-quality · bug-review · dead-comments · doc-drift.

## Méthode
1. Brancher off master : `git checkout -b improve/host-core-audit`.
2. Analyser le code du host paquet par paquet sur les 6 axes. Vérifier chaque finding
   de façon adversariale (essaie de le RÉFUTER) avant de le retenir — on ne veut pas de
   faux positifs.
3. Séparer en deux catégories :
   - **safe-fixes** : mécaniques, sans changement de comportement (commentaires morts,
     format, doc, resserrage de type trivial, prélocations évidentes, wrap d'erreurs `%w`
     manifestement corrects). → À APPLIQUER.
   - **proposals** : tout ce qui touche au comportement, à la concurrence, à la sécurité,
     à une API. → NE PAS appliquer, seulement rapporter.
4. Appliquer UNIQUEMENT les safe-fixes.

## Contraintes
- Aucune nouvelle feature. Aucun changement de comportement dans les safe-fixes.
- Préserver l'API publique et la compatibilité (le host est consommé par personne mais
  ses plugins/contrats doivent rester stables).
- Idiomes Go : erreurs `%w`, pas de fuite de secret/stderr dans les strings d'erreur,
  `exec.CommandContext` + annulation via ctx, pas de shadowing de builtins, préloc slices.
- Si un finding est un faux positif ou trop risqué → SKIP avec justification. Reporting
  fidèle : ne jamais maquiller un skip en fix.

## Gate (doit être vert avant tout commit, depuis /home/shan/dev/herrscher)
```
GOWORK=off gofmt -l .
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test ./...   # 21 pkgs ok, 0 fail
```

## Livrables
1. Un commit sur `improve/host-core-audit` avec les safe-fixes :
   `fix(host): apply continuous-improvement safe-fixes (host core audit)`.
2. Un rapport markdown `docs/improvement/2026-07-16-host-core.md` listant :
   - les safe-fixes appliqués (fichier:ligne, avant/après en une ligne) ;
   - les **proposals** triées par sévérité (axe, fichier:ligne, problème, suggestion) ;
   - les findings skippés + raison.
   Committe le rapport dans le même commit ou un commit `docs:` séparé.
NE PAS push, NE PAS merger, NE PAS tag — laisse la branche + le rapport pour revue.
