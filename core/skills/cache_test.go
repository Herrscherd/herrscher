package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A rescan runs on every turn, so it reuses what it already parsed. That is only
// safe if an edit still lands: these tests are the ones that make the cache an
// optimisation rather than a way to serve yesterday's instructions.

func TestRefreshPicksUpAnEditedDescription(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "one", "name: one\ndescription: first\n", "ONE\n")
	e := NewEngine([]string{root})

	writeSkill(t, root, "one", "name: one\ndescription: second, and longer\n", "ONE\n")
	e.Refresh()

	if got := e.Menu(); !strings.Contains(got, "second, and longer") {
		t.Fatalf("menu kept the stale description:\n%s", got)
	}
}

func TestRefreshPicksUpAnEditedBody(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "one", "name: one\ndescription: first\n", "OLD BODY\n")
	e := NewEngine([]string{root})
	e.Detect("<use-skill>one</use-skill>")
	if got := e.Expansions(); !strings.Contains(got, "OLD BODY") {
		t.Fatalf("expansion did not carry the body:\n%s", got)
	}

	writeSkill(t, root, "one", "name: one\ndescription: first\n", "NEW BODY, rewritten\n")
	e.Refresh()

	got := e.Expansions()
	if strings.Contains(got, "OLD BODY") || !strings.Contains(got, "NEW BODY") {
		t.Fatalf("expansion served the cached body after an edit:\n%s", got)
	}
}

// An edit that keeps the byte count is the case a size check alone would miss,
// and it is the realistic one: fixing a typo in place.
func TestRefreshPicksUpASameSizeEdit(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "one", "name: one\ndescription: aaa\n", "BODY A\n")
	e := NewEngine([]string{root})
	e.Detect("<use-skill>one</use-skill>")
	_ = e.Expansions()

	writeSkill(t, root, "one", "name: one\ndescription: bbb\n", "BODY B\n")
	e.Refresh()

	if got := e.Menu(); !strings.Contains(got, "bbb") {
		t.Fatalf("menu kept the stale description:\n%s", got)
	}
	if got := e.Expansions(); !strings.Contains(got, "BODY B") {
		t.Fatalf("expansion kept the stale body:\n%s", got)
	}
}

// A skill that disappears must not leave its body behind in the cache, or a
// later skill reusing the path would inherit it.
func TestRefreshForgetsARemovedSkill(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "one", "name: one\ndescription: first\n", "GONE SOON\n")
	e := NewEngine([]string{root})
	e.Detect("<use-skill>one</use-skill>")
	_ = e.Expansions()

	if err := os.RemoveAll(filepath.Join(root, "one")); err != nil {
		t.Fatal(err)
	}
	e.Refresh()

	if len(e.bodies) != 0 {
		t.Fatalf("body cache kept %d entries for a skill that no longer exists", len(e.bodies))
	}
	if got := e.Expansions(); got != "" {
		t.Fatalf("expansion still carries a removed skill:\n%s", got)
	}
}

// The point of all of the above: an unchanged tree is not read again.
func TestRefreshDoesNotRereadAnUnchangedSkill(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "one", "name: one\ndescription: first\n", "ONE\n")
	e := NewEngine([]string{root})

	_, reread := discover(e.roots, e.byPath)
	if len(reread) != 0 {
		t.Fatalf("an unchanged SKILL.md was read again: %v", reread)
	}
}
