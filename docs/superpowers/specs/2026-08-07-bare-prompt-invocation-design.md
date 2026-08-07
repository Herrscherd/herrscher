# `herrscher "<texte libre>"` — invocation par prompt nu

## Problème

`herrscher 'Va lire ça … on veut implémenter ça'` répond `unknown command`.

Tout argv que `main.go` ne reconnaît pas part au daemon (`dispatchUnknown`,
`main.go:201`) ; si le daemon ne le connaît pas non plus, l'invocation meurt sur
`unknown command %q`. Un texte libre finit donc toujours là, alors que c'est la
façon la plus naturelle de confier une tâche au host depuis un terminal — celle
que `claude "<prompt>"` offre déjà.

Le socle existe pourtant en entier : `session create` monte un worktree isolé,
`session seed` (`core/host/cli.go:101`) fait tourner un tour et rend la réponse,
et les deux se font forwarder au daemon quand il tourne
(`core/host/operator.go:25`). Il ne manque que le chemin qui va du texte à ces
deux verbes.

## Ce qu'on construit

`herrscher "<texte>"` crée une session dédiée, y fait tourner un tour, imprime
la réponse, et laisse la session en place pour la suite.

```
$ herrscher "Va lire le thread Guide de l'aventurier et propose un découpage"
session: va-lire-le-thread-guide-a3fq          (stderr)
<réponse de l'agent>                            (stdout)

$ herrscher session list
  va-lire-le-thread-guide-a3fq   claude/opus   session/va-lire-le-thread-guide-a3fq
```

En prime, le `main` codé en dur de la TUI disparaît : la session que la TUI
ouvre à l'arrivée prend elle aussi un nom généré.

## Hors périmètre

- Le streaming. `session seed` rend la réponse finale ; on imprime ça. Les
  événements intermédiaires (thinking/chunks) restent l'affaire de la TUI.
- Toute notion de « session courante » ou de continuité entre deux invocations.
  Chaque prompt ouvre sa session ; enchaîner se fait avec `session seed --name`.
- Le worktree n'est pas nettoyé automatiquement. La session est persistante,
  `session close` reste le geste explicite.

---

## 1. Détection du prompt

Un nouveau `promptOf(cmd string, args []string) (string, bool)` dans le package
`main`, appelé depuis `main()` juste après le découpage `cmd`/`args` et **avant**
le switch des verbes de gestion.

