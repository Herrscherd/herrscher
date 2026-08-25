# Skills auto-écrites

Date : 2026-08-25
Statut : design validé, plan à écrire
Axe : **A** de la roadmap Hermes v2 (les trois autres : agents proactifs, exécution
isolée, approbation de commandes)

## Le problème

Herrscher tient trois briques qui ne se touchent pas.

`core/skills/` découvre des `SKILL.md` sur disque (`<worktree>/.claude/skills`,
`~/.claude/skills`), injecte un menu nom + description à chaque tour, et détend le
corps complet quand le modèle émet `<use-skill>NOM</use-skill>`. C'est de la
divulgation progressive, elle marche sur tous les backends, et elle ne lit que des
fichiers que quelqu'un d'autre a écrits.

L'orchestrateur tient la mémoire. Son extracteur émet des `Candidate{Node, Private}`
où `Private: true` veut dire, dans son propre commentaire, « une compétence apprise
qui reste avec cet agent ». Ces candidates atterrissent sous `agents/<a>/…` via
`RecordPrivate`. `conscious.go` donne déjà au modèle des marqueurs en bande,
`<remember>` et `<recall>`, traités dans `React`.

`promote.go` sait déjà faire traverser `agents/<a>/skills/<n>` vers
`projects/<p>/skills/<n>`.

Donc une compétence apprise existe, mais **c'est du texte récité dans le digest de
recall**. Elle n'est jamais un fichier. Elle n'apparaît pas dans le menu de skills,
ne bénéficie pas de la divulgation progressive, ne peut pas pointer un dossier de
ressources, et consomme du budget de prompt à chaque tour qu'elle serve ou non.

Hermes, lui, documente « autonomous skill creation from experience » et « skill
self-improvement during use ». C'est le chaînon manquant, et c'est le dernier gap
de la couche apprentissage.

## Ce qu'on construit

Une skill apprise est **un nœud `KindSkill`** dans le vault, sous
`agents/<a>/skills/<n>`. Le disque n'en est qu'une projection, réécrite au
démarrage de session.

```
  <skill> (en bande)  ─┐
                       ├─►  RecordPrivate(KindSkill, skills/<n>)
  extracteur (hors bande)─┘                │
                                           │  bridge, au démarrage de session
                                           ▼
                  <worktree>/.herrscher/skills/<n>/SKILL.md
                                           │
                                           ▼
                     skills.Discover ─► Menu ─► <use-skill> ─► Expansions
                                           │
                           eng.Detect(reply) ─► lastSeen rafraîchi sur le nœud
```

Le vault est la vérité parce que toute la mécanique du premier axe (G1 à G7) est
écrite pour des nœuds : le vieillissement, la fusion sémantique, la promotion
cross-agent, l'archivage réversible, l'audit et `memory restore`. Une skill qui vit
dans le vault hérite des six gratuitement. Une skill qui vit sur disque obligerait
à toutes les réécrire.

### La projection

Elle est écrite **par le bridge**, pas par `Agent.Materialize`. `core/internal/agent`
ne dépend aujourd'hui que de la bibliothèque standard et ne connaît que des
fichiers ; lui donner le port mémoire lui ferait perdre sa neutralité. Le bridge,
lui, tient déjà l'orchestrateur, connaît son cwd et construit lui-même le moteur de
skills.

Elle court dans `runHub`, **juste avant `newSkillEngine`**, pour que le menu du
premier tour contienne déjà les skills apprises.

Une fois par session, pas à chaque tour. Conséquence assumée : une skill écrite par
`<skill>` au tour N n'apparaît dans le menu qu'à la session suivante. C'est le bon
compromis. L'agent qui vient d'écrire une procédure la connaît, la lui reservir
dans le menu du tour d'après ne lui apprend rien ; et re-rendre le disque à chaque
tour coûterait une requête mémoire par tour pour un contenu presque toujours
identique.

Ce qu'elle projette, **dans les deux portées** :

- les skills privées de l'agent, sous `agents/<a>/skills/` ;
- les skills partagées du projet, sous `projects/<p>/skills/`.

La seconde n'est pas un détail, c'est la raison d'être de la promotion : une skill
approuvée doit arriver sur le disque des *autres* agents du projet, sinon
`promote.go` ne fait que déplacer une clé. En cas de collision de nom entre les
deux portées, la privée gagne, parce qu'un agent qui a raffiné sa propre version
d'une procédure partagée voulait sa version.

