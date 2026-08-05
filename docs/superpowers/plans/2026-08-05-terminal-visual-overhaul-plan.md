# Plan — le terminal dit ce qu'il est

Spec : `docs/superpowers/specs/2026-08-05-terminal-visual-overhaul-design.md`.

Sept tâches, dans l'ordre. Chacune laisse l'arbre compilable et la suite verte : on peut
s'arrêter après n'importe laquelle sans laisser le TUI à moitié refait. L'ordre suit la spec
— les rôles d'abord, la peinture ensuite.

## T1 — Les rôles sémantiques

`tui.go`, `render.go`.

- Ajouter les rôles `thinking`, `tool`, `notice`, `error` à côté des existants.
- `renderInto` : un `case "thinking"` (le bug — l'événement était jeté) ; les lignes
  synthétiques `(turn reset)` / `(turn abandoned)` deviennent `notice` ; une ligne de statut
  préfixée par le préfixe d'erreur du host devient `error`.
- `renderEntry` devient un pur aiguillage vers une fonction par rôle. `renderTool` disparaît.
- `toolVerb(text) (verb, rest string)` : premier mot testé contre un vocabulaire fermé
  (read/edit/write/bash/grep/glob/task/webfetch/websearch, insensible à la casse) ; sinon
  `("", text)`.

Tests : `toolVerb` sur chaque verbe, un inconnu, une ligne vide, une ligne d'un seul mot ;
un cas de rendu par rôle ; un événement `thinking` produit bien une entrée.

Vérif : `go test ./plugins/terminal/...`.

## T2 — Le moteur markdown

`markdown.go` (nouveau), `render.go`.

- `go get github.com/charmbracelet/glamour`.
- `renderMarkdown(text string, width int) string`, avec un renderer construit une fois et
  réutilisé (glamour recompile son style à chaque construction — le faire par frame serait
  visible).
- Le rôle `agent` passe par lui **sauf** quand `entry.streaming` est vrai : un bloc en cours
  d'arrivée est du markdown incomplet et se rend brut jusqu'à `endStream`.

Tests : titre, liste, span inline, bloc de code ; un bloc de code non clos en streaming
ressort brut ; le même contenu à deux largeurs donne deux rendus (verrouille le re-wrap).

## T3 — Les diffs

`markdown.go`.

- Intercepter les blocs dont la langue est `diff` avant glamour, les rendre à la main :
  `+` en `greenStyle`, `-` en `redStyle`, `@@` en dim, le reste au texte.
- C'est le premier site d'appel de `greenStyle` / `redStyle`, morts jusqu'ici.

Tests : un bloc `diff` colore ses trois classes de ligne ; un bloc `go` ne passe pas par ce
chemin.

## T4 — La palette Herrscher

`theme.go`.

- Remplacer les constantes de couleur : froid dominant (cyan, violet), accent rouge Herrscher,
  vert/rouge sémantiques, gris de fond. Sortir de `#d97757`.
- Un style nommé par rôle du transcript, plus les styles de chrome.
- Les glyphes suivent : chevrons pour la chrome, une puce par famille d'outil.

Tests : les styles existants ne régressent pas (les tests de rendu de T1 vérifient le glyphe
et le rôle, pas le hex — ils doivent passer sans être touchés). C'est le garde-fou qui rend
cette tâche sûre.

## T5 — La bannière, les onglets, l'état vide

`chrome.go` (nouveau), `tui.go`.

- Bannière `──◂ HERRSCHER ▸──` + les onglets, chacun avec son nom et sa pastille de non-lu.
- Séparateur au-dessus du composer.
- État vide : session, projet, vendor, et les gestes utiles — rendu quand l'onglet actif n'a
  aucune entrée.
- `applySize` / `chromeHeight` doivent compter ces lignes, sinon le viewport déborde.

Tests : un modèle sans entrée rend un état vide non vide ; la bannière contient le nom de
l'onglet actif ; `chromeHeight` augmente de la hauteur de la chrome.

## T6 — La barre de statut

`status.go` (nouveau), `tui.go`.

- Par onglet : coût **cumulé** (aujourd'hui `lastCost` est écrasé à chaque tour), occupation
  du contexte dérivée de `TokensIn + CacheRead + CacheCreate` du dernier tour, début de session.
- `contextLimit(vendor) int` : constante par vendor, surchargée par env.
- Rendu : session · projet · vendor · `42k/200k` avec jauge par paliers (calme <50 %,
  avertissement ≥80 %, alarme ≥95 %) · coût cumulé · durée. Sous la largeur seuil : session +
  contexte seulement.

Tests : l'occupation dérivée des trois compteurs ; les trois paliers ; le cumul sur trois
tours ; la réduction sous la largeur seuil.

## T7 — Finalisation

- Relecture complète : CI, architecture, performance, qualité, sécurité, bugs.
- Retirer les commentaires devenus inutiles ; vérifier qu'aucun style n'est déclaré sans site
  d'appel (c'est le défaut d'origine — ne pas le réintroduire).
- Mettre à jour la page `plugins/terminal` du site docs (fr + en).
- Supprimer cette spec et ce plan une fois la PR mergée.

## Ce qui pourrait mal tourner

- **glamour impose son propre style de fond.** S'il ne se laisse pas assez configurer pour
  suivre la palette de T4, on garde son rendu de structure et on surcharge ses couleurs via
  un `StyleConfig` — pas de fork, pas de renderer maison.
- **Le coût par frame.** Le rendu markdown est plus lourd que `lipgloss.Width().Render()`. Le
  cache de transcript (`tsCache`) existe déjà et se déclenche sur un vrai changement de
  contenu ou de largeur, pas sur une frame d'animation — c'est lui qui rend T2 tenable. Si le
  profil dit le contraire, on met en cache le rendu par entrée close plutôt que par transcript.
- **Le parseur de verbe se trompe.** Une prose qui commence par « Read » sur une ligne de
  statut serait typée à tort. Le coût est cosmétique et le vocabulaire est fermé ; on ne
  cherche pas mieux tant que `contracts` ne porte pas le nom d'outil.
