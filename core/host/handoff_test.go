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

func TestParseDelegate(t *testing.T) {
	agent, task, ok := parseDelegate("blabla\n⟢ delegate: scripter — écris le module de vol")
	if !ok || agent != "scripter" || task != "écris le module de vol" {
		t.Fatalf("delegate valide mal parsé: %q %q %v", agent, task, ok)
	}
	if _, _, ok := parseDelegate("⟢ delegate: scripter"); ok {
		t.Fatalf("delegate sans em-dash devrait échouer")
	}
	if _, _, ok := parseDelegate("⟢ delegate:  — tâche"); ok {
		t.Fatalf("delegate sans agent devrait échouer")
	}
	if _, _, ok := parseDelegate("pas de trailer ici"); ok {
		t.Fatalf("absence de trailer devrait échouer")
	}
	if _, _, ok := parseDelegate("⟢ handoff: scripter — x"); ok {
		t.Fatalf("handoff ne doit pas matcher delegate")
	}
}

func TestParseDone(t *testing.T) {
	summary, ok := parseDone("j'ai fini\n⟢ done: module de vol commité, 12 tests verts")
	if !ok || summary != "module de vol commité, 12 tests verts" {
		t.Fatalf("done valide mal parsé: %q %v", summary, ok)
	}
	// pas d'em-dash requis : tout le corps est le résumé (même avec un tiret dedans)
	if s, ok := parseDone("⟢ done: fait — et testé"); !ok || s != "fait — et testé" {
		t.Fatalf("done avec tiret dans le corps: %q %v", s, ok)
	}
	if _, ok := parseDone("⟢ done:   "); ok {
		t.Fatalf("done vide devrait échouer")
	}
	if _, ok := parseDone("pas de trailer"); ok {
		t.Fatalf("absence de trailer devrait échouer")
	}
}

func TestParseMerge(t *testing.T) {
	cases := []struct {
		name       string
		reply      string
		wantWorker string
		wantOK     bool
	}{
		{"valid", "doing the thing\n⟢ merge: worker-x", "worker-x", true},
		{"trims spaces", "x\n⟢ merge:   worker-2  ", "worker-2", true},
		{"empty body", "x\n⟢ merge:", "", false},
		{"not last line", "⟢ merge: worker-x\nmore text", "", false},
		{"different marker", "x\n⟢ done: summary", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, ok := parseMerge(tc.reply)
			if ok != tc.wantOK || w != tc.wantWorker {
				t.Fatalf("parseMerge(%q) = (%q, %v), want (%q, %v)", tc.reply, w, ok, tc.wantWorker, tc.wantOK)
			}
		})
	}
}
