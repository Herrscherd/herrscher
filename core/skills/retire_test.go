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

func TestInstallNeverAdoptsASymbolicLink(t *testing.T) {
	shippedBody := []byte("---\nname: linked\n---\nshipped")
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(elsewhere, "linked"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "linked", "SKILL.md"), shippedBody, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "linked")
	if err := os.Symlink(filepath.Join(elsewhere, "linked"), dest); err != nil {
		t.Fatal(err)
	}
	out, err := Install(fstest.MapFS{"linked/SKILL.md": &fstest.MapFile{Data: shippedBody}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Linked) != 1 || len(out.Installed) != 0 || len(out.Updated) != 0 {
		t.Fatalf("outcome = %+v, want the link reported as linked and nothing written", out)
	}
	info, err := os.Lstat(dest)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the operator's symbolic link must stay a link, got %v, %v", info, err)
	}
	if _, err := Retire(dir, nil); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(dest); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("retire must never remove a symbolic link, got %v, %v", info, err)
	}
}

func TestRetireNeverRemovesASymbolicLink(t *testing.T) {
	body := []byte("---\nname: retired\n---\nold")
	dir := installedDir(t, fstest.MapFS{"retired/SKILL.md": &fstest.MapFile{Data: body}})
	dest := filepath.Join(dir, "retired")
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(elsewhere, "retired"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "retired", "SKILL.md"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "retired"), dest); err != nil {
		t.Fatal(err)
	}
	out, err := Retire(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Removed) != 0 || len(out.Linked) != 1 || len(out.Diverged) != 0 {
		t.Fatalf("outcome = %+v, want the link kept and reported as linked", out)
	}
	if info, err := os.Lstat(dest); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symbolic link must stay, got %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "retired", "SKILL.md")); err != nil {
		t.Fatalf("the target of the link must not be touched: %v", err)
	}
}

func TestRetireKeepsASkillHoldingAnInnerSymbolicLink(t *testing.T) {
	body := []byte("---\nname: retired\n---\nold")
	dir := installedDir(t, fstest.MapFS{"retired/SKILL.md": &fstest.MapFile{Data: body}})
	inner := filepath.Join(dir, "retired", "SKILL.md")
	elsewhere := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(elsewhere, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(inner); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, inner); err != nil {
		t.Fatal(err)
	}
	out, err := Retire(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Removed) != 0 || len(out.Diverged) != 1 {
		t.Fatalf("outcome = %+v, want a skill with an inner link kept and reported", out)
	}
	if info, err := os.Lstat(inner); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the inner symbolic link must stay, got %v, %v", info, err)
	}
}
