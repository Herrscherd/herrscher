package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestPlanRefusesToMoveASessionIntoAWorktreeNamedLikeTheInstanceID(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "worktree", "add", filepath.Join(repo, sessionsDir, "alice"), "-b", "session/alice-old")
	from := bareAlphaWorktree(t, repo)
	_, err := NewWorktreer(clitext.NewMessages(), "alice").PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if key := relocationFailureKey(t, err); key != clitext.WorktreeRelocateInstanceFolder {
		t.Fatalf("refusal key = %s, want %s: %v", key, clitext.WorktreeRelocateInstanceFolder, err)
	}
}

func TestPlanRefusesToMoveASessionIntoAnInstanceFolderThatIsAFile(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	if err := os.WriteFile(filepath.Join(repo, sessionsDir, "inst"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if key := relocationFailureKey(t, err); key != clitext.WorktreeRelocateInstanceFolder {
		t.Fatalf("refusal key = %s, want %s: %v", key, clitext.WorktreeRelocateInstanceFolder, err)
	}
}

func TestPlanRefusesToMoveAScratchFolderIntoAnInstanceFolderHoldingAGitEntry(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, scratchDir, "notes")
	if err := os.MkdirAll(filepath.Join(root, scratchDir, "inst", ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(from, 0o750); err != nil {
		t.Fatal(err)
	}
	_, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), BareSession{Name: "notes", Path: from, Scratch: true})
	if key := relocationFailureKey(t, err); key != clitext.WorktreeRelocateInstanceFolder {
		t.Fatalf("refusal key = %s, want %s: %v", key, clitext.WorktreeRelocateInstanceFolder, err)
	}
}

func TestPlanRefusesAWorktreeOfACopiedRepositoryAndItsRepairCommandLetsTheMoveThrough(t *testing.T) {
	original := trappedRepo(t)
	bareAlphaWorktree(t, original)
	originalBeta := filepath.Join(original, sessionsDir, "beta")
	gitIn(t, original, "worktree", "add", originalBeta, "-b", "session/beta")
	copied := filepath.Join(t.TempDir(), "co py $HOME 'x'")
	if out, err := exec.CommandContext(t.Context(), "cp", "-a", original, copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v\n%s", err, out)
	}
	from := filepath.Join(copied, sessionsDir, "alpha")
	err := planTrappedAlpha(t, from)
	failure := refusalParam(t, err, clitext.WorktreeRelocateForeignRepository, clitext.ParamTarget)
	if !strings.Contains(failure, original) {
		t.Fatalf("refusal names %s as the repository of the worktree, want %s", failure, original)
	}
	runShell(t, refusalCommand(t, err, clitext.WorktreeRelocateForeignRepository))
	if planErr := planTrappedAlpha(t, from); planErr != nil {
		t.Fatalf("plan once the worktree is linked to the copy: %v", planErr)
	}
	if got := gitIn(t, originalBeta, "rev-parse", "--path-format=absolute", "--git-common-dir"); got != filepath.Join(original, ".git") {
		t.Fatalf("the repair of alpha in the copy moved the original beta to %s", got)
	}
	if registered, err := NewWorktreer(clitext.NewMessages(), "inst").registeredWorktree(t.Context(), original, originalBeta); err != nil || !registered {
		t.Fatalf("the original no longer lists beta (%v):\n%s", err, gitIn(t, original, "worktree", "list", "--porcelain"))
	}
}

func TestPlanRefusesAnAlreadyMovedWorktreeOfACopiedRepositoryAndItsRepairCommandLetsTheMoveThrough(t *testing.T) {
	original := trappedRepo(t)
	from := bareAlphaWorktree(t, original)
	moved := NewWorktreer(clitext.NewMessages(), "inst").Path(original, "alpha")
	if err := os.MkdirAll(filepath.Dir(moved), 0o750); err != nil {
		t.Fatal(err)
	}
	gitIn(t, original, "worktree", "move", from, moved)
	copied := filepath.Join(t.TempDir(), "co py $HOME 'x'")
	if out, err := exec.CommandContext(t.Context(), "cp", "-a", original, copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v\n%s", err, out)
	}
	copiedFrom := filepath.Join(copied, sessionsDir, "alpha")
	err := planTrappedAlpha(t, copiedFrom)
	if path := refusalParam(t, err, clitext.WorktreeRelocateForeignRepository, clitext.ParamPath); !strings.Contains(path, filepath.Join(copied, sessionsDir, "inst", "alpha")) {
		t.Fatalf("refusal names %s, want the moved worktree of the copy", path)
	}
	runShell(t, refusalCommand(t, err, clitext.WorktreeRelocateForeignRepository))
	if planErr := planTrappedAlpha(t, copiedFrom); planErr != nil {
		t.Fatalf("plan once the moved worktree is linked to the copy: %v", planErr)
	}
}

func TestPlanRefusesAForeignWorktreeTheRepositoryKeepsNoEntryFor(t *testing.T) {
	original := initRepo(t)
	bareAlphaWorktree(t, original)
	copied := filepath.Join(t.TempDir(), "copy")
	if out, err := exec.CommandContext(t.Context(), "cp", "-a", original, copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v\n%s", err, out)
	}
	if err := os.RemoveAll(filepath.Join(copied, ".git", "worktrees", "alpha")); err != nil {
		t.Fatal(err)
	}
	_, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: filepath.Join(copied, sessionsDir, "alpha")})
	if key := relocationFailureKey(t, err); key != clitext.WorktreeRelocateForeignUnlinked {
		t.Fatalf("refusal key = %s, want %s: %v", key, clitext.WorktreeRelocateForeignUnlinked, err)
	}
}