Un nœud dont l'état est `stale` ou `archived` n'est pas projeté, et le rendu efface
le fichier d'une skill qu'il ne projette plus, sinon une skill archivée
continuerait de vivre sur le disque d'une session déjà ouverte. Le vieillissement
serait alors une étiquette sans effet, ce qui est pire que pas de vieillissement du
tout.

### Une racine qui n'appartient qu'à la projection

Ce dernier point interdit d'écrire dans `<worktree>/.claude/skills/`. Un rendu qui
efface ce qu'il ne projette plus, dans un dossier que l'opérateur peut aussi
remplir à la main, finit par supprimer le travail de quelqu'un. Et distinguer ses
propres fichiers de ceux d'un tiers par un marqueur dans le frontmatter, c'est
faire reposer une suppression sur une heuristique.

La projection reçoit donc une racine à elle, `<worktree>/.herrscher/skills/`,
qu'elle possède entièrement : elle est seule à y écrire, donc elle peut y effacer
sans se poser de question. `/.herrscher/` rejoint `materializedGitExcludes`.

`skillRoots` devient, dans cet ordre :

1. `<worktree>/.claude/skills`, les skills du dépôt ;
2. `<worktree>/.herrscher/skills`, les skills apprises, projetées ;
3. `~/.claude/skills`, les playbooks de la machine ;
4. les racines supplémentaires de la config.

`Discover` dédoublonne déjà par nom, racine antérieure gagnante. Cet ordre dit donc
trois choses d'un coup, et ce sont exactement les trois qu'on veut : **une skill du
dépôt bat toujours une skill apprise** (une procédure auto-écrite ne peut pas
masquer celle que le projet a committée), une skill apprise bat un playbook global
(l'expérience de cet agent sur ce projet est plus spécifique qu'une instruction de
machine), et rien de ce qu'écrit la projection ne peut détruire un fichier
qu'elle n'a pas écrit.

## Répartition par module

| Module | Ce qui change |
|---|---|
| contracts | `KindSkill NodeKind = "skill"`. Rien d'autre. |
| obsidian | rien. Il stocke des nœuds sans savoir de quel genre. |
| llm-extractor | rien. Le Learner normalise, voir §« Normalisation ». |
| orchestrator | marqueur `<skill>`, normalisation, seam `LearnedSkills`, seam `Used`, exclusion du digest, garde de promotion, seam `ApproveSkill`. |
| host | projection dans le bridge, quatrième racine dans `skillRoots`, `/.herrscher/` dans `materializedGitExcludes`, remontée d'usage, famille de verbes `skill`. |

`contracts.Orchestrator` ne gagne **aucune méthode**. Chaque nouvelle capacité est
un seam optionnel découvert côté hôte par assertion de type, exactement comme
`SetScope`, `Start`, `Consolidator` et `Merger` avant elle. Les métadonnées
d'approbation restent des constantes internes à l'orchestrateur, comme
`MetaPromotedTo` et `MetaMergedInto` l'ont fait avant elles.

## Les trois boucles

### 1. Écriture consciente

Un troisième marqueur rejoint `<remember>` et `<recall>` dans `conscious.go` :

```
<skill name="retry-http">
Quand une requête sortante renvoie 429, attendre le Retry-After ...
</skill>
```

Traité dans le même `React`, avec les mêmes règles que ses deux voisins : la
regex est tolérante à la casse et aux espaces et matche sur plusieurs lignes,
l'écriture est best-effort, le marqueur est retiré de la réponse pour que
l'humain ne le voie jamais, et un échec mémoire ne casse pas le tour.

Le préambule mémoire gagne une phrase qui annonce le marqueur, sinon aucun
modèle ne l'émettra jamais.

Un `name` absent ou qui ne se normalise pas en un segment de clé valide fait
ignorer le marqueur. Un `name` déjà connu **remplace** le corps : `Record` est un
upsert par clé, donc réécrire sa propre skill est la façon dont l'agent la révise,
et c'est ce que « self-improvement during use » veut dire ici.

### 2. Écriture hors bande

L'extracteur émet déjà des candidates privées. On ne touche pas au module.

**Normalisation** : le Learner stampe `Kind = KindSkill` sur toute candidate
`Private: true` dont la clé est sous `skills/`. La décision appartient à la
plomberie, pas à l'extracteur, pour deux raisons : le module d'extraction est le
morceau fermé du moat et on ne veut pas qu'une politique de genre y vive, et une
candidate privée peut légitimement être un *fait* privé plutôt qu'une procédure.
Le préfixe de clé est le discriminant.

