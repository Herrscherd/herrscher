# Le terminal dit ce qu'il est

## Le problème

Le TUI est un clone de Claude Code — la doc de `theme.go` le dit elle-même, « Claude-parity
visual language », et `colAccent` vaut `#d97757`, l'orange d'Anthropic. On a pris la forme
sans prendre ce qui la rend lisible.

Trois constats, tous vérifiables dans le code d'aujourd'hui :

- **Au repos, l'écran est muet.** Pas de bannière, pas de nom de session, pas de projet, pas
  de séparateur au-dessus du composer. Un terminal ouvert sur Herrscher n'annonce même pas
  ce qu'il est.
- **La couleur ne porte aucune information.** Quatre rôles de style sont utilisés ;
  `greenStyle`, `redStyle` et `selStyle` sont déclarés et n'ont **aucun site d'appel**. Une
  lecture de fichier, une écriture et un échec sortent tous en `● texte` orange.
- **La prose de l'agent est imprimée brute.** L'agent écrit du markdown ; `renderEntry` le
  passe à `textStyle` tel quel. Pas de titres, pas de listes, pas de blocs de code, pas de
  spans inline. Aucune dépendance de rendu dans le `go.mod`.

Et un bug : **les événements `thinking` sont jetés.** `core/bridge/hub.go` les émet
(`emitBackendEvent`, `case "thinking"`), le `switch` de `renderInto` n'a pas de cas pour eux.
Le résumé de réflexion — ce que Claude affiche en « Cogitated for 34m 16s » — n'atteint jamais
l'écran.

## Ce qu'on construit

Une refonte du rendu du terminal en quatre couches, dans cet ordre, parce que chacune est la
fondation de la suivante : des **rôles sémantiques** dans le transcript, un **moteur de rendu
markdown**, une **palette Herrscher** projetée sur ces rôles, et une **chrome** (bannière,
onglets, état vide, barre de statut).

L'ordre est le point de la spec. Repeindre avant d'avoir des rôles à colorer ne produirait
qu'un autre mur d'une seule teinte.

## Où ça vit

Entièrement dans `plugins/terminal/tui/`. Aucun changement de `contracts`, aucune release de
repo tiers, une seule PR. Tout ce dont la barre de statut a besoin est déjà sur le fil et
jeté à l'arrivée : `Event` porte `Cost`, `Tokens`, `TokensIn`, `CacheRead`, `CacheCreate`,
`Agent` ; `SessionInfo` porte `Vendor`, `Project`, `Dir`. Le TUI n'en lit que `Cost` et
`Tokens`.

## Ce que le fil ne porte pas

À écrire noir sur blanc, parce que ça borne ce que le rendu peut promettre :

- **L'issue d'un outil.** `BackendEvent` porte `Tool` et `Detail` — l'invocation, jamais son
  résultat. `IsError` appartient à `result`, donc au tour entier. Pas de ✓/✗ par ligne
  d'outil, pas de durée par outil.
- **Le nom de l'outil, structurellement.** `hub.go:282` aplatit `Tool + " " + Detail` en une
  chaîne avant d'émettre. Le gateway reçoit `"Read core/parse.go"` comme texte opaque.
- **Le diff d'une édition.** `Detail` est « the salient input field », c'est-à-dire le chemin.
  Le contenu du patch n'est nulle part.
- **Le nom du modèle et la taille de la fenêtre.** `Vendor` donne `claude`, pas `opus`.

On fait donc avec : le verbe d'outil est **parsé** du premier mot côté TUI, les diffs sont
rendus dans les blocs ```` ```diff ```` de la prose, et la fenêtre de contexte a un
dénominateur par vendor, en constante, surchargeable par env.

Porter l'issue, la durée et le nom d'outil jusqu'aux gateways demande un champ de plus sur
`contracts.Event` et une release de la chaîne. C'est une suite, délibérément hors périmètre :
elle améliorerait ce rendu, elle n'est pas nécessaire pour qu'il existe.

## Les quatre couches

### 1. Des rôles sémantiques

`entry.role` vaut aujourd'hui `you` / `agent` / `status` / `cost` / `scrollback`, et `status`
avale tout ce qui n'est ni une question ni une réponse : outils, réinitialisation, abandon,
sortie de commande. Un seul rôle, quatre intentions différentes, un seul rendu.

On le découpe :

| Rôle | Ce qu'il porte | D'où il vient |
|------|----------------|---------------|
| `you` | le tour de l'opérateur | inchangé |
| `agent` | la prose de l'agent | inchangé, mais rendu en markdown (§2) |
| `thinking` | le résumé de réflexion | `case "thinking"`, aujourd'hui jeté |
| `tool` | verbe + cible | `status`, verbe parsé du premier mot |
| `notice` | tour réinitialisé / abandonné | `status` synthétisé par `renderInto` |
| `error` | une ligne qui commence par le préfixe d'erreur du host | `status` / résultat de dispatch |
| `cost` | le coût du tour | inchangé |
| `scrollback` | l'historique rejoué | inchangé |

`renderTool` disparaît : chaque rôle a sa fonction de rendu, et `renderEntry` n'est plus
qu'un aiguillage. Le parseur de verbe (`toolVerb`) reconnaît un vocabulaire fermé — lecture,
écriture, édition, shell, recherche, web, sous-agent — et rend le reste sous une puce
générique. Un verbe inconnu doit rester lisible, jamais disparaître.

### 2. Le rendu markdown

`glamour` (Charm, comme le reste de la pile) rend titres, listes, gras, spans de code inline
et blocs de code coloriés via `chroma`. Deux dépendances qui portent à elles seules la moitié
de l'écart avec Claude.

Trois contraintes :

- **Le re-wrap à la largeur reste à nous.** Le TUI stocke du texte logique et re-wrappe à
  chaque redimensionnement (`renderTranscript`) ; glamour prend une largeur en entrée, donc
  il est appelé depuis le même chemin et le cache de transcript (`tsCache`) le couvre déjà.
- **Le streaming.** Un bloc de prose en cours d'arrivée est du markdown incomplet — une
  clôture ```` ``` ```` peut manquer. Un bloc `streaming` est rendu brut ; il ne passe par
  glamour qu'une fois clos par `endStream`. Ça évite qu'un demi-bloc de code clignote.
