package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutsideAnyRepositoryTellsAFolderOfNoRepositoryFromAFolderOfOne(t *testing.T) {
	root := t.TempDir()
	if entry, err := gitEntryOnPath(root); err != nil || entry != "" {
		t.Skipf("the temporary directory %s sits under %q (err %v), so no folder in it is outside every repository; point TMPDIR outside any repository to run this test", root, entry, err)
	}
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

func TestRepositoryDiscoveryFailedOnlyForASearchThatFoundNoRepository(t *testing.T) {
	cases := []struct {
		name     string
		exitCode int
		stderr   string
		want     bool
	}{
		{"no repository in any parent", 128, "fatal: not a git repository (or any of the parent directories): .git\n", true},
		{"no repository up to a mount point", 128, "fatal: not a git repository (or any parent up to mount point /mnt)\nStopping at filesystem boundary (GIT_DISCOVERY_ACROSS_FILESYSTEM not set).\n", true},
		{"dangling gitfile on git 2.39 and older", 128, "fatal: not a git repository: /tmp/g/repo/.git/worktrees/wt\n", false},
		{"dangling gitfile on recent git", 128, "fatal: gitfile does not point to a valid repository: /tmp/g/repo/.git/worktrees/wt\n", false},
		{"other exit code", 1, "fatal: not a git repository (or any of the parent directories): .git\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repositoryDiscoveryFailed(tc.exitCode, tc.stderr); got != tc.want {
				t.Fatalf("repositoryDiscoveryFailed(%d, %q) = %v, want %v", tc.exitCode, tc.stderr, got, tc.want)
			}
		})
	}
}

func TestOutsideAnyRepositoryRefusesAFolderUnderAGitEntryGitDoesNotSee(t *testing.T) {
	repo := t.TempDir()
	hidden := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(hidden, 0o750); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Join(repo, "a"))
	if outside, err := OutsideAnyRepository(t.Context(), hidden); !errors.Is(err, ErrHiddenRepository) || !strings.Contains(fmt.Sprint(err), filepath.Join(repo, ".git")) || outside {
		t.Fatalf("OutsideAnyRepository(repo/a/b) = %v, %v, want an error naming %s", outside, err, filepath.Join(repo, ".git"))
	}
}
