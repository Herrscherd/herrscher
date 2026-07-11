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

func TestParseSeal(t *testing.T) {
	cases := []struct {
		name   string
		reply  string
		wantN  int
		wantOK bool
	}{
		{"valide", "je scelle.\n⟢ seal: 5", 5, true},
		{"un", "⟢ seal: 1", 1, true},
		{"zéro refusé", "⟢ seal: 0", 0, false},
		{"négatif refusé", "⟢ seal: -2", 0, false},
		{"non entier refusé", "⟢ seal: trois", 0, false},
		{"corps vide refusé", "⟢ seal:", 0, false},
		{"marker absent", "⟢ done: fini", 0, false},
		{"pas en dernière ligne", "⟢ seal: 5\nautre chose", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := parseSeal(tc.reply)
			if ok != tc.wantOK || n != tc.wantN {
				t.Fatalf("parseSeal(%q) = (%d,%v), want (%d,%v)", tc.reply, n, ok, tc.wantN, tc.wantOK)
			}
		})
	}
}

func TestParseFanOut(t *testing.T) {
	cases := []struct {
		reply     string
		wantAgent string
		wantTasks []string
		wantOK    bool
	}{
		{"txt\n⟢ fanout: alpha — t1 ;; t2 ;; t3", "alpha", []string{"t1", "t2", "t3"}, true},
		{"txt\n⟢ fanout: alpha — seule tâche", "alpha", []string{"seule tâche"}, true},
		{"txt\n⟢ fanout: alpha —  t1  ;;  t2 ", "alpha", []string{"t1", "t2"}, true},
		{"txt\n⟢ fanout: alpha — t1 ;; ;; t2", "alpha", []string{"t1", "t2"}, true},
		{"txt\n⟢ fanout:  — t1", "", nil, false},           // agent vide
		{"txt\n⟢ fanout: alpha —", "", nil, false},         // aucune tâche
		{"txt\n⟢ fanout: alpha — ;; ;;", "", nil, false},   // tâches toutes vides
		{"txt\n⟢ fanout: sans separateur", "", nil, false}, // pas d'em-dash
		{"aucun trailer", "", nil, false},
	}
	for _, tc := range cases {
		agent, tasks, ok := parseFanOut(tc.reply)
		if ok != tc.wantOK {
			t.Fatalf("reply %q → ok=%v (voulu %v)", tc.reply, ok, tc.wantOK)
		}
		if !ok {
			continue
		}
		if agent != tc.wantAgent {
			t.Fatalf("reply %q → agent=%q (voulu %q)", tc.reply, agent, tc.wantAgent)
		}
		if len(tasks) != len(tc.wantTasks) {
			t.Fatalf("reply %q → %d tâches (voulu %d): %v", tc.reply, len(tasks), len(tc.wantTasks), tasks)
		}
		for i := range tasks {
			if tasks[i] != tc.wantTasks[i] {
				t.Fatalf("reply %q → tâche %d = %q (voulu %q)", tc.reply, i, tasks[i], tc.wantTasks[i])
			}
		}
	}
}
