package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestPromptOf(t *testing.T) {
	cases := []struct {
		name   string
		cmd    string
		args   []string
		want   string
		wantOK bool
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
