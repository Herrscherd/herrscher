# Bare-prompt invocation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `herrscher "<texte libre>"` ouvre une session dédiée sur cette tâche, fait tourner un tour et imprime la réponse, au lieu de rendre `unknown command`.

**Architecture:** Le package `main` gagne deux fonctions pures (détection du prompt, dérivation du nom de session) et un `runPrompt` qui dispatche `session create --terminal_only` puis `session seed` à travers le registre CLI existant — donc forwardé au daemon quand il tourne, exécuté localement sinon. Deux trous préexistants sont bouchés au passage : le registre opérateur ne câblait pas l'admin terminal, et le tour de seed était plafonné à 120 s en dur.

**Tech Stack:** Go 1.24+ (`crypto/rand.Text`), `github.com/Herrscherd/herrscher-contracts`, tests `go test` standards (pas de framework tiers, pas de testify).

## Global Constraints

- Spec de référence : `docs/superpowers/specs/2026-08-07-bare-prompt-invocation-design.md`.
- Le package `main` **ne peut pas** importer `core/internal/...` (packages internes à `core/`). Toute dérivation de slug côté `main` est autonome.
- Le host reste agnostique de la passerelle : aucun fichier touché ici ne nomme Discord ni n'importe un adaptateur concret.
- Les commentaires suivent le style du dépôt : ils expliquent *pourquoi*, pas *quoi*. Pas de commentaire qui paraphrase la ligne suivante.
- Tests en Go standard : `func TestXxx(t *testing.T)`, `t.Fatalf`, tables de cas. Pas de `assert`.
- Chaque tâche finit par `go build ./... && go test ./...` vert avant le commit.
- Messages de commit en anglais, format Conventional Commits (le dépôt s'y tient : `feat(host):`, `fix(tui):`, …).

---

## File Structure

| Fichier | Rôle | Tâche |
|---|---|---|
| `prompt.go` *(créé)* | package `main` : `promptOf` (détection) + `sessionNameFor` (nom de session). Deux fonctions pures, aucune I/O. | 1, 2 |
| `prompt_test.go` *(créé)* | tests des deux ci-dessus. | 1, 2 |
| `core/host/seed.go` *(modifié)* | `seedTurnTimeout` devient un plancher par défaut ; `oneShotSeedRuntime` porte le délai ; le forward socket le transporte. | 3 |
| `core/host/seedtimeout.go` *(créé)* | `resolveSeedTimeout` : param → env → défaut. Isolé parce que c'est la seule règle de résolution et qu'elle se teste seule. | 3 |
| `core/host/seedtimeout_test.go` *(créé)* | tests de `resolveSeedTimeout`. | 3 |
| `core/host/cli.go` *(modifié)* | `session seed` gagne `--timeout` ; `NewRegistry` prend `[]Deps` et câble l'admin terminal. | 3, 4 |
| `core/host/registry_gateways_test.go` *(créé)* | `NewRegistry` avec plusieurs passerelles. | 4 |
| `serve.go` *(modifié, package `main`)* | `buildGateway` → `buildGateways` ; extraction de `newOperatorRegistry` ; ajout de `runPrompt`/`runPromptWith`. | 4, 5 |
| `prompt_run_test.go` *(créé)* | test de `runPromptWith` sur un registre bouchon. | 5 |
| `main.go` *(modifié)* | branchement de `promptOf` avant le switch des verbes. | 5 |
| `usage.go` *(modifié)* | une ligne d'aide. | 5 |
| `plugins/terminal/terminal.go` *(modifié)* | `ensureDefaultSession` : nom généré au lieu de `main`. | 6 |
| `plugins/terminal/terminal_test.go` *(modifié)* | le test de nom devient un test de forme. | 6 |

**Ordre :** 1 → 2 → 3 → 4 → 5. La tâche 6 est indépendante et peut être faite à tout moment.

---

### Task 1: Détection du prompt (`promptOf`)

**Files:**
- Create: `prompt.go`
- Test: `prompt_test.go`

**Interfaces:**
- Consumes: rien.
- Produces: `func promptOf(cmd string, args []string) (string, bool)` — rend le texte du prompt et `true` quand l'argv est un prompt libre ; `("", false)` quand c'est un verbe. Un `-p`/`--prompt` sans texte rend `("", true)` : c'est un prompt *demandé* mais vide, et l'appelant (tâche 5) le refuse avec un message clair. Consommée par `main()` en tâche 5.

- [ ] **Step 1: Write the failing test**

Créer `prompt_test.go` :

```go
package main

import "testing"

func TestPromptOf(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		args    []string
		want    string
		wantOK  bool
	}{
		{"espace dans l'argument", "lis le thread X", nil, "lis le thread X", true},
		{"argument à espace puis positionnels", "lis le thread", []string{"en", "entier"}, "lis le thread en entier", true},
		{"tabulation compte comme espace", "lis\tle thread", nil, "lis\tle thread", true},
		{"saut de ligne compte comme espace", "lis\nle thread", nil, "lis\nle thread", true},
		{"-p force un mot seul", "-p", []string{"refactor"}, "refactor", true},
		{"--prompt force un mot seul", "--prompt", []string{"refactor"}, "refactor", true},
		{"-p joint ses arguments", "-p", []string{"lis", "le", "thread"}, "lis le thread", true},
		{"-p nu est un prompt vide", "-p", nil, "", true},
		{"-p blanc est un prompt vide", "-p", []string{"   "}, "", true},
		{"verbe d'un mot", "sesion", nil, "", false},
		{"verbe avec sous-commande", "session", []string{"list"}, "", false},
		{"flag inconnu", "-x", nil, "", false},
		{"flag long inconnu", "--config", []string{"x.json"}, "", false},
		{"argument entièrement blanc", "   ", nil, "", false},
		{"argument vide", "", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := promptOf(tc.cmd, tc.args)
			if ok != tc.wantOK {
				t.Fatalf("promptOf(%q, %v) ok = %v, want %v", tc.cmd, tc.args, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("promptOf(%q, %v) = %q, want %q", tc.cmd, tc.args, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestPromptOf -v`
Expected: FAIL — `undefined: promptOf`

- [ ] **Step 3: Write minimal implementation**

Créer `prompt.go` :

```go
package main

import "strings"

// promptOf decides whether an argv is a free-text task rather than a verb.
//
// The rule is the one thing that keeps `herrscher sesion` an honest typo instead
// of a billed agent turn: a single unrecognised WORD stays a verb, and only text
// that could not be a verb becomes a prompt. No verb in the registry contains a
// space — contracts.New takes path segments and the registry matches them one by
// one — so whitespace is the discriminator, and -p is the escape hatch for the
// one-word prompt the rule necessarily refuses.
//
// A -p with nothing after it returns ("", true): the caller asked for a prompt
// and gave none, which is a mistake worth naming, not an argv to fall through to
// the verb switch.
func promptOf(cmd string, args []string) (string, bool) {
	if cmd == "-p" || cmd == "--prompt" {
		return strings.TrimSpace(strings.Join(args, " ")), true
	}
	if strings.HasPrefix(cmd, "-") {
		return "", false
	}
	if !strings.ContainsAny(cmd, " \t\n\r") {
		return "", false
	}
	text := strings.TrimSpace(strings.Join(append([]string{cmd}, args...), " "))
	if text == "" {
		return "", false
	}
	return text, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestPromptOf -v`
Expected: PASS (15 sous-tests)

- [ ] **Step 5: Commit**

```bash
git add prompt.go prompt_test.go
git commit -m "feat(cli): tell a free-text prompt from a mistyped verb"
```

---

### Task 2: Nom de session dérivé du prompt (`sessionNameFor`)

**Files:**
- Modify: `prompt.go`
- Test: `prompt_test.go`

**Interfaces:**
- Consumes: rien de la tâche 1 (même fichier, fonctions indépendantes).
- Produces: `func sessionNameFor(prompt string) string` — un slug valide, toujours suffixé de 4 caractères aléatoires. Consommée par `runPrompt` en tâche 5.

- [ ] **Step 1: Write the failing test**

Ajouter à `prompt_test.go` :

```go
import (
	"regexp"
	"strings"
	"testing"
)

// sessionNameRe is a copy of the guard in core/internal/manager/validate.go:13.
// The main package cannot import that internal package, so the invariant is
// pinned here instead: whatever sessionNameFor produces must already pass it.
var sessionNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func TestSessionNameForIsAlwaysAValidSlug(t *testing.T) {
	prompts := []string{
		"Va lire le thread Guide de l'aventurier",
		"REFACTOR the whole thing!!!",
		"a",
		"🔥🔥🔥",
		"日本語だけ",
		"   ",
		"",
		strings.Repeat("mot ", 60),
		"--not-a-flag mais du texte",
		"___",
	}
	for _, p := range prompts {
		name := sessionNameFor(p)
		if !sessionNameRe.MatchString(name) {
			t.Fatalf("sessionNameFor(%q) = %q, which the session-name guard rejects", p, name)
		}
	}
}

func TestSessionNameForKeepsTheOpeningWords(t *testing.T) {
	name := sessionNameFor("Va lire le thread Guide de l'aventurier et propose un plan")
	if !strings.HasPrefix(name, "va-lire-le-thread-guide-") {
		t.Fatalf("name = %q, want it to open on the first five words", name)
	}
}

func TestSessionNameForFallsBackWhenNothingSurvives(t *testing.T) {
	name := sessionNameFor("🔥🔥🔥")
	if !strings.HasPrefix(name, "s-") {
		t.Fatalf("name = %q, want the s- fallback when no character is usable", name)
	}
}

func TestSessionNameForIsUniquePerCall(t *testing.T) {
	p := "lis le thread"
	if a, b := sessionNameFor(p), sessionNameFor(p); a == b {
		t.Fatalf("two calls both gave %q; session create refuses a name already taken", a)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestSessionNameFor -v`
Expected: FAIL — `undefined: sessionNameFor`

- [ ] **Step 3: Write minimal implementation**

Ajouter à `prompt.go` (et compléter le bloc d'import : `crypto/rand`, `regexp`, `strings`) :

```go
// nameStemMax bounds the readable part of a session name. The guard downstream
// allows 64 characters and the suffix takes 5 of them; stopping at 40 leaves the
// name legible in `session list` without the branch name running off the line.
const nameStemMax = 40

// nameStemWords is how much of a prompt makes it into the name. Five words is
// enough to recognise the task in a list and short enough that the name does not
// become a paraphrase of the prompt.
const nameStemWords = 5

// slugInvalid matches any run of characters a session name cannot carry. The
// name becomes both a filesystem path and a git ref, which is why the allowed
// set is this narrow.
var slugInvalid = regexp.MustCompile(`[^a-z0-9_-]+`)

// sessionNameFor derives a session name from a free-text prompt.
//
// The random suffix is not decoration. `session create` refuses a name already
// taken, so two prompts opening on the same words would collide — and it also
// stops the prompt's exact wording from deciding a git branch name.
//
// The result already satisfies the session-name guard; `session create`
// slugifies again and stays the final word, but it is never handed something it
// would have to repair.
func sessionNameFor(prompt string) string {
	words := strings.Fields(prompt)
	if len(words) > nameStemWords {
		words = words[:nameStemWords]
	}
	stem := slugInvalid.ReplaceAllString(strings.ToLower(strings.Join(words, " ")), "-")
	if len(stem) > nameStemMax {
		stem = stem[:nameStemMax]
	}
	stem = strings.Trim(stem, "-_")
	if stem == "" {
		stem = "s" // an all-emoji or all-CJK prompt leaves nothing to name it with
	}
	return stem + "-" + strings.ToLower(rand.Text()[:4])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run 'TestSessionNameFor|TestPromptOf' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add prompt.go prompt_test.go
git commit -m "feat(cli): derive a session name from the prompt's opening words"
```

---

### Task 3: `session seed --timeout`

**Files:**
- Create: `core/host/seedtimeout.go`
- Create: `core/host/seedtimeout_test.go`
- Modify: `core/host/seed.go:17` (la constante), `core/host/seed.go:29-33` (le struct), `core/host/seed.go:53-72` (le forward), `core/host/seed.go:118` (l'application)
- Modify: `core/host/cli.go:101-127` (le Do de `session seed`)

**Interfaces:**
- Consumes: rien des tâches précédentes.
- Produces:
  - `func resolveSeedTimeout(raw string, getenv func(string) string) (time.Duration, error)` — `0` signifie « non réglé, garder le plafond historique ».
  - `const EnvSeedTimeout = "HERRSCHER_SEED_TIMEOUT"`
  - le champ `timeout time.Duration` sur `oneShotSeedRuntime`.
  - le paramètre `--timeout` de `session seed`, consommé par `runPrompt` en tâche 5.

- [ ] **Step 1: Write the failing test**

Créer `core/host/seedtimeout_test.go` :

```go
package host

import (
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func TestResolveSeedTimeoutUnsetMeansZero(t *testing.T) {
	got, err := resolveSeedTimeout("", noEnv)
	if err != nil {
		t.Fatalf("resolveSeedTimeout: %v", err)
	}
	if got != 0 {
		t.Fatalf("got %v, want 0 — an unset timeout must leave the historical cap alone", got)
	}
}

func TestResolveSeedTimeoutParamWins(t *testing.T) {
	env := func(k string) string {
		if k == EnvSeedTimeout {
			return "5s"
		}
		return ""
	}
	got, err := resolveSeedTimeout("30m", env)
	if err != nil {
		t.Fatalf("resolveSeedTimeout: %v", err)
	}
	if got != 30*time.Minute {
		t.Fatalf("got %v, want 30m", got)
	}
}

func TestResolveSeedTimeoutFallsBackToEnv(t *testing.T) {
	env := func(k string) string {
		if k == EnvSeedTimeout {
			return "45s"
		}
		return ""
	}
	got, err := resolveSeedTimeout("", env)
	if err != nil {
		t.Fatalf("resolveSeedTimeout: %v", err)
	}
	if got != 45*time.Second {
		t.Fatalf("got %v, want 45s", got)
	}
}

func TestResolveSeedTimeoutRefusesBadValues(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		env  string
	}{
		{"param illisible", "bientôt", ""},
		{"param nul", "0s", ""},
		{"param négatif", "-1m", ""},
		{"env illisible", "", "bientôt"},
		{"env nul", "", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := func(k string) string {
				if k == EnvSeedTimeout {
					return tc.env
				}
				return ""
			}
			if _, err := resolveSeedTimeout(tc.raw, env); err == nil {
				t.Fatalf("resolveSeedTimeout(%q, env=%q) accepted a value it must refuse", tc.raw, tc.env)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/host -run TestResolveSeedTimeout -v`
Expected: FAIL — `undefined: resolveSeedTimeout`, `undefined: EnvSeedTimeout`

- [ ] **Step 3: Write minimal implementation**

Créer `core/host/seedtimeout.go` :

```go
package host

import (
	"fmt"
	"time"
)

// EnvSeedTimeout overrides the default cap on a one-shot seed turn for every
// seed that does not name its own.
const EnvSeedTimeout = "HERRSCHER_SEED_TIMEOUT"

// resolveSeedTimeout settles how long a seed turn may run: the command's own
// --timeout first, then the environment, then zero — which means "say nothing"
// and leaves seedTurnTimeout in force.
//
// Zero and negative durations are refused rather than clamped. A cap of 0s reads
// as "no limit" to whoever typed it and would in fact cancel the turn before the
// backend answers, which is the opposite; an operator deserves to hear that at
// the point they typed it, not as a turn that dies instantly.
func resolveSeedTimeout(raw string, getenv func(string) string) (time.Duration, error) {
	source, value := "--timeout", raw
	if value == "" {
		source, value = EnvSeedTimeout, getenv(EnvSeedTimeout)
	}
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a duration (try 90s or 30m)", source, value)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %s", source, d)
	}
	return d, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/host -run TestResolveSeedTimeout -v`
Expected: PASS

- [ ] **Step 5: Carry the timeout through the seed runtime**

Dans `core/host/seed.go`, remplacer la constante ligne 17 :

```go
// seedTurnTimeout is the cap a seed turn runs under when nothing names another.
// A coordination seed is a short question; a turn started by hand from a terminal
// is not, which is what --timeout / HERRSCHER_SEED_TIMEOUT exist for.
const seedTurnTimeout = 120 * time.Second
```

Ajouter le champ au struct `oneShotSeedRuntime` (ligne 29-33) :

```go
type oneShotSeedRuntime struct {
	coordinator contracts.Coordinator
	publish     func(session string, event contracts.Event)
	record      func(session string, entry state.TranscriptEntry)
	// timeout caps this turn; zero keeps seedTurnTimeout. It rides the runtime
	// rather than a parameter because the seed path already threads exactly one
	// per-request value through these three calls, and a second one would make
	// every call site unreadable.
	timeout time.Duration
}
```

Dans `runOneShotSeedCommand`, le bloc de forward (lignes 64-70) devient :

```go
	if runtime.coordinator == nil && forward != nil {
		argv := []string{"session", "seed", "--name", name, "--task", task, "--turn_id", turnID}
		// Only a settled timeout travels. The daemon runs this turn, so a silent
		// caller must leave the daemon's own default in force — sending a resolved
		// value unconditionally would make this process's environment decide a cap
		// for a turn it does not run.
		if runtime.timeout > 0 {
			argv = append(argv, "--timeout", runtime.timeout.String())
		}
		if reply, handled, err := forward(ctx, CommandSocketPath(instID), argv); handled {
			return reply, err
		}
	}
```

Dans `runOneShotSeedWithIDRuntime`, la ligne 118 devient :

```go
	turnTimeout := runtime.timeout
	if turnTimeout <= 0 {
		turnTimeout = seedTurnTimeout
	}
	seedCtx, cancel := context.WithTimeout(ctx, turnTimeout)
```

- [ ] **Step 6: Expose the parameter on the command**

Dans `core/host/cli.go`, le bloc `session seed` (lignes 101-127). Ajouter le paramètre après `turn_id` :

```go
		ValueParam("timeout", "cap this turn (Go duration, e.g. 30m); default HERRSCHER_SEED_TIMEOUT then 120s", false).
```

et, dans le `Do`, juste après la résolution du turn id :

```go
			timeout, err := resolveSeedTimeout(in.Get("timeout"), os.Getenv)
			if err != nil {
				return "", err
			}
			runtime := oneShotSeedRuntimeFrom(cmdCtx)
			runtime.timeout = timeout
			reply, err := runOneShotSeedCommand(
				cmdCtx, st, in.Get("name"), in.Get("task"),
				turnID, runtime,
				seedCoord.coord, instID, dispatchLiveCommand,
			)
```

(remplace l'appel existant qui passait `oneShotSeedRuntimeFrom(cmdCtx)` en ligne.)

- [ ] **Step 7: Run the full host suite**

Run: `go build ./... && go test ./core/host`
Expected: PASS. `TestRunOneShotSeedCommandForwardsToLiveCoordinator` (`core/host/seed_test.go:184`) doit rester vert : il passe `oneShotSeedRuntime{}`, donc `timeout` vaut 0 et aucun `--timeout` ne part sur le socket.

- [ ] **Step 8: Commit**

```bash
git add core/host/seedtimeout.go core/host/seedtimeout_test.go core/host/seed.go core/host/cli.go
git commit -m "feat(host): let a seed turn name its own timeout"
```

---

### Task 4: `NewRegistry` voit toutes les passerelles

**Files:**
- Modify: `core/host/cli.go:367-409` (signature + câblage)
- Modify: `serve.go:224-257` (package `main` : `buildGateway` → `buildGateways`, appel de `NewRegistry`), `serve.go:295-305`
- Create: `core/host/registry_gateways_test.go`
- Modify: `core/host/memory_search_verb_test.go:20,38`, `core/host/hub_test.go:86`, `core/host/memory_restore_verb_test.go:93,118,150,169,187`, `memory_verbs_test.go:16` — les `Deps{}` deviennent `nil`.

**Interfaces:**
- Consumes: rien des tâches précédentes.
- Produces:
  - `func NewRegistry(ctx context.Context, gws []Deps, o Options) (*cli.Registry, error)` — signature changée : `Deps` → `[]Deps`.
  - `func buildGateways(ctx context.Context) ([]host.Deps, error)` dans le package `main`, remplaçant `buildGateway`.

**Pourquoi :** `SetTerminalAdmin` n'est appelé que par le daemon (`core/host/serve.go:229`). Sans daemon, `session create --terminal_only` retombe sur le home passerelle et échoue avec `no home set` alors qu'il n'a besoin d'aucun home. La tâche 5 en dépend, mais le trou est préexistant.

- [ ] **Step 1: Write the failing test**

Créer `core/host/registry_gateways_test.go`. Le test réutilise `kindGateway` et
`labeledAdmin`, déjà définis dans `core/host/serve_test.go` (même package) — ne
pas les redéclarer.

Le point délicat : un `session create` qui va au bout appelle `h.sup.Start(sess)`,
qui exécute le binaire courant (donc le binaire de test) en sous-processus. Le
test s'arrête avant, en faisant échouer la création de channel : l'erreur qui
remonte nomme l'admin qui a été consulté, ce qui est exactement ce qu'on veut
prouver.

```go
package host

import (
	"context"
	"fmt"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// namedAdmin refuses to mint a channel, with a message that says who it is. A
// create that fails here has already chosen its admin and has not yet reached
// the supervisor — which would spawn a bridge out of the test binary.
type namedAdmin struct{ labeledAdmin }

func (a namedAdmin) CreateUnder(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("admin %s was consulted", a.id)
}

// A terminal-only session names no home: its channel is local. NewRegistry must
// wire the terminal admin exactly as RunHub does (serve.go:229) — otherwise the
// one path with no daemon behind it refuses a session it is able to make.
func TestNewRegistryWiresTheTerminalAdmin(t *testing.T) {
	gws := []Deps{
		{Gateway: kindGateway{"chat"}, Admin: namedAdmin{labeledAdmin{"chat"}}},
		{Gateway: kindGateway{"terminal"}, Admin: namedAdmin{labeledAdmin{"terminal"}}},
	}
	reg, err := NewRegistry(context.Background(), gws, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_, err = reg.Dispatch(context.Background(),
		[]string{"session", "create", "--name", "x", "--terminal_only", "--shared"})
	if err == nil {
		t.Fatal("create should have stopped at the channel admin")
	}
	if strings.Contains(err.Error(), "no home set") {
		t.Fatalf("terminal-only create asked for a home it does not need: %v", err)
	}
	if !strings.Contains(err.Error(), "admin terminal was consulted") {
		t.Fatalf("err = %v, want the terminal admin to have minted the channel", err)
	}
}

// Without a terminal gateway there is nothing to wire, and the historical
// refusal must stand rather than quietly degrade into another gateway's admin.
func TestNewRegistryWithoutTerminalGatewayStillNeedsAHome(t *testing.T) {
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_, err = reg.Dispatch(context.Background(),
		[]string{"session", "create", "--name", "x", "--terminal_only", "--shared"})
	if err == nil || !strings.Contains(err.Error(), "no home set") {
		t.Fatalf("err = %v, want the no-home refusal", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/host -run TestNewRegistry -v`
Expected: FAIL — la compilation casse (`cannot use gws (variable of type []Deps) as Deps value`).

- [ ] **Step 3: Change the signature and wire the admin**

Dans `core/host/cli.go`, remplacer l'en-tête de `NewRegistry` (ligne 363-367) :

```go
// NewRegistry builds the operator CLI registry: it loads its own state +
// supervisor (the operator invocation is a short-lived process) and registers
// the session/service handler's commands. The returned registry dispatches argv
// (see core/cli).
//
// It takes every built gateway set rather than one, for the same reason RunHub
// does: which admin a session channel is minted on is a question about the SET
// — the home's owner for a normal session, the terminal gateway for a
// terminal-only one. Handed a single set, this path could only ever answer the
// first question, and terminal-only sessions were unreachable with no daemon up.
func NewRegistry(ctx context.Context, gws []Deps, o Options) (*cli.Registry, error) {
```

Remplacer la construction du registre (ligne 399) :

```go
	reg, deps, err := buildRegistry(ctx, Deps{Admin: adminForHome(gws, st.Home)}, o, st, sup, instID, nil)
	if err != nil {
		return nil, err
	}
	// Terminal-only sessions route through the terminal gateway's own admin, so
	// they open as local `terminal/…` channels whatever home is configured — or
	// none. Mirrors RunHub (serve.go:229); without it this path refuses a
	// terminal-only create for want of a home it does not need.
	if ta := terminalAdmin(gws); ta != nil {
		deps.handler.SetTerminalAdmin(ta)
	}
```

Et le calcul de `DefaultGateways` (ligne 391-393) devient :

```go
	if o.DefaultGateways == nil {
		o.DefaultGateways = nonTerminalKinds(gws)
	}
```

(le `[]Deps{d}` disparaît).

- [ ] **Step 4: Update the call sites**

Dans `serve.go` (package `main`), remplacer `buildGateway` (lignes 291-305) :

```go
// buildGateways returns every gateway set the hub could build. BuildHub already
// builds them all — the old buildGateway threw away everything but the first —
// so this costs nothing extra and lets the operator registry answer the same
// questions the daemon can (which admin owns the home, which one is the
// terminal). Gateways whose config is absent are simply not in the result.
func buildGateways(ctx context.Context) ([]host.Deps, error) {
	hub, err := host.BuildHub(ctx, contracts.Default.Gateways(), os.Getenv)
	if err != nil {
		return nil, err
	}
	var sets []host.Deps
	for _, kind := range hub.Kinds() {
		if set, ok := hub.Get(kind); ok {
			sets = append(sets, set)
		}
	}
	if len(sets) == 0 {
		return nil, fmt.Errorf("no gateway built")
	}
	return sets, nil
}
```

Dans `runRegistryVerb` (lignes 229-245), remplacer :

```go
	deps, err := buildGateway(ctx)
```

par :

```go
	gws, err := buildGateways(ctx)
```

et l'appel `host.NewRegistry(ctx, deps, host.Options{…})` par `host.NewRegistry(ctx, gws, host.Options{…})`.

Dans les tests du package `host` et du package `main`, remplacer `Deps{}` / `host.Deps{}` par `nil` :

```bash
# core/host/memory_search_verb_test.go:20,38
# core/host/hub_test.go:86
# core/host/memory_restore_verb_test.go:93,118,150,169,187
# memory_verbs_test.go:16
```

`NewRegistry(context.Background(), Deps{}, Options{…})` → `NewRegistry(context.Background(), nil, Options{…})`
`host.NewRegistry(context.Background(), host.Deps{}, host.Options{…})` → `host.NewRegistry(context.Background(), nil, host.Options{…})`

- [ ] **Step 5: Run the tests**

Run: `go build ./... && go test ./...`
Expected: PASS, y compris les deux nouveaux tests.

- [ ] **Step 6: Commit**

```bash
git add core/host/cli.go core/host/registry_gateways_test.go core/host/hub_test.go core/host/memory_search_verb_test.go core/host/memory_restore_verb_test.go serve.go memory_verbs_test.go
git commit -m "fix(host): the operator registry never wired the terminal admin"
```

---

### Task 5: `runPrompt` et le branchement dans `main`

**Files:**
- Modify: `serve.go` (package `main`) — extraction de `newOperatorRegistry`, ajout de `runPrompt`/`runPromptWith`
- Modify: `main.go:115-120`
- Modify: `usage.go:45-49`
- Create: `prompt_run_test.go`

**Interfaces:**
- Consumes: `promptOf` (tâche 1), `sessionNameFor` (tâche 2), `--timeout` de `session seed` (tâche 3), `buildGateways` + `NewRegistry([]Deps, …)` (tâche 4).
- Produces:
  - `type verbDispatcher interface { Dispatch(context.Context, []string) (string, error) }`
  - `func runPromptWith(ctx context.Context, d verbDispatcher, name, prompt string, stdout, stderr io.Writer) error`
  - `func runPrompt(ctx context.Context, prompt string) error`
  - `func newOperatorRegistry(ctx context.Context) (*cli.Registry, error)`

- [ ] **Step 1: Write the failing test**

Créer `prompt_run_test.go` :

```go
package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// recordingDispatcher stands in for the operator registry: it records every argv
// and answers from a scripted table, so runPromptWith can be exercised with no
// daemon, no gateway and no backend.
type recordingDispatcher struct {
	seen  [][]string
	reply map[string]string // first two argv segments -> reply
	fail  map[string]error
}

func (r *recordingDispatcher) Dispatch(_ context.Context, argv []string) (string, error) {
	r.seen = append(r.seen, append([]string(nil), argv...))
	key := strings.Join(argv[:2], " ")
	if err := r.fail[key]; err != nil {
		return "", err
	}
	return r.reply[key], nil
}

func argOf(argv []string, flag string) (string, bool) {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1], true
		}
	}
	return "", false
}

func TestRunPromptCreatesThenSeeds(t *testing.T) {
	d := &recordingDispatcher{reply: map[string]string{"session seed": "voilà le plan"}}
	var out, errOut strings.Builder

	if err := runPromptWith(context.Background(), d, "lis-le-thread-a3fq", "lis le thread X", &out, &errOut); err != nil {
		t.Fatalf("runPromptWith: %v", err)
	}
	if len(d.seen) != 2 {
		t.Fatalf("dispatched %d commands, want create then seed: %v", len(d.seen), d.seen)
	}
	if got := strings.Join(d.seen[0][:2], " "); got != "session create" {
		t.Fatalf("first command = %q, want session create", got)
	}
	if got := strings.Join(d.seen[1][:2], " "); got != "session seed" {
		t.Fatalf("second command = %q, want session seed", got)
	}
	createName, _ := argOf(d.seen[0], "--name")
	seedName, _ := argOf(d.seen[1], "--name")
	if createName != "lis-le-thread-a3fq" || seedName != createName {
		t.Fatalf("create named %q but seed named %q", createName, seedName)
	}
	if task, _ := argOf(d.seen[1], "--task"); task != "lis le thread X" {
		t.Fatalf("seed task = %q, want the prompt verbatim", task)
	}
	if _, ok := argOf(d.seen[1], "--timeout"); !ok {
		t.Fatalf("seed must carry a timeout: %v", d.seen[1])
	}
}

// A terminal-only session is the point: the prompt path must not mint a gateway
// channel, and must not need a home to run.
func TestRunPromptCreatesATerminalOnlySession(t *testing.T) {
	d := &recordingDispatcher{reply: map[string]string{"session seed": "ok"}}
	var out, errOut strings.Builder
	if err := runPromptWith(context.Background(), d, "n-a3fq", "x y", &out, &errOut); err != nil {
		t.Fatalf("runPromptWith: %v", err)
	}
	if !slicesContain(d.seen[0], "--terminal_only") {
		t.Fatalf("create argv = %v, want --terminal_only", d.seen[0])
	}
}

func slicesContain(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// The reply is the output; the session name is a note to the operator. Splitting
// them across stdout/stderr is what makes `herrscher "…" > out.md` hold the
// answer alone.
func TestRunPromptSplitsReplyFromTheSessionName(t *testing.T) {
	d := &recordingDispatcher{reply: map[string]string{"session seed": "voilà le plan"}}
	var out, errOut strings.Builder
	if err := runPromptWith(context.Background(), d, "n-a3fq", "x y", &out, &errOut); err != nil {
		t.Fatalf("runPromptWith: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "voilà le plan" {
		t.Fatalf("stdout = %q, want the reply alone", got)
	}
	if !strings.Contains(errOut.String(), "n-a3fq") {
		t.Fatalf("stderr = %q, want the session name", errOut.String())
	}
}

// A session that could not be created has nothing to seed; seeding anyway would
// hit a second, more confusing error about a session that does not exist.
func TestRunPromptDoesNotSeedWhenCreateFails(t *testing.T) {
	d := &recordingDispatcher{fail: map[string]error{"session create": fmt.Errorf("worktree: boom")}}
	var out, errOut strings.Builder
	err := runPromptWith(context.Background(), d, "n-a3fq", "x y", &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want the create failure", err)
	}
	if len(d.seen) != 1 {
		t.Fatalf("dispatched %v, want create alone", d.seen)
	}
}

// A seed that fails leaves the session standing: it carries a worktree and the
// start of a transcript, and tearing it down is what the operator needs to
// diagnose the failure. The error names it so they can.
func TestRunPromptNamesTheSessionWhenSeedFails(t *testing.T) {
	d := &recordingDispatcher{fail: map[string]error{"session seed": fmt.Errorf("seed timeout")}}
	var out, errOut strings.Builder
	err := runPromptWith(context.Background(), d, "n-a3fq", "x y", &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "n-a3fq") {
		t.Fatalf("err = %v, want it to name the session left behind", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestRunPrompt -v`
Expected: FAIL — `undefined: runPromptWith`

- [ ] **Step 3: Extract the registry builder**

Dans `serve.go`, découper `runRegistryVerb` (lignes 224-257) en deux :

```go
// newOperatorRegistry builds the operator CLI registry — the same command
// surface the daemon serves, over this short-lived process's own state. Split
// out of runRegistryVerb because a prompt dispatches two verbs and must not
// build the gateway stack twice: each build opens the configured gateways.
func newOperatorRegistry(ctx context.Context) (*cli.Registry, error) {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return nil, err
	}
	gws, err := buildGateways(ctx)
	if err != nil {
		return nil, err
	}
	var home *host.HomeRef
	if cfg.Home != nil && cfg.Home.ID != "" {
		home = &host.HomeRef{ID: cfg.Home.ID, Type: cfg.Home.Type}
	}
	return host.NewRegistry(ctx, gws, host.Options{
		StatePath:  host.DefaultStatePath(),
		DefaultCmd: or(cfg.Cmd, "claude"),
		InstanceID: or(envx.Get("INSTANCE_ID"), cfg.Instance),
		Owner:      or(envx.Get("OWNER_ID"), cfg.Owner),
		Home:       home,
		Workspace:  cfg.Workspace,
		Source:     cfg.Source,
	})
}

// runRegistryVerb builds the operator registry (the same one the daemon serves)
// and dispatches a single top-level verb through it, printing any output. Both
// session and agent verbs share this so the binary and the gateways drive an
// identical command surface.
func runRegistryVerb(ctx context.Context, verb string, args []string) error {
	reg, err := newOperatorRegistry(ctx)
	if err != nil {
		return err
	}
	out, err := reg.Dispatch(ctx, append([]string{verb}, args...))
	if err != nil {
		return err
	}
	if out != "" {
		fmt.Println(out)
	}
	return nil
}
```

Ajouter `"github.com/Herrscherd/herrscher/core/cli"` aux imports de `serve.go`.

- [ ] **Step 4: Write runPrompt**

Ajouter à `serve.go` :

```go
// promptTimeout caps the turn a bare prompt runs. A coordination seed is a short
// question and keeps the 120s default; a task typed by hand at a terminal is not
// one, and the operator is sitting there to interrupt it.
const promptTimeout = 30 * time.Minute

// verbDispatcher is what runPromptWith needs of the registry, and nothing more —
// so the sequencing can be tested without a gateway, a daemon or a backend.
type verbDispatcher interface {
	Dispatch(context.Context, []string) (string, error)
}

// runPrompt opens a session on a free-text task, runs one turn there, and prints
// the reply.
func runPrompt(ctx context.Context, prompt string) error {
	reg, err := newOperatorRegistry(ctx)
	if err != nil {
		return err
	}
	return runPromptWith(ctx, reg, sessionNameFor(prompt), prompt, os.Stdout, os.Stderr)
}

// runPromptWith is the sequencing itself: create, then seed.
//
// The session name goes to stderr and the reply to stdout, so `herrscher "…" >
// out.md` holds the answer and nothing else.
//
// A failed seed deliberately leaves the session standing. It owns a worktree and
// the beginning of a transcript, and removing that is removing what the operator
// needs to see why the turn failed; the error names the session so they can
// resume or close it themselves.
func runPromptWith(ctx context.Context, d verbDispatcher, name, prompt string, stdout, stderr io.Writer) error {
	if _, err := d.Dispatch(ctx, []string{
		"session", "create", "--name", name, "--terminal_only",
	}); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "session: "+name)
	reply, err := d.Dispatch(ctx, []string{
		"session", "seed", "--name", name, "--task", prompt,
		"--timeout", promptTimeout.String(),
	})
	if err != nil {
		return fmt.Errorf("session %s: %w", name, err)
	}
	if reply != "" {
		fmt.Fprintln(stdout, reply)
	}
	return nil
}
```

Ajouter `"io"` aux imports de `serve.go`.

- [ ] **Step 5: Run the test**

Run: `go test . -run TestRunPrompt -v`
Expected: PASS (5 tests)

- [ ] **Step 6: Wire it into main()**

Dans `main.go`, après `cmd := os.Args[1]` / `args := os.Args[2:]` (lignes 115-116) et **avant** le switch des verbes de gestion, insérer :

```go
	// A free-text argv is a task, not a mistyped verb: open a session on it and
	// print the reply. Checked here, ahead of every switch below, because the two
	// surfaces cannot collide — no verb in the registry contains whitespace.
	if prompt, ok := promptOf(cmd, args); ok {
		if prompt == "" {
			fmt.Fprintln(os.Stderr, "herrscher: -p needs a task, e.g. herrscher -p refactor")
			os.Exit(2)
		}
		if err := runPrompt(ctx, prompt); err != nil {
			fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
			os.Exit(1)
		}
		return
	}
```

- [ ] **Step 7: Document it in usage**

Dans `usage.go`, dans le groupe `DÉMARRER` (ligne 45-49), insérer en tête :

```go
		group("DÉMARRER",
			row("herrscher \"<texte>\"", "open a session on this task and print the reply"),
			row("herrscher", "open the multi-session terminal TUI"),
			row("herrscher version", "print the build version"),
			row("herrscher init", "compose the plugin stack + secrets (wizard)"),
		),
```

- [ ] **Step 8: Verify the whole binary**

Run: `go build ./... && go test ./...`
Expected: PASS

Run: `go run . sesion`
Expected: `herrscher: unknown command "sesion"` puis l'aide — la détection ne doit pas avoir avalé la faute de frappe.

Run: `go run . -p`
Expected: `herrscher: -p needs a task, e.g. herrscher -p refactor`, exit 2.

- [ ] **Step 9: Commit**

```bash
git add serve.go main.go usage.go prompt_run_test.go
git commit -m "feat(cli): herrscher \"<task>\" opens a session and prints the reply"
```

---

### Task 6: La TUI perd son `main` codé en dur

**Files:**
- Modify: `plugins/terminal/terminal.go:70-83`
- Modify: `plugins/terminal/terminal_test.go:343-361`

**Interfaces:**
- Consumes: rien (indépendante des tâches 1-5).
- Produces: rien de consommé ailleurs.

- [ ] **Step 1: Rewrite the failing test**

Dans `plugins/terminal/terminal_test.go`, remplacer `TestEnsureDefaultSessionCreatesWhenNone` (lignes 343-361) :

```go
// terminalSessionNameRe mirrors the session-name guard in
// core/internal/manager/validate.go: whatever name the TUI mints must already
// pass it, since it becomes a filesystem path and a git ref downstream.
var terminalSessionNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func TestEnsureDefaultSessionCreatesWhenNone(t *testing.T) {
	fake := &fakeSessionControl{} // Sessions() returns nil/empty
	if err := ensureDefaultSession(context.Background(), fake); err != nil {
		t.Fatalf("ensureDefaultSession: %v", err)
	}
	if len(fake.created) != 1 {
		t.Fatalf("expected one typed Create, got: %+v", fake.created)
	}
	spec := fake.created[0]
	if !terminalSessionNameRe.MatchString(spec.Name) {
		t.Fatalf("default session name %q is not a valid session slug", spec.Name)
	}
	if spec.Name == "main" {
		t.Fatalf("the default session must not carry a fixed name")
	}
	if !spec.TerminalOnly {
		t.Fatalf("default session must be terminal-only: %+v", spec)
	}
	if !spec.Shared {
		t.Fatalf("default session must be shared: %+v", spec)
	}
}

func TestEnsureDefaultSessionNamesAreDistinct(t *testing.T) {
	a, b := &fakeSessionControl{}, &fakeSessionControl{}
	if err := ensureDefaultSession(context.Background(), a); err != nil {
		t.Fatalf("ensureDefaultSession: %v", err)
	}
	if err := ensureDefaultSession(context.Background(), b); err != nil {
		t.Fatalf("ensureDefaultSession: %v", err)
	}
	if a.created[0].Name == b.created[0].Name {
		t.Fatalf("both runs named the session %q; session create refuses a name already taken", a.created[0].Name)
	}
}
```

Ajouter `"regexp"` aux imports du fichier de test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./plugins/terminal -run TestEnsureDefaultSession -v`
Expected: FAIL — `the default session must not carry a fixed name`, et `TestEnsureDefaultSessionNamesAreDistinct` échoue aussi (les deux valent `main`).

- [ ] **Step 3: Generate the name**

Dans `plugins/terminal/terminal.go`, remplacer `ensureDefaultSession` (lignes 70-83) :

```go
// ensureDefaultSession creates a default terminal-bound session when none is
// live yet, so a freshly launched TUI has a ready tab that replies immediately.
// It is a no-op when a session already bound to the terminal gateway exists —
// and that check is now the only thing bounding creation, since the name no
// longer collides with itself: without it every launch would stack one more
// session row.
func ensureDefaultSession(ctx context.Context, c contracts.SessionControl) error {
	for _, s := range c.Sessions() {
		for _, g := range s.Gateways {
			if g == "terminal" {
				return nil // a terminal session already exists
			}
		}
	}
	_, err := c.Create(ctx, contracts.CreateSession{Name: defaultSessionName(), TerminalOnly: true, Shared: true})
	return err
}

// defaultSessionName mints the name of the TUI's own tab. It used to be the
// fixed "main", which read as a place rather than a session and could not be
// created twice; a short random slug says what it is — one session among
// others — and stays a valid session name (filesystem path, git ref).
func defaultSessionName() string {
	return "s-" + strings.ToLower(rand.Text()[:4])
}
```

Ajouter `"crypto/rand"` et `"strings"` aux imports de `plugins/terminal/terminal.go` (vérifier s'ils y sont déjà).

- [ ] **Step 4: Run the tests**

Run: `go test ./plugins/terminal -run TestEnsureDefaultSession -v`
Expected: PASS

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add plugins/terminal/terminal.go plugins/terminal/terminal_test.go
git commit -m "fix(tui): the default tab is a session, not a place called main"
```

---

## Vérification finale

- [ ] `go build ./... && go test ./...` — vert.
- [ ] `go vet ./...` — vert.
- [ ] `go run . sesion` → `unknown command "sesion"` (la détection n'avale pas les fautes de frappe).
- [ ] `go run . session list` → la liste (les verbes existants sont intacts).
- [ ] `go run . "dis bonjour et rien d'autre"` → un nom de session sur stderr, une réponse sur stdout, et `go run . session list` montre la session.
- [ ] Le spec `docs/superpowers/specs/2026-08-07-bare-prompt-invocation-design.md` et ce plan sont supprimés au moment d'ouvrir la PR — c'est la convention du dépôt (commit `a913b09`).