func TestPlanRefusesToMoveASessionIntoARegisteredWorktreeThatLostItsGitEntry(t *testing.T) {
	repo := initRepo(t)
	instanceFolder := filepath.Join(repo, sessionsDir, "inst")
	gitIn(t, repo, "worktree", "add", instanceFolder, "-b", "session/kept")
	if err := os.Remove(filepath.Join(instanceFolder, ".git")); err != nil {
		t.Fatal(err)
	}
	from := bareAlphaWorktree(t, repo)
	_, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if key := relocationFailureKey(t, err); key != clitext.WorktreeRelocateInstanceFolder {
		t.Fatalf("refusal key = %s, want %s: %v", key, clitext.WorktreeRelocateInstanceFolder, err)
	}
}

func TestRelocatingTheBranchOfAGoneWorktreeLeavesNoStaleEntryHoldingIt(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	if err := os.RemoveAll(from); err != nil {
		t.Fatal(err)
	}
	w := NewWorktreer(clitext.NewMessages(), "inst")
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RenamesBranch() || plan.DirAtTarget() {
		t.Fatalf("plan = %+v, want only the branch renamed", plan)
	}
	if relocateErr := w.Relocate(t.Context(), plan); relocateErr != nil {
		t.Fatal(relocateErr)
	}
	gitIn(t, repo, "worktree", "add", w.Path(repo, "alpha"), "session/inst/alpha")
}

