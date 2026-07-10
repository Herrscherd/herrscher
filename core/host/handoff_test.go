package host

import "testing"

func TestParseHandoff(t *testing.T) {
	cases := []struct {
		name        string
		reply       string
		agent, task string
		ok          bool
	}{
		{"valid", "Voilà.\n⟢ handoff: scripter — finir le module", "scripter", "finir le module", true},
		{"trailing spaces", "x\n  ⟢ handoff:  scripter  —  tâche  ", "scripter", "tâche", true},
		{"not last line ignored", "⟢ handoff: a — b\nplus de texte après", "", "", false},
		{"no marker", "juste une réponse normale", "", "", false},
		{"empty agent", "⟢ handoff:  — tâche", "", "", false},
		{"empty task", "⟢ handoff: scripter — ", "", "", false},
		{"missing separator", "⟢ handoff: scripter finir", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, tk, ok := parseHandoff(c.reply)
			if ok != c.ok || a != c.agent || tk != c.task {
				t.Fatalf("parseHandoff(%q) = (%q,%q,%v), want (%q,%q,%v)",
					c.reply, a, tk, ok, c.agent, c.task, c.ok)
			}
		})
	}
}
