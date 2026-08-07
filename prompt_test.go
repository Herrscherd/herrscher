package main

import "testing"

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
