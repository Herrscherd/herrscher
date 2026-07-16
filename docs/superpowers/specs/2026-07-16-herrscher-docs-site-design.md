# Herrscher — Site de documentation SvelteKit (design)

**Date :** 2026-07-16
**Statut :** validé, prêt pour le plan d'implémentation
**Livrable :** un nouveau repo `herrscher-docs`, site de documentation statique.

---

## 1. Objectif

Produire une documentation « hyper complète » du projet Herrscher : une
explication poussée du fonctionnement interne, une référence détaillée de tous
les plugins, un guide utilisateur, le tout très visuel (diagrammes Mermaid).
Le README actuel (41 KB) est la source de vérité du contenu ; le site le
restructure, l'approfondit et le rend navigable.

## 2. Décisions

| Axe | Décision |
|-----|----------|
| Framework | **SvelteKit + MDsveX**, `@sveltejs/adapter-static` (site 100 % statique) |
| Emplacement | **repo séparé `herrscher-docs`** (cycle de vie découplé du binaire) |
| Langue | **bilingue FR + EN** (routing `[lang]`, contenu miroir, sélecteur de langue) |
| Style | **CSS maison distinctif**, theme-aware light/dark, accent ambre repris du diagramme d'archi |
| Diagrammes | composant `<Mermaid>` Svelte, rendu client, réactif au thème |
| Contenu plugins | **hybride** : archi/rôle/config depuis le README ; précision des interfaces vérifiée sur le module réel `herrscher-contracts` (déjà dans `go.mod`) — pas de clone des repos plugins |

### Alternatives écartées
- **Astro/Starlight** : le user veut du Svelte.
- **mdBook** : pas de composants Svelte.
- **Mermaid pré-rendu au build** : moins souple pour la réactivité au thème.

## 3. Stack & structure du repo

```
herrscher-docs/
├── package.json            SvelteKit, mdsvex, adapter-static
├── svelte.config.js        mdsvex ({ extensions: ['.svx'] }) + adapter-static
├── vite.config.ts
├── src/
│   ├── lib/
│   │   ├── components/      Mermaid.svelte, Sidebar.svelte, Toc.svelte,
│   │   │                    LangSwitch.svelte, ThemeToggle.svelte,
│   │   │                    Callout.svelte, CodeBlock.svelte
│   │   ├── nav.ts           arborescence de nav (par langue)
│   │   └── styles/          design system maison (tokens, thème light/dark)
│   ├── content/
│   │   ├── fr/  overview/ architecture/ plugins/ guide/ reference/  (*.svx)
│   │   └── en/  (miroir)
│   └── routes/
│       ├── [lang]/+layout.svelte     shell doc (sidebar + contenu + TOC)
│       └── [lang]/[...slug]/+page    résout le .svx correspondant
└── static/                 favicon, og-image
```