- **Les diffs.** glamour ne colore pas un patch. Un bloc dont la langue est `diff` est
  intercepté et rendu à la main, `+` en vert, `-` en rouge — c'est là que `greenStyle` et
  `redStyle` reprennent enfin du service.

### 3. La palette Herrscher

On sort de l'orange d'Anthropic. Froid dominant, cyan et violet ; un accent rouge Herrscher
réservé à ce qui demande une action de l'opérateur ; vert et rouge sémantiques pour le diff
et l'erreur ; le gris de fond pour tout ce qui est secondaire.

Deux règles de tenue :

- **Le rôle reste porté par le glyphe et la position**, pas seulement par la couleur. C'est
  déjà la règle du fichier actuel et elle est la seule raison pour laquelle un terminal 16
  couleurs, ou un opérateur daltonien, garde un écran lisible.
- **Un accent, pas cinq.** Le rouge Herrscher signale ; s'il est partout il ne signale plus.

La chrome est anguleuse (chevrons, filets fins) ; le corps du transcript reste calme. L'identité
vit dans les bords, parce qu'on lit la prose pendant des heures et pas la bannière.

### 4. La chrome

- **Bannière** : `──◂ HERRSCHER ▸──` suivie des onglets, chacun avec son nom de session et sa
  pastille de non-lu.
- **État vide** : ce qu'on voit avant d'avoir tapé quoi que ce soit — la session, le projet,
  le vendor, et les trois ou quatre gestes qui servent. Aujourd'hui : rien.
- **Séparateur** au-dessus du composer, pour que la saisie cesse de flotter dans le noir.
- **Barre de statut** : session · projet · vendor · occupation du contexte avec jauge colorée
  · coût cumulé de la session · durée. Sous une largeur seuil elle se réduit à session +
  contexte, comme le fait Hermes sous 52 colonnes.

L'occupation du contexte se dérive de `TokensIn + CacheRead + CacheCreate` du dernier tour :
c'est la taille du prompt réellement envoyé, donc ce que la fenêtre porte. Le dénominateur est
une constante par vendor surchargeable par env, et la jauge se colore par paliers (calme sous
50 %, avertissement à 80 %, alarme à 95 %).

Le coût cumulé est la somme des `Cost` reçus sur la session, gardée par onglet : aujourd'hui
on écrase `lastCost` à chaque tour, si bien qu'une session de six heures affiche le prix du
dernier tour.

## Ce qui ne change pas

Le seam `Backend`, le routage par conversation, les onglets et leurs raccourcis, la palette
de commandes, la mention `@`, les pièces jointes et l'aperçu kitty, le rappel d'historique,
`Esc` pour interrompre. Rien de la logique ; tout du rendu.

Et rien de Discord : le gateway Discord ne voit aucun de ces changements, puisqu'ils vivent
sous `plugins/terminal/tui/`.

## Hors périmètre

Le mode shell `!`, la redirection en cours de tour, `Ctrl+G` vers `$EDITOR`, `/context` et
`/usage`. Ce sont des fonctionnalités, pas du rendu. Elles méritent leur spec une fois
celle-ci posée — et la barre de statut de §4 est ce qui les rendra utiles.

## Les tests

- `toolVerb` : chaque verbe connu, un verbe inconnu, une ligne vide, une ligne sans espace.
- `renderEntry` : un cas par rôle, chacun vérifiant le glyphe et le style, pas la couleur brute.
- markdown : un titre, une liste, un span inline, un bloc de code, un bloc `diff` (les `+`
  en vert et les `-` en rouge), et un bloc de code **non clos** en cours de streaming rendu
  brut.
- re-wrap : le même contenu à deux largeurs, pour verrouiller que glamour est bien rappelé
  au redimensionnement.
- `thinking` : un événement `thinking` produit une entrée (test de régression du bug trouvé).
- barre de statut : occupation dérivée des trois compteurs, les paliers de couleur, le coût
  cumulé sur trois tours, et la réduction sous la largeur seuil.
- état vide : un modèle sans entrée rend quelque chose de non vide.