| argv | décision |
|---|---|
| `herrscher "lis le thread X"` | prompt (l'argument contient une espace) |
| `herrscher "lis le thread" en entier` | prompt : `lis le thread en entier` |
| `herrscher -p refactor` | prompt : `refactor` (mot seul forcé) |
| `herrscher --prompt refactor` | idem |
| `herrscher sesion` | **pas** un prompt → `unknown command` (inchangé) |
| `herrscher session list` | **pas** un prompt → verbe (inchangé) |
| `herrscher -x` | **pas** un prompt → erreur de flag (inchangé) |

Règles, dans cet ordre :

1. `cmd` vaut `-p` ou `--prompt` → le prompt est `args` joint par des espaces.
   Un `-p` sans texte est une erreur explicite, pas un prompt vide.
2. `cmd` commence par `-` → pas un prompt. Les flags gardent leur chemin.
3. `cmd` contient une espace, une tabulation ou un saut de ligne, et n'est pas
   vide une fois taillé → prompt : `cmd` puis `args`, joints par des espaces.
4. Sinon → pas un prompt.

La règle 3 est sûre parce qu'aucun verbe du registre ne contient d'espace :
`contracts.New(…)` prend des segments, et le registre les dispatche segment par
segment. Un verbe futur à espace serait déjà cassé côté socket ; `-p` reste la
sortie de secours si ça arrivait.

`promptOf` est une fonction pure, testable sans daemon ni process — même forme
que `dispatchUnknown`, qui prend `cmd` et `args` séparés précisément pour être
exerçable avec un forwarder bouchon.

## 2. Nom de session

`sessionNameFor(prompt string) string`, package `main`.

- Les 5 premiers mots du prompt, minuscules, tout ce qui n'est pas
  `[a-z0-9_-]` réduit à un `-`, tirets de tête et de queue enlevés.
- Tronqué à 40 caractères (puis re-taillé, pour ne pas finir sur un `-`).
- Suffixe `-` + 4 caractères aléatoires minuscules, tirés de `rand.Text()`
  (crypto/rand).
- Un prompt dont rien d'utilisable ne survit (emojis seuls, CJK) donne `s-<suffixe>`.

Le suffixe n'est pas décoratif : `session create` refuse un nom déjà pris
(`core/internal/manager/session.go:373`), et deux prompts qui commencent pareil
se marcheraient dessus. Il rend aussi le nom insensible au contenu exact, ce qui
évite qu'un prompt joue sur le nom d'une branche git.

Le résultat est déjà un slug valide au sens de `sessionNameRe`
(`core/internal/manager/validate.go:13`) ; `session create` le re-slugifie de
toute façon, et reste le garde-fou final. Le package `main` ne peut pas importer
`core/internal/manager`, donc la dérivation vit chez lui — mais elle produit une
entrée que le validateur accepte telle quelle, jamais une entrée qu'il devrait
réparer.

## 3. Exécution du prompt

`runPrompt(ctx context.Context, prompt string) error` dans `serve.go`, à côté de
`runRegistryVerb`.

`runRegistryVerb` construit aujourd'hui un registre puis dispatche un seul verbe.
On en extrait `newOperatorRegistry(ctx) (*cli.Registry, error)` — construction du
registre seule — que `runRegistryVerb` et `runPrompt` partagent. Un prompt
dispatche deux verbes et ne doit pas payer deux constructions de passerelle.

Séquence :

1. `session create --name <slug> --terminal_only`
   Worktree isolé, branche `session/<slug>`, aucun channel passerelle : le
   channel est un `terminal/…` local, donc la commande n'exige aucun home
   configuré.
2. `session: <slug>` sur **stderr**, pour que `herrscher "…" > out.md` ne
   contienne que la réponse.
3. `session seed --name <slug> --task <prompt> --timeout <durée>` → réponse sur
   **stdout**.

Si l'étape 1 échoue, on s'arrête là et on rend son erreur. Si l'étape 3 échoue,
la session reste : elle porte un worktree et un début d'état, et la détruire
sous les pieds de l'opérateur lui enlèverait ce qu'il faut pour comprendre
l'échec. L'erreur nomme la session pour qu'il puisse la reprendre ou la fermer.

## 4. Deux trous que ce chemin ouvre

### 4.1 L'admin terminal manque au CLI opérateur

`SetTerminalAdmin` n'est appelé que par le daemon (`core/host/serve.go:229`).
`NewRegistry` — le registre du process CLI court — ne le câble pas. Sans daemon,
un `--terminal_only` retombe donc sur le home passerelle et échoue sur
`no home set — run set home first`, alors qu'il n'a besoin d'aucun home.

C'est un trou préexistant : `herrscher session create --terminal_only` est déjà
cassé daemon éteint. Le chemin prompt le rend simplement inévitable.

Correction : `NewRegistry` prend `gws []Deps` au lieu d'un `Deps` unique, et en
tire `adminForHome(gws, st.Home)` et `terminalAdmin(gws)` — exactement ce que
`RunHub` fait déjà de son côté (`core/host/serve.go:218` et `:229`). Les deux
chemins deviennent symétriques au lieu de diverger silencieusement.

Côté appelant, `buildGateway` rend aujourd'hui `hub.First()`. Il devient
`buildGateways` et rend l'ensemble des sets construits ; `runRegistryVerb` les
passe à `NewRegistry`. Les sites de test qui passent `Deps{}` passeront `nil`.

### 4.2 Le tour de seed est plafonné à 120 s

`seedTurnTimeout` (`core/host/seed.go:17`) est une constante à 120 s. Un prompt
du genre de celui qui a motivé ce travail — lire un thread, en tirer un plan —
dépasse ça et rend `seed timeout`.

Le plafond ne peut pas être réglé par l'environnement du CLI : quand un daemon
tourne, le seed est exécuté **chez lui**, et c'est son environnement à lui qui
compterait. Le délai doit donc voyager avec la commande.

`session seed` gagne un `ValueParam("timeout", …, false)` :

- absent → `HERRSCHER_SEED_TIMEOUT` s'il est posé, sinon 120 s (inchangé) ;
- présent → une durée Go (`30m`, `90s`) ; une valeur illisible ou ≤ 0 est
  refusée avant que le tour ne démarre.

`argvOf` (`core/host/operator.go:42`) le transporte tel quel sur le socket, donc
le plafond est le même que le seed tourne ici ou chez le daemon.

`runPrompt` passe `30m`. Un tour lancé à la main depuis un terminal n'a pas
l'urgence d'un tour de coordination, et l'opérateur est là pour l'interrompre.

## 5. La TUI perd son `main`

`ensureDefaultSession` (`plugins/terminal/terminal.go:73`) crée une session
nommée `main` en dur. Elle prend désormais un nom généré, de la même forme que
le suffixe ci-dessus : `s-<4 caractères>`.

Le reste ne bouge pas :

- `Shared: true, TerminalOnly: true` — la TUI travaille dans le checkout
  courant, comme aujourd'hui.
- Le garde-fou « une session terminal existe déjà → ne rien créer » reste. C'est
  lui qui empêche chaque relance de la TUI d'empiler un worktree de plus ; sans
  nom fixe, il devient la seule chose qui borne la création.

Le générateur vit dans le plugin terminal. Le package `main` a le sien
(section 2) et les deux ne peuvent pas s'importer l'un l'autre ; quatre lignes
en double valent mieux qu'un paquet partagé pour un tirage aléatoire.

## 6. Aide

`usage()` (`usage.go:45`) gagne une ligne en tête du groupe DÉMARRER :

```
herrscher "<texte>"    open a session on this task and print the reply
```

## 7. Tests

Chaque unité est exerçable sans daemon, sans backend et sans terminal.

**`promptOf`** — table couvrant les sept lignes du tableau de la section 1, plus
un `-p` nu (erreur), un `cmd` à espaces qui ne laisse rien après taille, et
`herrscher "a b" c` (jonction).

**`sessionNameFor`** — le slug est toujours accepté par la même expression que
`sessionNameRe` ; un prompt long est tronqué et ne finit pas par `-` ; un prompt
sans caractère utilisable donne `s-…` ; deux appels sur le même prompt donnent
deux noms différents.

**`session seed --timeout`** — un timeout illisible est refusé avant le tour ;
absent, la valeur historique s'applique ; `argvOf` le rend bien sur le socket.

**`NewRegistry` avec plusieurs passerelles** — un set contenant une passerelle
`terminal` fait qu'un `session create --terminal_only` n'exige plus de home ;
sans passerelle terminal, le comportement actuel tient.

**`ensureDefaultSession`** — le test existant qui assert `spec.Name != "main"`
(`plugins/terminal/terminal_test.go:352`) devient un test de forme : nom non
vide, accepté par la règle de slug, et différent d'un appel à l'autre. Le test
« une session terminal existe déjà » ne bouge pas.

**`runPrompt`** — exercé à travers un registre bouchon (deux verbes enregistrés
qui enregistrent leur argv) : create précède seed, le slug est le même dans les
deux, le nom part sur stderr et la réponse sur stdout, et un create qui échoue
n'appelle jamais seed.