Le composant `<Mermaid>` importe `mermaid` dynamiquement à l'`onMount`, rend le
graphe, et re-render sur changement de thème (observe l'attribut de thème racine).

## 4. Architecture d'information (la navigation)

### Section 1 — Overview
- Le mental model : « un seul job : router ».
- Les deux invariants : `contracts` est l'autorité ; toutes les flèches pointent
  vers `contracts`.
- Diagramme d'archi global (le flowchart du README).

### Section 2 — Architecture (le fonctionnement poussé)
Une page par concept :
- **Archi hexagonale & la golden rule** — flèches entrantes, pureté (`TestHostPurity`, `TestCorePurity`).
- **`contracts`** — les ports, types neutres, zéro dep/zéro logique.
- **`core`** — le domaine agnostique (sessions, worktrees, supervision).
- **`host`** — la seule `main()`, composition root + daemon.
- **Le flux d'un message** — sequence diagram hub → bridge → backend → modèle ; FIFO, un turn actif à la fois, `abandoned`, `PendingChoice`/`MenuRouter`.
- **Les deux run modes** — `serve` (hub), `bridge` (pure runner), TUI terminal (tabs, keybindings).
- **Cycle de vie d'une session** — state diagram (Created → Worktree → Channel → Running → Closed), `project:`/`clone:`/`shared:`, `/allow`, `--agent`.
- **Coordination inter-sessions** — les trailers `⟢` (done/delegate/fanout/route/seal/merge/handoff), ordre de priorité, routing déterministe par `TAGS`.
- **Agents durables & mémoire** — home d'agent (`SOUL.md`/`mcp.json`/`settings.json`), provisioning dans un worktree, le vault Obsidian auto-provisionné, scope shared/private, learning (write side, extractor opt-in).
- **Transport distribué** — NATS (discovery) + gRPC (calls), `HERRSCHER_REMOTE`, multi-machine + mTLS fail-closed.

### Section 3 — Plugins (référence)
- **Le modèle de plugins** — 4 catégories, compile-in façon xcaddy, self-register via `init()` → `contracts.Default`, ports optionnels nil + `contracts.Degrade`, gateways plain vs smart (`EventSink`).
- Une page par plugin, structure commune (rôle · port(s) implémenté(s) · config/env · statut · liens repo) :
  - **Gateway** — Discord (`herrscher-discord-gateway`) + terminal in-tree.
  - **Backend** — Claude (`herrscher-claude-backend`), `ChoiceAware`/`ChoiceInjector`.
  - **Memory** — Obsidian (`herrscher-obsidian-memory`).
  - **Orchestrator** — `herrscher-orchestrator` (kind `basic`), `Curator`/`Learner`.
  - **Terminal** — le gateway in-tree (`plugins/terminal`), `Foreground`.
- **Écrire ton plugin** — implémenter un port, blank import entre les marqueurs de `plugins.go`, rebuild ; le diagramme author→build→register→discover.

### Section 4 — Guide / Operations
- Install & build (prérequis, `go build`, PKGBUILD/pacman).
- Quickstart (les run modes en pratique).
- CLI reference — table complète (`serve`/`bridge`/`session`/`agent`/`service`).
- Managing plugins — `init` (catalogue, wizard, config-only), `plugin`/`update`/`install`.
- Configuration — précédence flag→env→config.json→défaut, le `.env` racine, `config.json`.
- Service install — Linux/macOS/Windows, update out-of-band, uninstall.
- Layout & wiring — un module Go, pas de `replace`, `go.work` pour le dev multi-repo.

### Section Reference (pages courtes tabulaires)
- Catalogue des ports `contracts`.
- Variables d'environnement.
- Trailers `⟢`.
- Keybindings TUI.

## 5. Découpage en unités

- **Rendu de contenu** : `[lang]/[...slug]` + import glob des `.svx` → une seule
  route pilote toutes les pages ; testable indépendamment.
- **Navigation** : `nav.ts` déclaratif par langue → `Sidebar`/`Toc`/`LangSwitch`
  consomment cette donnée ; changer la nav ne touche pas le rendu.
- **`<Mermaid>`** : encapsule tout le couplage à la lib mermaid et au thème ;
  interface = un bloc de code mermaid en prop/slot.
- **Design system** : tokens CSS (couleurs, typo, espacements) isolés dans
  `lib/styles` ; les composants ne codent pas de valeurs en dur.

## 6. Vérification (definition of done)

- `npm run build` vert (adapter-static produit le site).
- Toutes les pages de la nav existent et rendent (pas de lien mort).
- Au moins un diagramme Mermaid rendu correctement, vérifié visuellement
  (capture d'écran d'une page architecture).
- Bascule light/dark et FR/EN fonctionnelles.
- Précision des ports plugins vérifiée contre `herrscher-contracts`.

## 7. Hors périmètre (YAGNI)

- Recherche full-text (peut venir après ; pas requis pour v1).
- Versioning multi-release de la doc.
- Déploiement/CI (le repo est séparé ; le déploiement se décidera ensuite).
- Génération auto de la doc depuis le source Go (rédaction manuelle fidèle).
