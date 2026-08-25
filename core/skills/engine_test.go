package skills

import (
	"strings"
	"testing"
)

func TestEngineMenuDetectExpand(t *testing.T) {
	repo := t.TempDir()
	writeSkill(t, repo, "pdf-fill", "name: pdf-fill\ndescription: fill PDFs\n", "FILL THE PDF\n")
	writeSkill(t, repo, "web", "name: web\ndescription: browse\n", "BROWSE\n")

	e := NewEngine([]string{repo})

	menu := e.Menu()
	for _, want := range []string{"<skills", "pdf-fill: fill PDFs", "web: browse", "<use-skill>"} {
		if !strings.Contains(menu, want) {
			t.Fatalf("menu missing %q:\n%s", want, menu)
		}
	}
	if e.Expansions() != "" {
		t.Fatalf("nothing active yet, want empty expansions, got %q", e.Expansions())
	}

	e.Detect("sure, I'll use it <use-skill> pdf-fill </use-skill> now")
	exp := e.Expansions()
	if !strings.Contains(exp, "FILL THE PDF") {
		t.Fatalf("active skill body missing:\n%s", exp)
	}
	if !strings.Contains(exp, `name="pdf-fill"`) || !strings.Contains(exp, repo) {
		t.Fatalf("expansion should carry name + abs dir:\n%s", exp)
	}
	if strings.Contains(exp, "BROWSE") {
		t.Fatalf("only activated skill should expand:\n%s", exp)
	}
}

func TestEngineStripRemovesMarkers(t *testing.T) {
	e := NewEngine([]string{t.TempDir()})
	got := e.Strip("here you go <use-skill> pdf-fill </use-skill>")
	if got != "here you go" {
		t.Fatalf("marker must be stripped from reply, got %q", got)
	}
}

func TestEngineRefreshPicksUpNewSkill(t *testing.T) {
	repo := t.TempDir()
	writeSkill(t, repo, "one", "name: one\ndescription: first\n", "ONE\n")
	e := NewEngine([]string{repo})
	e.Detect("<use-skill>one</use-skill>")

	writeSkill(t, repo, "two", "name: two\ndescription: second\n", "TWO\n")
	e.Refresh()

	if !strings.Contains(e.Menu(), "two: second") {
		t.Fatalf("refresh must surface the newly added skill:\n%s", e.Menu())
	}
	if !strings.Contains(e.Expansions(), "ONE") {
		t.Fatalf("refresh must keep the already-active skill active:\n%s", e.Expansions())
	}
}

func TestEngineUnknownMarkerIgnored(t *testing.T) {
	e := NewEngine([]string{t.TempDir()})
	e.Detect("<use-skill>nope</use-skill>")
	if e.Expansions() != "" {
		t.Fatalf("unknown skill must not activate, got %q", e.Expansions())
	}
	if e.Menu() != "" {
		t.Fatalf("no skills discovered, menu must be empty, got %q", e.Menu())
	}
}

func TestDetectReturnsTheKnownNamesItActivated(t *testing.T) {
	repo := t.TempDir()
	writeSkill(t, repo, "alpha", "name: alpha\ndescription: do alpha\n", "DO ALPHA\n")
	writeSkill(t, repo, "beta", "name: beta\ndescription: do beta\n", "DO BETA\n")
	e := NewEngine([]string{repo})

	got := e.Detect("<use-skill>alpha</use-skill> puis <use-skill>ghost</use-skill> " +
		"puis <use-skill>alpha</use-skill> et <use-skill>beta</use-skill>")

	want := []string{"alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("Detect = %v, want %v (an unknown name names nothing to record, a repeat is one use)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Detect = %v, want %v in order of first appearance", got, want)
		}
	}
	// The names are a by-product; activation must still have happened.
	if exp := e.Expansions(); !strings.Contains(exp, "DO ALPHA") || !strings.Contains(exp, "DO BETA") {
		t.Errorf("Detect returned names but activated nothing:\n%s", exp)
	}
}

func TestDetectReturnsNothingWhenNothingMatched(t *testing.T) {
	e := NewEngine([]string{t.TempDir()})
	if got := e.Detect("pas de marqueur ici"); len(got) != 0 {
		t.Errorf("Detect = %v, want empty", got)
	}
}
