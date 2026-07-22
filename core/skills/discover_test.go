package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, root, name, front, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\n" + front + "---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverParsesAndDedupes(t *testing.T) {
	repo := t.TempDir()
	global := t.TempDir()
	writeSkill(t, repo, "pdf-fill", "name: pdf-fill\ndescription: fill PDFs\n", "step one\n")
	writeSkill(t, global, "pdf-fill", "name: pdf-fill\ndescription: GLOBAL loses\n", "x\n")
	writeSkill(t, global, "web", "name: web\ndescription: browse\n", "y\n")
	writeSkill(t, repo, "broken", "no-frontmatter-here\n", "z\n")

	got := Discover([]string{repo, global})
	if len(got) != 2 {
		t.Fatalf("want 2 skills (deduped, malformed skipped), got %d: %+v", len(got), got)
	}
	byName := map[string]Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if byName["pdf-fill"].Description != "fill PDFs" {
		t.Fatalf("repo root must win de-dup, got %q", byName["pdf-fill"].Description)
	}
	body, err := byName["pdf-fill"].Body()
	if err != nil || body != "step one\n" {
		t.Fatalf("Body() = %q, %v", body, err)
	}
	if _, ok := byName["web"]; !ok {
		t.Fatalf("global-only skill missing: %+v", byName)
	}
}

func TestDiscoverMissingRootIsSkipped(t *testing.T) {
	got := Discover([]string{filepath.Join(t.TempDir(), "does-not-exist")})
	if len(got) != 0 {
		t.Fatalf("missing root should yield no skills, got %+v", got)
	}
}
