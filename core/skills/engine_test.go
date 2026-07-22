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
