package skills

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func shippedFS() fstest.MapFS {
	return fstest.MapFS{
		"pr-job/SKILL.md":     &fstest.MapFile{Data: []byte("---\nname: pr-job\n---\nshipped")},
		"pr-job/ref/notes.md": &fstest.MapFile{Data: []byte("nested")},
		"other/SKILL.md":      &fstest.MapFile{Data: []byte("other")},
		"README.md":           &fstest.MapFile{Data: []byte("not a skill")},
	}
}

func TestInstallWritesEverySkillTree(t *testing.T) {
	dir := t.TempDir()
	made, err := Install(shippedFS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 2 {
		t.Fatalf("installed %v, want both skill directories", made)
	}
	b, err := os.ReadFile(filepath.Join(dir, "pr-job", "ref", "notes.md"))
	if err != nil || string(b) != "nested" {
		t.Fatalf("nested file = %q, %v", b, err)
	}
	// A loose file beside the skills is not a skill and must not be installed.
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
		t.Fatal("a non-directory entry was installed as a skill")
	}
}

// The playbook is meant to be edited on the machine that runs it, so a restart
// must never quietly restore the shipped text over the operator's version.
func TestInstallNeverOverwritesAnEditedSkill(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(shippedFS(), dir); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(dir, "pr-job", "SKILL.md")
	if err := os.WriteFile(edited, []byte("my own procedure"), 0o644); err != nil {
		t.Fatal(err)
	}
	made, err := Install(shippedFS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 0 {
		t.Fatalf("installed %v on the second run, want nothing", made)
	}
	if b, _ := os.ReadFile(edited); string(b) != "my own procedure" {
		t.Fatalf("edited skill = %q, want the operator's version kept", b)
	}
}