Les deux boucles partagent le même écrivain : une seule fonction sait fabriquer un
nœud de skill, stamper ses métadonnées et l'enregistrer.

### 3. Amélioration à l'usage

`eng.Detect(reply)` sait déjà exactement quelles skills ont été activées pendant
un tour. Le bridge remonte ces noms à l'orchestrateur par un seam optionnel, qui
rafraîchit `MetaLastSeen` sur les nœuds correspondants.

Deux conséquences tombent toutes seules, et c'est le cœur du design :

- **Le sweep archive la skill que personne n'active et laisse vivre celle qui
  sert.** Une skill inutile meurt d'elle-même, réversiblement, sans qu'on écrive
  une ligne de politique de rétention.
- **`promoteEligible` exige déjà que `lastSeen` ait dépassé `capturedAt` d'au
  moins `promote-min-age-days`.** Une skill écrite une fois et jamais activée ne
  devient donc jamais promouvable. Le critère d'éligibilité existant devient, sans
  modification, « cette skill a servi ».

## Le double paiement dans le prompt

Un nœud `KindSkill` est projeté sur disque et annoncé dans le menu. S'il reste
visible du digest de recall, il est payé deux fois dans le même prompt : une fois
en entier dans le digest, une fois en résumé dans le menu.

Le digest de l'orchestrateur exclut donc `KindSkill` de ce qu'il récite. C'est le
même geste que G7 a fait pour `KindTranscript`, à un étage différent : G7 a mis la
porte dans le stockage parce qu'un transcript brut ne doit atteindre personne, ici
la porte est dans le digest parce qu'une skill doit rester trouvable par
`memory search` et par `memory restore`, elle ne doit simplement pas être récitée.

## Frontière de confiance

Une skill auto-écrite est du texte que l'agent exécutera au tour suivant et qui
survit à la session. Son contenu vient du journal, où se trouvent des messages de
chat, des fichiers de dépôt et des pages web. Herrscher tient déjà les transcripts
bruts pour attaqueur-contrôlés.

La règle : **privé libre, partagé approuvé.**

Une skill s'écrit et se révise seule dans `agents/<a>/skills/` sans rien demander.
Le rayon d'action est l'agent qui l'a apprise, et c'est déjà sa mémoire.

`promoteEligible` refuse un nœud `KindSkill` qui ne porte pas la marque
d'approbation. C'est le seul endroit du système où le rayon d'action change, donc
c'est là et nulle part ailleurs que la frontière est tenue. Le reste de
`promote.go` ne bouge pas : le mapping de clé, la copie réversible et les
back-pointers marchent déjà pour une clé sous `skills/`.

Côté hôte, deux verbes :

- `herrscher skill list` montre les skills apprises, leur portée, leur état, la
  date de dernier usage, et si elles sont approuvées.
- `herrscher skill approve <nom>` pose la marque. `--revoke` la retire, ce qui
  n'annule pas une promotion déjà faite (celle-là se défait avec `memory unlink`
  et `memory restore --force`, qui existent) mais empêche les suivantes.

## Invariants

1. **Ports uniquement.** `contracts.Orchestrator` ne gagne aucune méthode, aucun
   type concret ne traverse le cœur, et les tests de pureté restent verts.
2. **L'apprentissage ne casse jamais le tour.** Écriture de skill, stamp d'usage
   et projection sur disque sont tous best-effort. Une erreur est loguée en WARN
   et le tour continue.
3. **Réversible.** Une skill n'est jamais supprimée, seulement étiquetée. Le
   `memory restore` existant la ramène.
4. **Rien ne franchit la frontière privé → partagé sans approbation humaine.**
5. **Une skill n'est jamais payée deux fois.** Projetée sur disque implique
   exclue du digest.

## Gestion d'erreur

