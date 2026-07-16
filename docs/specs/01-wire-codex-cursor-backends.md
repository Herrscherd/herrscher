# Spec 01 — Câbler les backends codex & cursor dans le host

## Contexte
`herrscher-codex-backend` (v0.1.0) et `herrscher-cursor-backend` (v0.1.1) sont publiés
et s'auto-enregistrent via `init()` → `contracts.Register(...)` (voir leur `register.go`,
Kind `"codex"` / `"cursor"`, Category `CategoryBackend`). Ils ne sont PAS encore importés
par le host `herrscher`, donc invisibles au binaire. Cette spec les rend sélectionnables.

## Repo cible
`/home/shan/dev/herrscher` (module `github.com/Herrscherd/herrscher`).

## Contrainte de sécurité NON-NÉGOCIABLE
Tout module importé dans `plugins.go` DOIT être un repo **public** : le CI du host fetch
sans authentification, un module privé fait rougir master. `herrscher-codex-backend` et
`herrscher-cursor-backend` sont publics (org Herrscherd) — OK. Ne JAMAIS importer
`neublox-extractor` ni aucun module privé.

## Travail
1. Brancher off master : `git checkout -b feat/wire-codex-cursor-backends`.
2. Ajouter les deux dépendances aux versions publiées :
   ```
   GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-codex-backend@v0.1.0
   GOFLAGS=-mod=mod go get github.com/Herrscherd/herrscher-cursor-backend@v0.1.1
   ```
3. Ajouter deux blank imports ENTRE les marqueurs `// herrscher:plugins` et
   `// herrscher:end` de `plugins.go`, en respectant l'ordre alphabétique existant :
   ```go
   _ "github.com/Herrscherd/herrscher-claude-backend"
   _ "github.com/Herrscherd/herrscher-codex-backend"
   _ "github.com/Herrscherd/herrscher-cursor-backend"
   _ "github.com/Herrscherd/herrscher-discord-gateway"
   ...
   ```
   (Si une commande `herrscher plugin add <module>` existe et fait exactement ça, tu peux
   l'utiliser à la place de l'édition manuelle — mais vérifie le diff résultant.)
4. `GOFLAGS=-mod=mod go mod tidy`.

## Portée — NE PAS faire
- Aucune nouvelle feature, aucun changement de comportement des backends.
- Ne touche qu'à `go.mod`, `go.sum`, `plugins.go`. Rien d'autre.

## Gate (doit être vert, depuis /home/shan/dev/herrscher)
```
GOWORK=off gofmt -l .          # aucune sortie
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test ./...       # 21 pkgs ok, 0 fail
```

## Critères d'acceptation
- `plugins.go` contient les deux nouveaux blank imports entre les marqueurs.
- `go.mod` require `herrscher-codex-backend v0.1.0` et `herrscher-cursor-backend v0.1.1`.
- Le binaire compile et expose les backends `codex` et `cursor` à la sélection (mêmes
  Kind que dans leur `register.go`).
- Gate 100% vert.

## Livrable
Un commit sur la branche `feat/wire-codex-cursor-backends` :
`feat(plugins): wire codex & cursor backends into host`. NE PAS push, NE PAS merger,
NE PAS tag — laisse la branche locale pour revue.
