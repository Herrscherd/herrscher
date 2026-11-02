package skills

import (
	"os"
	"path/filepath"
	"strings"
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

func upgradedFS() fstest.MapFS {
	fs := shippedFS()
	fs["pr-job/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: pr-job\n---\nrewritten")}
	return fs
}

func TestInstallWritesEverySkillTree(t *testing.T) {
	dir := t.TempDir()
	out, err := Install(shippedFS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Installed) != 2 {
		t.Fatalf("installed %v, want both skill directories", out.Installed)
	}
	b, err := os.ReadFile(filepath.Join(dir, "pr-job", "ref", "notes.md"))
	if err != nil || string(b) != "nested" {
		t.Fatalf("nested file = %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
		t.Fatal("a non-directory entry was installed as a skill")
	}
}

func TestInstallNeverOverwritesAnEditedSkill(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(shippedFS(), dir); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(dir, "pr-job", "SKILL.md")
	if err := os.WriteFile(edited, []byte("my own procedure"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Install(upgradedFS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Installed) != 0 || len(out.Updated) != 0 {
		t.Fatalf("wrote %v/%v on the second run, want nothing", out.Installed, out.Updated)
	}
	b, err := os.ReadFile(edited)
	must(t, err)
	if string(b) != "my own procedure" {
		t.Fatalf("edited skill = %q, want the operator's version kept", b)
	}
	if len(out.Diverged) != 1 || out.Diverged[0] != "pr-job" {
		t.Fatalf("diverged = %v, want pr-job reported so the operator learns of it", out.Diverged)
	}
}

func TestInstallUpdatesAnUntouchedSkill(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(shippedFS(), dir); err != nil {
		t.Fatal(err)
	}
	out, err := Install(upgradedFS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Updated) != 1 || out.Updated[0] != "pr-job" {
		t.Fatalf("updated = %v, want pr-job refreshed to the shipped text", out.Updated)
	}
	if len(out.Diverged) != 0 {
		t.Fatalf("diverged = %v, want nothing: the copy was ours", out.Diverged)
	}
	b, err := os.ReadFile(filepath.Join(dir, "pr-job", "SKILL.md"))
	must(t, err)
	if string(b) != "---\nname: pr-job\n---\nrewritten" {
		t.Fatalf("skill = %q, want the newly shipped text", b)
	}
	again, err := Install(upgradedFS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Installed)+len(again.Updated)+len(again.Diverged) != 0 {
		t.Fatalf("a third run reported %+v, want silence", again)
	}
}

func TestUpdateRemovesFilesTheReleaseDropped(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(shippedFS(), dir); err != nil {
		t.Fatal(err)
	}
	slimmed := upgradedFS()
	delete(slimmed, "pr-job/ref/notes.md")
	if _, err := Install(slimmed, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pr-job", "ref", "notes.md")); err == nil {
		t.Fatal("a file the release dropped is still installed")
	}
}

func TestUnmanagedSkillIsReportedNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pr-job"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "pr-job", "SKILL.md")
	if err := os.WriteFile(old, []byte("an older release"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Install(shippedFS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(old)
	must(t, err)
	if string(b) != "an older release" {
		t.Fatalf("unmanaged skill = %q, want it kept", b)
	}
	if len(out.Diverged) != 1 || out.Diverged[0] != "pr-job" {
		t.Fatalf("diverged = %v, want pr-job reported", out.Diverged)
	}
}

func TestUnmanagedSkillIdenticalToShippedIsAdopted(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pr-job", "ref"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, f := range shippedFS() {
		if !strings.HasPrefix(p, "pr-job/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(p)), f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Install(shippedFS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Diverged) != 0 {
		t.Fatalf("diverged = %v, want nothing: it matches what we ship", out.Diverged)
	}
	if out, err = Install(upgradedFS(), dir); err != nil {
		t.Fatal(err)
	} else if len(out.Updated) != 1 || out.Updated[0] != "pr-job" {
		t.Fatalf("updated = %v, want the adopted skill to take the new release", out.Updated)
	}
}

func TestManifestIsNotASkillAndSurvivesCorruption(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(shippedFS(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, manifestName)); err != nil {
		t.Fatalf("the manifest must be written: %v", err)
	}
	for _, s := range Discover([]string{dir}) {
		if strings.Contains(s.Dir, manifestName) || s.Name == manifestName {
			t.Fatalf("the manifest was discovered as a skill: %+v", s)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(upgradedFS(), dir); err != nil {
		t.Fatalf("a corrupt manifest must not fail the install: %v", err)
	}
}
