package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPathAndBranch(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		session    string
		wantPath   string
		wantBranch string
	}{
		{
			name:       "namespaced",
			instanceID: "alice",
			session:    "foo",
			wantPath:   filepath.Join("/repo", ".dctl-sessions", "alice", "foo"),
			wantBranch: "session/alice/foo",
		},
		{
			name:       "legacy-empty-id",
			instanceID: "",
			session:    "foo",
			wantPath:   filepath.Join("/repo", ".dctl-sessions", "foo"),
			wantBranch: "session/foo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorktreer(context.Background(), tt.instanceID)
			if got := w.Path("/repo", tt.session); got != tt.wantPath {
				t.Fatalf("Path = %q, want %q", got, tt.wantPath)
			}
			if got := w.Branch(tt.session); got != tt.wantBranch {
				t.Fatalf("Branch = %q, want %q", got, tt.wantBranch)
			}
		})
	}
}

// initRepo makes a real git repo with one commit, so worktree add works.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Ignore the worktree scratch dir, mirroring the real repo's .gitignore, so
	// git status on the main worktree isn't dirtied by nested worktree dirs.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("/.dctl-sessions/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", ".gitignore"},
		{"commit", "-m", "ignore worktree scratch dir"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// gitAt runs a git command in dir, failing the test on error. Identity is
// inherited from the repo config that initRepo already set.
func gitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s", args, out)
	}
}

// commitFile writes name=content in dir and commits it.
func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", msg)
}

func TestMergeIntoCleanMerge(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "")
	// Base file on the lead's main worktree.
	commitFile(t, repo, "f.txt", "base\n", "base file")

	// A worker branch (created off the base) that adds a new, non-conflicting file.
	if _, err := w.Create(repo, "worker", ""); err != nil {
		t.Fatal(err)
	}
	wp := w.Path(repo, "worker")
	commitFile(t, wp, "g.txt", "worker\n", "worker work")

	// Merge the worker branch into the repo's main worktree (the lead here).
	outcome, conflicts, err := w.MergeInto(repo, w.Branch("worker"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if outcome != MergeApplied {
		t.Fatalf("outcome = %v, want MergeApplied", outcome)
	}
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %v, want none", conflicts)
	}
	// Lead worktree must be clean and now contain the worker's file.
	if clean, _ := w.IsCleanAt(repo); !clean {
		t.Fatal("lead worktree not clean after merge")
	}
	if _, err := os.Stat(filepath.Join(repo, "g.txt")); err != nil {
		t.Fatalf("merged file missing: %v", err)
	}
}

func TestMergeIntoConflictAborts(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "")
	// Base file both sides will edit on the same line.
	commitFile(t, repo, "f.txt", "base\n", "base file")

	// Worker (branched off base) edits f.txt.
	if _, err := w.Create(repo, "worker", ""); err != nil {
		t.Fatal(err)
	}
	wp := w.Path(repo, "worker")
	commitFile(t, wp, "f.txt", "worker-change\n", "worker edit")

	// Lead diverges on the same line, committed (so the lead worktree is clean).
	commitFile(t, repo, "f.txt", "lead-change\n", "lead edit")

	outcome, conflicts, err := w.MergeInto(repo, w.Branch("worker"))
	if err != nil {
		t.Fatalf("conflict must not be an error, got: %v", err)
	}
	if outcome != MergeConflict {
		t.Fatalf("outcome = %v, want MergeConflict", outcome)
	}
	if len(conflicts) == 0 || conflicts[0] != "f.txt" {
		t.Fatalf("conflicts = %v, want [f.txt]", conflicts)
	}
	// Abort must have restored a clean worktree.
	if clean, _ := w.IsCleanAt(repo); !clean {
		t.Fatal("lead worktree not clean after conflict abort")
	}
}

func TestCreateUsesPassedRepo(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "")
	path, err := w.Create(repo, "feat1", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := filepath.Join(repo, ".dctl-sessions", "feat1")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}
}

func TestCreateNonGitRepoFallsBack(t *testing.T) {
	w := NewWorktreer(context.Background(), "")
	path, err := w.Create(t.TempDir(), "feat1", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if path != "" {
		t.Fatalf("non-git repo should yield empty path, got %q", path)
	}
}

func TestRemoveUsesPassedRepo(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "")
	if _, err := w.Create(repo, "feat1", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Remove(repo, "feat1", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".dctl-sessions", "feat1")); !os.IsNotExist(err) {
		t.Fatalf("worktree should be gone, stat err = %v", err)
	}
}

func TestCreateWithBaseBranchesOffIt(t *testing.T) {
	w := NewWorktreer(context.Background(), "")
	repo := initRepo(t)
	// Base worktree "alpha" gets a commit so its tip carries work.
	pa, err := w.Create(repo, "alpha", "")
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pa, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "work"}} {
		if out, err := exec.Command("git", append([]string{"-C", pa}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	// beta branches off session/alpha and must inherit f.txt.
	pb, err := w.Create(repo, "beta", "session/alpha")
	if err != nil {
		t.Fatalf("create beta on base: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pb, "f.txt")); err != nil {
		t.Fatalf("beta did not inherit alpha's committed work: %v", err)
	}
}

func TestCreateRejectsFlagLikeBase(t *testing.T) {
	w := NewWorktreer(context.Background(), "")
	repo := initRepo(t)
	path, err := w.Create(repo, "x", "--foo")
	if err == nil {
		t.Fatal("expected error for flag-like base, got nil")
	}
	if path != "" {
		t.Fatalf("path = %q, want empty", path)
	}
	if _, err := os.Stat(w.Path(repo, "x")); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should not have been created, stat err = %v", err)
	}
}

func TestBranchExistsAt(t *testing.T) {
	w := NewWorktreer(context.Background(), "")
	repo := initRepo(t)
	pathAlpha, err := w.Create(repo, "alpha", "")
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	exists, err := w.BranchExistsAt(pathAlpha, "session/alpha")
	if err != nil || !exists {
		t.Fatalf("session/alpha should exist: exists=%v err=%v", exists, err)
	}
	exists, err = w.BranchExistsAt(pathAlpha, "session/nope")
	if err != nil || exists {
		t.Fatalf("session/nope should not exist: exists=%v err=%v", exists, err)
	}
}

func TestBranchExistsAtNonRepoErrors(t *testing.T) {
	w := NewWorktreer(context.Background(), "")
	_, err := w.BranchExistsAt(t.TempDir(), "session/x")
	if err == nil {
		t.Fatal("expected error for non-repo path, got nil")
	}
}

func TestIsCleanAt(t *testing.T) {
	w := NewWorktreer(context.Background(), "")
	repo := initRepo(t)
	p, err := w.Create(repo, "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	clean, err := w.IsCleanAt(p)
	if err != nil || !clean {
		t.Fatalf("fresh worktree should be clean: clean=%v err=%v", clean, err)
	}
	if err := os.WriteFile(filepath.Join(p, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, err = w.IsCleanAt(p)
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Fatal("worktree with an untracked file should be dirty")
	}
}