| Situation | Comportement |
|---|---|
| Vault injoignable au démarrage de session | Aucune projection écrite, le moteur découvre les skills sur disque comme aujourd'hui, WARN. |
| Un nœud rend un `SKILL.md` invalide | `Discover` le saute déjà en silence. On écrit un frontmatter bien formé et on n'écrit rien du tout si le nom ne se normalise pas. |
| Une skill projetée porte le nom d'une skill du dépôt | Le dépôt gagne, par l'ordre des racines. Une skill apprise ne peut pas masquer une skill committée. À verrouiller par un test : c'est une propriété de sécurité, pas un détail d'ordonnancement. |
| Une skill privée et une skill promue portent le même nom | La privée gagne. Une seule des deux est rendue, donc `Discover` ne voit jamais le conflit. |
| Un fichier traîne dans la racine de projection sans nœud correspondant | Effacé au rendu suivant. C'est permis parce que cette racine n'appartient qu'à la projection. |
| Écriture concurrente sur la même clé | `Record` est un upsert, dernier écrivain gagnant, comme pour les faits. |
| L'agent émet `<skill>` avec un corps vide | Ignoré. Une skill vide viderait la précédente par upsert. |

## Configuration

Le triplet habituel, éteint par défaut, dans le style des axes précédents :

- `skills.learned` / `MEMORY_LEARNED_SKILLS`, booléen, défaut `false`. Éteint, ni
  le marqueur ni la normalisation ni la projection n'existent, et la phrase du
  préambule n'est pas émise.
- Le vieillissement, la fusion et la promotion réutilisent les réglages existants
  (`AGENT_STALE_DAYS`, `AGENT_ARCHIVE_DAYS`, `MEMORY_PROMOTE_MIN_AGE_DAYS`). On
  n'ajoute pas de seuils parallèles pour les skills : deux jeux de seuils qui
  peuvent diverger, c'est une source de bug et pas un réglage.

## Tests

Côté orchestrateur, table-driven, sans réseau ni LLM (un extracteur factice) :

- `React` reconnaît `<skill>`, écrit sous la bonne clé et le bon genre, dépouille
  la réponse, et ne casse pas le tour quand la mémoire renvoie une erreur.
- Un `name` vide, non normalisable, ou un corps vide n'écrivent rien.
- Un second `<skill>` de même nom remplace le corps sans créer de doublon.
- La normalisation stampe `KindSkill` sur une candidate privée sous `skills/` et
  laisse tranquille une candidate privée qui n'y est pas.
- Le stamp d'usage avance `lastSeen` et rend éligible à la promotion un nœud qui
  ne l'était pas, et un nœud jamais activé reste inéligible.
- `promoteEligible` refuse une skill non approuvée et accepte une skill approuvée
  par ailleurs éligible.
- Le digest ne récite aucun `KindSkill`, et `Search` les trouve toujours.

Côté hôte :

- La projection écrit un `SKILL.md` que `skills.Discover` reparse en un `Skill`
  dont le nom et la description sont ceux du nœud (aller-retour complet).
- Les deux portées sont projetées, et une skill privée masque une skill promue de
  même nom.
- Un nœud `stale` ou `archived` n'est pas projeté, et son fichier disparaît au
  rendu suivant.
- La projection n'écrit ni n'efface **rien** en dehors de sa propre racine : un
  fichier planté dans `<worktree>/.claude/skills` survit à un rendu complet. C'est
  le test qui garde la propriété non destructive.
- `skillRoots` rend l'ordre attendu, et une skill du dépôt du même nom gagne sur
  une skill apprise, qui gagne elle-même sur un playbook global.
- Un vault injoignable laisse la session démarrer avec les skills du disque.
- Les verbes `skill list` et `skill approve` répondent sans démon là où les
  verbes mémoire le font déjà, et refusent un nom inconnu en le nommant.
- La suite hôte complète reste verte, et les tests de pureté avec.

## Hors périmètre v1

Assumé et à ne pas glisser dedans en cours de route :

- Pas de ressources embarquées (scripts, données) dans une skill apprise. Le
  corps d'un nœud est du texte. Le jour où ça manque, c'est un second axe qui
  ajoute un dossier de ressources à côté du nœud, avec la question de durée de
  vie qui va avec.
- Pas de réécriture automatique du corps par le learner. On ne sait pas mesurer
  « la skill a marché », et une révision automatique fondée sur rien serait une
  dégradation silencieuse. La révision est consciente, par upsert.
- Pas de Skills Hub communautaire ni de dépôt de skills partagé entre machines.
- Pas d'approbation automatique par seuil d'usage. L'usage rend éligible,
  l'humain fait traverser. Les deux gardes sont indépendantes exprès.

## Suite

Les trois autres axes de la roadmap, dans l'ordre : agents proactifs (cron et
réveils, zéro planification dans le code aujourd'hui), exécution isolée ou
distante (Docker, SSH, hibernation), approbation de commandes.
