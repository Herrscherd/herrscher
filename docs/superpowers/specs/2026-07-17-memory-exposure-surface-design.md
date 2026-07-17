# Surface d'exposition mémoire de herrscher (issue vskstudio/Neublox#44)

**Date** : 2026-07-17
**Statut** : design validé, prêt pour plan d'implémentation
**Issue** : [vskstudio/Neublox#44](https://github.com/vskstudio/Neublox/issues/44)

## Problème

Le cockpit Neublox (onglet Mémoire) veut deux actions réelles sur un fait/skill :
« Ouvrir dans le vault » et « Oublier ». La tentation a été de coder le chemin
du vault en dur dans le shell Tauri de l'app (`~/.herrscher/memory`), ce qui
**casse la frontière d'adaptateur** : la mémoire appartient à herrscher ;
l'app ne doit jamais deviner son stockage interne.

Décision de l'issue : **herrscher doit exposer sa mémoire par une surface
dédiée**, que Neublox consomme.

## Décision d'architecture

Neublox-daemon (Rust) atteint herrscher en **exec du binaire `herrscher`**
(pas de gRPC daemon↔daemon pour cette surface). La forme idiomatique est donc
un **verbe opérateur `memory`** dans le registre CLI existant
(`core/host/cli.go`, `contracts.New(...).Do(...)`), au même titre que
`session` / `agent` routés par `main.go` via `runRegistryVerb`.

Le port `contracts.Memory` reste **storage-neutral** (le contrat dit
explicitement « says nothing about files »). Localiser une note est couplé au
stockage → on n'ajoute PAS `Reveal` au port. On le modélise en **capability
optionnelle type-assertée**, exactement comme `Provisioner` et `CurationHook`
vivent déjà à côté du port. `Delete` (pour « oublier ») suit le même patron.

## Répartition (3 repos Go, un seul chantier cohérent)

| Repo | Travail |
|---|---|
| `herrscher-contracts` | Définir les capabilities `Locator` et `Deleter` à côté du port `Memory`. |
| `herrscher-obsidian-memory` | Implémenter `Locate`/`Delete` sur `ObsidianMemory`. |
| `herrscher` (ce repo) | Verbes `memory locate/forget/record` dans le registre + routage `main.go`. |

Repos hors périmètre de ce spec (côté Neublox, autre chantier) : neublox-daemon
(Op qui exec le verbe) et l'app (réactive le bouton, retire `disabled`).

## Contrats

### herrscher-contracts — `memory.go`

```go
// Locator est une capability OPTIONNELLE : une mémoire adossée à des fichiers
// (le vault obsidian) sait localiser la ressource d'un Key et la rendre
// ouvrable par un humain. Une mémoire non-fichier ne l'implémente pas ; le
// caller type-assert et dégrade proprement — même patron que Provisioner.
type Locator interface {
	// Locate renvoie les URIs ouvrables de la note du Key, ou une erreur si le
	// Key n'existe pas. Au moins un des deux champs est non vide.
	Locate(ctx context.Context, key string) (Location, error)
}

// Location porte les manières d'ouvrir une note. Obsidian est le lien humain
// préféré ; File est le fallback direct (éditeur/OS par défaut).
type Location struct {
	Obsidian string // "obsidian://open?vault=<vault>&file=<key>", "" si indisponible
	File     string // "file:///abs/chemin.md", toujours renseigné pour un vault fichier
}

// Deleter est une capability OPTIONNELLE : retirer un nœud par Key (« oublier »).
type Deleter interface {
	// Delete retire le nœud au Key. Idempotent : un Key absent n'est pas une erreur.
	Delete(ctx context.Context, key string) error
}
```

### herrscher-obsidian-memory

`ObsidianMemory` implémente déjà `contracts.Memory` et `Provisioner`, et
possède `validKey`, `keyToRel`/`keyToPath`, et un `*os.Root`. On ajoute :

- `Locate(ctx, key)` :
  1. `validKey(key)` sinon erreur ;
  2. vérifie l'existence via le `*os.Root` (`stat` du `.md`), sinon erreur `not found` ;
  3. `File` = `file://` + chemin absolu (`filepath.Join(root.Name(), keyToRel(key))`) ;
  4. `Obsidian` = `obsidian://open?vault=<vaultName>&file=<key>`, où `vaultName`
     = basename du root, chaque composant `url.QueryEscape`.
- `Delete(ctx, key)` : `validKey`, puis suppression atomique via le `*os.Root`
  (invalide le `parseCache`), idempotent si absent.
- Preuves à la compilation : `var _ contracts.Locator = (*ObsidianMemory)(nil)`
  et `var _ contracts.Deleter = (*ObsidianMemory)(nil)`, comme le fait déjà
  `Provisioner`.

### herrscher (ce repo)

`core/host/cli.go` — dans `buildRegistry`, enregistrer un verbe `memory` avec
trois sous-commandes. Le handler construit le plugin mémoire **in-process**
depuis `contracts.Default.Memories()` (obsidian-memory est compilé-in via
`plugins.go`) — la logique de `firstMemory` (`pluginhost.go`), qu'on factorise
en un helper partagé `buildFirstMemory(ctx) (contracts.Memory, error)`.

- `memory locate <key>` : type-assert `Locator` ; sortie JSON
  `{"obsidian":"...","file":"..."}` (le mode JSON du registre est déjà géré via
  `in.JSON`). Capability absente → erreur claire « memory backend ne supporte
  pas locate ».
- `memory forget <key>` : type-assert `Deleter` ; appelle `Delete`.
- `memory record` : mappe sur `Memory.Record`. Flags : `--key` (requis),
  `--kind` (requis, valeur d'une `NodeKind` du contrat), `--title`, `--body`.
  MVP minimal ; les champs `Links`/`Meta` sont différés (non exposés par le
  verbe pour l'instant).

`main.go` — `runMemory(ctx, args) → runRegistryVerb(ctx, "memory", args)`
(copie de `runAgent`) + branchement dans le switch des verbes.

## Flux de bout en bout

```
app (bouton « Ouvrir dans le vault »)
  → invoke Op neublox-daemon
    → exec `herrscher memory locate <key>` (JSON)
      → registre construit ObsidianMemory in-process, type-assert Locator
        → renvoie {obsidian, file}
  ← app : shell-open obsidian:// ; fallback file:// si Obsidian absent
```

## Gestion d'erreurs

- Key invalide / absent → exit non-zéro + message stderr ; l'app garde le bouton
  inerte plutôt que d'ouvrir un chemin mort.
- Backend mémoire sans capability → erreur explicite (pas un panic, pas un
  silence). Aucune autre mémoire fichier n'existe aujourd'hui, mais le contrat
  reste honnête pour un futur backend non-fichier.
- `forget` idempotent : rejouer sur un Key déjà oublié réussit.

## Tests

- **contracts** : les interfaces sont des déclarations ; un stub d'assertion
  suffit (comme `recMemory` prouve `Memory`).
- **obsidian-memory** : `Locate` sur une note semée (vérifie `file://` pointe
  le bon `.md` + `obsidian://` bien encodé) ; `Locate` sur Key absent → erreur ;
  `Delete` supprime + idempotence ; sécurité : un Key d'évasion (`../…`) est
  rejeté par `validKey`.
- **herrscher** : test de registre driving `memory locate --json` sur un vault
  temporaire (env `OBSIDIAN_VAULT`), asserte le JSON ; `memory forget` retire ;
  capability absente → erreur (mémoire stub sans Locator).

## Hors périmètre (YAGNI)

- Pas de `Reveal` sur le port `Memory`.
- Pas de gRPC pour cette surface (l'accès est par exec CLI).
- « Rejouer » et l'édition riche de faits (Links/Meta complets) : différés,
  pas nécessaires pour débloquer le bouton de l'issue #44.