func TestRelocatingTheBranchOfAGoneWorktreeKeepsTheEntryOfAnotherMissingWorktree(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	mine := filepath.Join(repo, "usb", "mine")
	gitIn(t, repo, "worktree", "add", mine, "-b", "mine")
	if err := os.Rename(filepath.Dir(mine), filepath.Join(t.TempDir(), "unmounted")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(from); err != nil {
		t.Fatal(err)
	}
	w := NewWorktreer(clitext.NewMessages(), "inst")
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if err != nil {
		t.Fatal(err)
	}
	if relocateErr := w.Relocate(t.Context(), plan); relocateErr != nil {
		t.Fatal(relocateErr)
	}
	if registered, err := w.registeredWorktree(t.Context(), repo, mine); err != nil || !registered {
		t.Fatalf("the entry of the unmounted worktree %s is gone (%v):\n%s", mine, err, gitIn(t, repo, "worktree", "list", "--porcelain"))
	}
}

func TestAResumedRelocationRepairsAWorktreeMovedWithoutItsGitEntry(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	w := NewWorktreer(clitext.NewMessages(), "inst")
	to := w.Path(repo, "alpha")
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if err != nil {
		t.Fatal(err)
	}
	if relocateErr := w.Relocate(t.Context(), plan); relocateErr != nil {
		t.Fatal(relocateErr)
	}
	registered, err := w.registeredWorktree(t.Context(), repo, to)
	if err != nil || !registered {
		t.Fatalf("git does not list the moved worktree %s (%v):\n%s", to, err, gitIn(t, repo, "worktree", "list", "--porcelain"))
	}
	if got := gitIn(t, to, "symbolic-ref", "--short", "HEAD"); got != "session/inst/alpha" {
		t.Fatalf("moved worktree is on %q, want session/inst/alpha", got)
	}
}

func TestAResumedRelocationLeavesTheRelativeLinksOfASiblingWorktree(t *testing.T) {
	for _, gitMoved := range []bool{true, false} {
		resumeBesideARelativeSibling(t, gitMoved)
	}
}

func resumeBesideARelativeSibling(t *testing.T, gitMoved bool) {
	t.Helper()
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	beta := filepath.Join(repo, sessionsDir, "beta")
	gitIn(t, repo, "worktree", "add", "--relative-paths", beta, "-b", "session/beta")
	w := NewWorktreer(clitext.NewMessages(), "inst")
	to := w.Path(repo, "alpha")
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		t.Fatal(err)
	}
	if gitMoved {
		gitIn(t, repo, "worktree", "move", from, to)
	} else if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if err != nil {
		t.Fatal(err)
	}
	if relocateErr := w.Relocate(t.Context(), plan); relocateErr != nil {
		t.Fatal(relocateErr)
	}
	link, err := os.ReadFile(filepath.Join(beta, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(link), "gitdir: ..") {
		t.Fatalf("moved by git %v: the link of beta became %q, want it relative", gitMoved, link)
	}
	if registered, err := w.registeredWorktree(t.Context(), repo, to); err != nil || !registered {
		t.Fatalf("moved by git %v: git does not list %s (%v)", gitMoved, to, err)
	}
}

func TestAResumedRelocationRefusesWithoutACommandWhenBothBranchNamesAreGone(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	w := NewWorktreer(clitext.NewMessages(), "inst")
	to := w.Path(repo, "alpha")
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "worktree", "move", from, to)
	gitIn(t, repo, "update-ref", "-d", "refs/heads/session/alpha")
	_, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if key := relocationFailureKey(t, err); key != clitext.WorktreeRelocateBranchesGone {
		t.Fatalf("refusal key = %s, want %s: %v", key, clitext.WorktreeRelocateBranchesGone, err)
	}
	failure, _ := errors.AsType[FailureError](err)
	if _, ok := failure.Params[clitext.ParamCommand]; ok {
		t.Fatalf("refusal %v offers a command", err)
	}
}

func TestAResumedRelocationRefusesAWorktreeLeftOnABranchThatIsGone(t *testing.T) {
	repo := trappedRepo(t)
	from := bareAlphaWorktree(t, repo)
	w := NewWorktreer(clitext.NewMessages(), "inst")
	to := w.Path(repo, "alpha")
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "worktree", "move", from, to)
	gitIn(t, repo, "update-ref", "refs/heads/session/inst/alpha", "refs/heads/session/alpha")
	gitIn(t, repo, "update-ref", "-d", "refs/heads/session/alpha")
	_, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	runShell(t, refusalCommand(t, err, clitext.WorktreeRelocateHeadGone))
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if err != nil {
		t.Fatal(err)
	}
	if relocateErr := w.Relocate(t.Context(), plan); relocateErr != nil {
		t.Fatal(relocateErr)
	}
	if got := gitIn(t, to, "symbolic-ref", "--short", "HEAD"); got != "session/inst/alpha" {
		t.Fatalf("worktree is on %q, want session/inst/alpha", got)
	}
}

func TestPlanRefusesToRenameTheBranchOfAGoneWorktreeGitKeepsLocked(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	gitIn(t, repo, "worktree", "lock", from)
	if err := os.RemoveAll(from); err != nil {
		t.Fatal(err)
	}
	w := NewWorktreer(clitext.NewMessages(), "inst")
	_, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	runShell(t, refusalCommand(t, err, clitext.WorktreeRelocateLocked))
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if err != nil {
		t.Fatal(err)
	}
	if relocateErr := w.Relocate(t.Context(), plan); relocateErr != nil {
		t.Fatal(relocateErr)
	}
}

func TestRelocatingAScratchFolderCreatesTheInstanceFolderPrivate(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, scratchDir, "notes")
	if err := os.MkdirAll(from, 0o700); err != nil {
		t.Fatal(err)
	}
	w := NewWorktreer(clitext.NewMessages(), "inst")
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "notes", Path: from, Scratch: true})
	if err != nil {
		t.Fatal(err)
	}
	if relocateErr := w.Relocate(t.Context(), plan); relocateErr != nil {
		t.Fatal(relocateErr)
	}
	info, err := os.Stat(filepath.Dir(plan.ToPath))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != scratchDirMode {
		t.Fatalf("instance scratch folder mode = %o, want %o", mode, scratchDirMode)
	}
}
