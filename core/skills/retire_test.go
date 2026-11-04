package skills

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func installedDir(t *testing.T, files fstest.MapFS) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := Install(files, dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRetireRemovesAnUneditedSkillNoLongerShipped(t *testing.T) {
	dir := installedDir(t, fstest.MapFS{
		"retired/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: retired\n---\nold")},
		"kept/SKILL.md":    &fstest.MapFile{Data: []byte("---\nname: kept\n---\nstill")},
	})
	out, err := Retire(dir, []string{"kept"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Removed) != 1 || out.Removed[0] != "retired" {
		t.Fatalf("removed = %v, want retired", out.Removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "retired")); err == nil {
		t.Fatal("the unedited copy of a retired skill must be gone")
	}
	if _, err := os.Stat(filepath.Join(dir, "kept", "SKILL.md")); err != nil {
		t.Fatalf("a skill still shipped must stay: %v", err)
	}
}

func TestRetireKeepsAnEditedSkillAndReportsIt(t *testing.T) {
	dir := installedDir(t, fstest.MapFS{
		"retired/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: retired\n---\nold")},
	})
	edited := filepath.Join(dir, "retired", "SKILL.md")
	if err := os.WriteFile(edited, []byte("my own notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := Retire(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Removed) != 0 || len(out.Diverged) != 1 || out.Diverged[0] != "retired" {
		t.Fatalf("outcome = %+v, want the edited skill reported as diverged and not removed", out)
	}
	b, err := os.ReadFile(edited)
	if err != nil || string(b) != "my own notes" {
		t.Fatalf("an edited skill must be left alone, got %q, %v", b, err)
	}
}

func TestRetireForgetsASkillTheOperatorDeleted(t *testing.T) {
	dir := installedDir(t, fstest.MapFS{
		"retired/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: retired\n---\nold")},
	})
	if err := os.RemoveAll(filepath.Join(dir, "retired")); err != nil {
		t.Fatal(err)
	}
	out, err := Retire(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Empty() {
		t.Fatalf("a skill already gone has nothing to report, got %+v", out)
	}
	again, err := Install(fstest.MapFS{"retired/SKILL.md": &fstest.MapFile{Data: []byte("back")}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Installed) != 1 {
		t.Fatalf("a forgotten skill must install again when a build ships it, got %+v", again)
	}
}

func TestRetireWithoutManifestDoesNothing(t *testing.T) {
	dir := t.TempDir()
	out, err := Retire(dir, nil)
	if err != nil || !out.Empty() {
		t.Fatalf("retire with no manifest = %+v, %v; want nothing", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, manifestName)); err == nil {
		t.Fatal("retire must not write a manifest where none existed")
	}
}
