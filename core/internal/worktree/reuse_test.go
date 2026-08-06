package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A session that comes back finds its own worktree still there — after a crash,
// a kill, or a daemon restart that never got to close it. Refusing to start on
// the grounds that its own directory is in the way was the bug: the session was
// left unable to run at all until someone deleted the directory by hand.
func TestCreateReusesTheSessionsOwnWorktree(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "inst")

	first, err := w.Create(repo, "ch-1", "")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first, "wip.txt"), []byte("half a thought"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := w.Create(repo, "ch-1", "")
	if err != nil {
		t.Fatalf("the session must be able to come back to its own worktree: %v", err)
	}
	if second != first {
		t.Fatalf("it must come back to the same worktree: %q then %q", first, second)
	}
	// Reuse means reuse: uncommitted work in the tree is the session's, and
	// silently starting from a clean one would be the same loss as refusing.
	if _, err := os.Stat(filepath.Join(second, "wip.txt")); err != nil {
		t.Fatalf("the work already in the tree must survive the reuse: %v", err)
	}
}

// A registration git still holds for a directory that is gone would make the add
// fail on git's own record alone. That state is stale, not a conflict: prune it
// and give the session a fresh tree.
func TestCreateRecoversFromAStaleRegistration(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "inst")

	path, err := w.Create(repo, "ch-1", "")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}

	again, err := w.Create(repo, "ch-1", "")
	if err != nil {
		t.Fatalf("a registration for a directory that is gone must be pruned, not fatal: %v", err)
	}
	if again != path {
		t.Fatalf("path = %q, want %q", again, path)
	}
	if _, err := os.Stat(filepath.Join(again, ".git")); err != nil {
		t.Fatalf("the recreated worktree must be a real one: %v", err)
	}
}

// A directory git knows nothing about is somebody else's. Removing it to make
// room is how a session would destroy work nobody asked it to touch, so the
// state is reported instead.
func TestCreateRefusesAForeignDirectory(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "inst")

	path := w.Path(repo, "ch-1")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := w.Create(repo, "ch-1", ""); err == nil {
		t.Fatal("a foreign directory in the way must be reported, not adopted")
	} else if !strings.Contains(err.Error(), "not a git worktree") {
		t.Fatalf("the error must say what is actually wrong: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "notes.txt")); err != nil {
		t.Fatalf("the foreign directory's contents must be left alone: %v", err)
	}
}

// A worktree at the session's path but on another branch is not the session's
// tree. Taking it over would move somebody else's checkout under them.
func TestCreateRefusesAWorktreeOnAnotherBranch(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "inst")

	path := w.Path(repo, "ch-1")
	gitAt(t, repo, "worktree", "add", path, "-b", "somebody-elses")

	_, err := w.Create(repo, "ch-1", "")
	if err == nil {
		t.Fatal("a worktree on another branch must not be taken over")
	}
	if !strings.Contains(err.Error(), "somebody-elses") {
		t.Fatalf("the error must name the branch found: %v", err)
	}
}

// The caller rolls back on a later failure, and must only ever remove what it
// created. A worktree that was already there predates the call, and the work
// inside it with it.
func TestPreExistingDistinguishesReuseFromCreation(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreer(context.Background(), "inst")

	if w.PreExisting(repo, "ch-1") {
		t.Fatal("a session with no worktree yet must not read as pre-existing")
	}
	if _, err := w.Create(repo, "ch-1", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !w.PreExisting(repo, "ch-1") {
		t.Fatal("a worktree already on disk must read as pre-existing")
	}
}
