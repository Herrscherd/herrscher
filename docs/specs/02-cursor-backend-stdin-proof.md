# Spec 02 — Prouver la livraison du prompt sur stdin (cursor-backend)

## Contexte
`herrscher-cursor-backend` v0.1.1 a supprimé la double-livraison du prompt : celui-ci
n'est plus passé en argv, uniquement sur `cmd.Stdin` (voir `runCmd` dans `backend.go`
et `Respond` dans `stream.go`). Le contrat a été vérifié sur papier (README
`cursor-agent -p --output-format json` sans arg positionnel, parité `claude -p`), mais
il n'existe aucun test hermétique qui le PROUVE, et `cursor-agent` n'est pas installé
sur cette machine — donc pas de smoke-test live possible.

## Objectif
Ajouter un test d'intégration **hermétique** (sans binaire externe réel) qui prouve que :
1. le prompt composé (contenu + contexte mémoire + attachements) arrive bien sur stdin ;
2. le prompt n'apparaît PAS dans les arguments de la ligne de commande (non-régression
   sécurité /proc/cmdline + ARG_MAX) ;
3. la sortie JSON du process est correctement parsée en réponse.

## Repo cible
`/home/shan/dev/herrscher-cursor-backend`.

## Approche (stub binary, pas de mock d'exec)
Dans un `*_test.go`, écrire à l'exécution un petit exécutable stub dans un `t.TempDir()`
et l'utiliser comme `cmd` de la Config. Deux options, choisis la plus simple qui marche
sur Linux :
- un script shell `#!/bin/sh` qui : lit tout stdin, et émet sur stdout un événement JSON
  cursor valide dont le champ résultat contient un marqueur + un hash/longueur du stdin
  reçu ; écrit aussi `"$@"` (ses argv) dans un fichier du TempDir pour que le test
  assert qu'aucun argv ne contient le prompt ;
- ou un mini-programme Go compilé via `go build` dans le TempDir (plus portable, mais
  plus lourd — le shell suffit sur la CI Linux).

Le test appelle `NewBackend` en mode oneshot avec `cmd` = le stub, envoie un `Prompt`
avec un `Content` distinctif + un `Context` non vide (pour couvrir `withContext`), puis :
- assert que la valeur de retour reflète le stdin reçu par le stub (preuve stdin) ;
- lit le fichier d'argv capturé et assert qu'AUCUN argument ne contient le `Content`
  ni le texte de contexte (preuve : pas de fuite en argv) ;
- assert que les flags attendus (`-p`, `--output-format json`, `--model` si fourni)
  SONT présents en argv.

Faire de même, ou un test jumeau, pour le chemin `stream` (`Respond` de `streamResponder`)
si réalisable hermétiquement ; sinon documenter pourquoi et couvrir au moins le oneshot.

## Portée — NE PAS faire
- Aucun changement de comportement du backend. C'est un ajout de TEST uniquement.
- Ne modifie `backend.go`/`stream.go` QUE si le code n'est pas testable sans une petite
  couture (ex. rendre `baseArgv` accessible au test) — dans ce cas, couture minimale,
  pas de changement de comportement, et justifie-la.

## Gate (depuis /home/shan/dev/herrscher-cursor-backend)
```
GOWORK=off gofmt -l .
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test ./...   # les tests existants (6) + les nouveaux, tous verts
```

## Critères d'acceptation
- Nouveau(x) test(s) qui échouerai(en)t si le prompt repassait en argv (garde anti-régression).
- Tests verts, gate 100% vert.

## Livrable
Branche `test/cursor-stdin-proof`, commit `test(cursor-backend): prove prompt delivered on stdin, not argv`.
Après gate vert : tag patch **v0.1.2** et note-le dans le rapport. NE PAS push (laisser au reviewer).
