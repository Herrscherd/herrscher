package checkpoint

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "tui/frame.go", "package tui\n")
	run(t, dir, "add", "-A")
	run(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")
	return dir
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestTakeLeavesIndexAndWorktreeAlone(t *testing.T) {
	var store Store
	dir := initRepo(t)
	write(t, dir, "tui/frame.go", "package tui // edited\n")
	write(t, dir, "new.txt", "untracked\n")
	indexBefore := run(t, dir, "ls-files", "-s")
	stagedBefore := run(t, dir, "diff", "--cached", "--name-only")

	if _, err := store.Take(context.Background(), dir, "alpha", Meta{Turn: "t1", Text: "premier message"}); err != nil {
		t.Fatal(err)
	}

	if got := run(t, dir, "ls-files", "-s"); got != indexBefore {
		t.Fatalf("index changed by checkpoint:\nbefore %s\nafter %s", indexBefore, got)
	}
	if got := run(t, dir, "diff", "--cached", "--name-only"); got != stagedBefore {
		t.Fatalf("staged set changed: %q", got)
	}
	if got := read(t, dir, "new.txt"); got != "untracked\n" {
		t.Fatalf("working tree changed: %q", got)
	}
}

func TestListReturnsCheckpointsInOrderWithMessage(t *testing.T) {
	var store Store
	dir := initRepo(t)
	ctx := context.Background()
	write(t, dir, "a.txt", "one\n")
	if _, err := store.Take(ctx, dir, "alpha", Meta{Turn: "t1", Text: "ajoute un fichier"}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "two\n")
	if _, err := store.Take(ctx, dir, "alpha", Meta{Turn: "t2", Text: "modifie le fichier"}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(ctx, dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Index != 1 || entries[1].Index != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Text != "ajoute un fichier" || entries[1].Turn != "t2" {
		t.Fatalf("entries lost their message or turn: %+v", entries)
	}
	other, err := store.List(ctx, dir, "beta")
	if err != nil || len(other) != 0 {
		t.Fatalf("other session sees checkpoints: %+v %v", other, err)
	}
}

func TestChangesListsFilesTouchedBetweenCheckpoints(t *testing.T) {
	var store Store
	dir := initRepo(t)
	ctx := context.Background()
	first, err := store.Take(ctx, dir, "alpha", Meta{Turn: "t1", Text: "premier"})
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "tui/frame.go", "package tui\nvar x = 1\n")
	write(t, dir, "added.go", "package x\n")
	second, err := store.Take(ctx, dir, "alpha", Meta{Turn: "t2", Text: "second"})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := Changes(ctx, dir, first.Commit, second.Commit)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]Change)
	for _, change := range changes {
		got[change.Path] = change
	}
	if _, ok := got["tui/frame.go"]; !ok || len(got) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	if got["tui/frame.go"].Added != 1 || got["tui/frame.go"].Removed != 0 {
		t.Fatalf("numstat wrong: %+v", got["tui/frame.go"])
	}
}

func TestRestoreRevertsCodeToCheckpointAndUndoBringsBackCurrent(t *testing.T) {
	var store Store
	dir := initRepo(t)
	ctx := context.Background()
	if _, err := store.Take(ctx, dir, "alpha", Meta{Turn: "t1", Text: "avant"}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "tui/frame.go", "package tui // after\n")
	write(t, dir, "created.go", "package x\n")

	if _, err := store.Restore(ctx, dir, "alpha", 1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "tui/frame.go"); got != "package tui\n" {
		t.Fatalf("frame.go not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "created.go")); !os.IsNotExist(err) {
		t.Fatalf("file created after checkpoint still present: %v", err)
	}
	if !store.HasUndo(ctx, dir, "alpha") {
		t.Fatal("restore did not keep an undo point")
	}

	if err := store.Undo(ctx, dir, "alpha"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "tui/frame.go"); got != "package tui // after\n" {
		t.Fatalf("undo did not bring back the state before restore: %q", got)
	}
	if got := read(t, dir, "created.go"); got != "package x\n" {
		t.Fatalf("undo lost the file created after checkpoint: %q", got)
	}
}

func TestUndoWithoutRestoreFails(t *testing.T) {
	var store Store
	dir := initRepo(t)
	if err := store.Undo(context.Background(), dir, "alpha"); !errors.Is(err, ErrNoUndo) {
		t.Fatalf("undo err = %v", err)
	}
}

func TestRejectsSessionNameWithSeparator(t *testing.T) {
	var store Store
	dir := initRepo(t)
	if _, err := store.Take(context.Background(), dir, "a/b", Meta{}); err == nil {
		t.Fatal("session name with / accepted")
	}
}

func TestRemoveInsideNeverFollowsASymlinkOutOfTheWorktree(t *testing.T) {
	worktree := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(worktree, "link")); err != nil {
		t.Fatal(err)
	}
	if err := removeInside(worktree, filepath.Join("link", "victim.txt")); err == nil {
		t.Fatal("removing through a symlink that leaves the worktree must fail")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the file outside the worktree was deleted: %v", err)
	}
}
