package skills

import "testing"

func TestStopWordsNeverScore(t *testing.T) {
	words := wordSet("écris les rapports pour les tableaux")
	if words["les"] || words["pour"] {
		t.Fatalf("stop words must not be kept as words: %v", words)
	}
	if got := relevance(words, Skill{Name: "pour-les-autres", Description: "pour les autres"}); got != 0 {
		t.Fatalf("a skill sharing only stop words must score 0, got %d", got)
	}
}

func TestStopWordsDoNotRankAboveRealWords(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "aide-les", "name: aide-les\ndescription: les pour les\n", "A\n")
	writeSkill(t, root, "reseau-x", "name: reseau-x\ndescription: gestion du réseau\n", "B\n")
	e := NewEngine([]string{root})
	ranked := e.rankedInactive("écris les rapports pour le réseau")
	if len(ranked) != 2 || ranked[0] != "reseau-x" {
		t.Fatalf("the skill sharing a real word must rank first, got %v", ranked)
	}
}

func TestAccentsAreFolded(t *testing.T) {
	if got := relevance(wordSet("gérer le réseau"), Skill{Description: "Gestion du reseau"}); got != 1 {
		t.Fatalf("réseau must match reseau, got score %d", got)
	}
	if !wordSet("Élève")["eleve"] {
		t.Fatalf("an accented word must be stored folded, got %v", wordSet("Élève"))
	}
}

func TestSimplePluralsAreStripped(t *testing.T) {
	if got := relevance(wordSet("rapports"), Skill{Description: "rapport"}); got != 1 {
		t.Fatalf("rapports must match rapport, got score %d", got)
	}
	if got := relevance(wordSet("tableau"), Skill{Description: "tableaux"}); got != 1 {
		t.Fatalf("tableaux must match tableau, got score %d", got)
	}
}
