package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestPromptOf(t *testing.T) {
	cases := []struct {
		name      string
		cmd       string
		args      []string
		want      string
		wantPrint bool
		wantOK    bool
	}{
		{"espace dans l'argument", "lis le thread X", nil, "lis le thread X", false, true},
		{"argument à espace puis positionnels", "lis le thread", []string{"en", "entier"}, "lis le thread en entier", false, true},
		{"tabulation compte comme espace", "lis\tle thread", nil, "lis\tle thread", false, true},
		{"saut de ligne compte comme espace", "lis\nle thread", nil, "lis\nle thread", false, true},
		{"-p force un mot seul", "-p", []string{"refactor"}, "refactor", false, true},
		{"--prompt force un mot seul", "--prompt", []string{"refactor"}, "refactor", false, true},
		{"-p joint ses arguments", "-p", []string{"lis", "le", "thread"}, "lis le thread", false, true},
		{"-p nu est un prompt vide", "-p", nil, "", false, true},
		{"-p blanc est un prompt vide", "-p", []string{"   "}, "", false, true},
		{"--print demande stdout", "--print", []string{"lis", "le", "thread"}, "lis le thread", true, true},
		{"--print force un mot seul", "--print", []string{"refactor"}, "refactor", true, true},
		{"--print nu est un prompt vide", "--print", nil, "", true, true},
		{"verbe d'un mot", "sesion", nil, "", false, false},
		{"verbe avec sous-commande", "session", []string{"list"}, "", false, false},
		{"flag inconnu", "-x", nil, "", false, false},
		{"flag long inconnu", "--config", []string{"x.json"}, "", false, false},
		{"argument entièrement blanc", "   ", nil, "", false, false},
		{"argument vide", "", nil, "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := promptOf(tc.cmd, tc.args)
			if ok != tc.wantOK {
				t.Fatalf("promptOf(%q, %v) ok = %v, want %v", tc.cmd, tc.args, ok, tc.wantOK)
			}
			if got.Text != tc.want {
				t.Fatalf("promptOf(%q, %v) = %q, want %q", tc.cmd, tc.args, got.Text, tc.want)
			}
			if got.Print != tc.wantPrint {
				t.Fatalf("promptOf(%q, %v) print = %v, want %v", tc.cmd, tc.args, got.Print, tc.wantPrint)
			}
		})
	}
}

// The window is the default, but only where one can be drawn: --print asks for
// stdout, and so does anything that makes a window impossible — no terminal to
// draw on, or no frontend compiled in to draw it.
func TestTaskPrintsTo(t *testing.T) {
	cases := []struct {
		name    string
		task    task
		canDraw bool
		want    bool
	}{
		{"fenêtre possible, pas de --print → fenêtre", task{Text: "x"}, true, false},
		{"fenêtre possible, --print → stdout", task{Text: "x", Print: true}, true, true},
		{"pas de fenêtre possible → stdout malgré tout", task{Text: "x"}, false, true},
		{"pas de fenêtre possible, --print → stdout", task{Text: "x", Print: true}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.task.printsTo(tc.canDraw); got != tc.want {
				t.Fatalf("printsTo(%v) = %v, want %v", tc.canDraw, got, tc.want)
			}
		})
	}
}

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
	if !strings.HasPrefix(name, "va-lire-thread-guide-l-aventurier-") {
		t.Fatalf("name = %q, want it to open on the first five content words", name)
	}
}

// The defect this guards: the name has five words, and a prompt that opens on
// filler spends them all on the half it shares with every other prompt. Two
// tasks that differ only after "the" have to arrive as two readable names.
func TestSessionNameForDistinguishesPromptsSharingAnOpening(t *testing.T) {
	a := sessionNameFor("please read the auth module and propose a split")
	b := sessionNameFor("please read the auth module and write tests")
	stem := func(s string) string { return s[:strings.LastIndex(s, "-")] }
	if stem(a) == stem(b) {
		t.Fatalf("both prompts named %q; the name must carry what differs", stem(a))
	}
	if !strings.HasPrefix(a, "read-auth-module-propose-split-") {
		t.Fatalf("a = %q, want the content words", a)
	}
	if !strings.HasPrefix(b, "read-auth-module-write-tests-") {
		t.Fatalf("b = %q, want the content words", b)
	}
}

// slugInvalid keeps [a-z0-9_-] only, so an accent left standing is not dropped
// but replaced: "résume" would arrive as "r-sume".
func TestSessionNameForFoldsAccents(t *testing.T) {
	name := sessionNameFor("Résume la discussion et détaille les décisions")
	if !strings.HasPrefix(name, "resume-discussion-detaille-decisions-") {
		t.Fatalf("name = %q, want the accents folded rather than punched out", name)
	}
}

// Filtering must never leave the session anonymous: a prompt that is all filler
// is still better named after itself than after the "s-" fallback.
func TestSessionNameForKeepsFillerWhenThatIsAllThereIs(t *testing.T) {
	name := sessionNameFor("please can you")
	if !strings.HasPrefix(name, "please-can-you-") {
		t.Fatalf("name = %q, want the words back when filtering empties them", name)
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
