package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOutsideAnyRepositoryTellsAFolderOfNoRepositoryFromAFolderOfOne(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("LANGUAGE", "fr")
	t.Setenv("LANG", "fr_FR.UTF-8")
	plain := filepath.Join(root, "plain")
	repo := filepath.Join(root, "repo")
	for _, dir := range []string{plain, filepath.Join(repo, "sub")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if outside, err := OutsideAnyRepository(t.Context(), plain); err != nil || !outside {
		t.Fatalf("OutsideAnyRepository(plain) = %v, %v, want true", outside, err)
	}
	if outside, err := OutsideAnyRepository(t.Context(), filepath.Join(repo, "sub")); err != nil || outside {
		t.Fatalf("OutsideAnyRepository(repo/sub) = %v, %v, want false", outside, err)
	}
}

func TestOutsideAnyRepositoryReportsAFolderGitCannotEnterAsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if outside, err := OutsideAnyRepository(t.Context(), missing); err == nil || outside {
		t.Fatalf("OutsideAnyRepository(missing) = %v, %v, want an error", outside, err)
	}
}
