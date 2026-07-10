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
	return dir
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
