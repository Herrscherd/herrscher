package worktree

import (
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
	if err := planTrappedAlpha(t, from); err != nil {
		t.Fatalf("plan once the worktree is linked to the copy: %v", err)
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
	if err := w.Relocate(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "worktree", "add", w.Path(repo, "alpha"), "session/inst/alpha")
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
	if err := w.Relocate(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	registered, err := w.registeredWorktree(t.Context(), repo, to)
	if err != nil || !registered {
		t.Fatalf("git does not list the moved worktree %s (%v):\n%s", to, err, gitIn(t, repo, "worktree", "list", "--porcelain"))
	}
	if got := gitIn(t, to, "symbolic-ref", "--short", "HEAD"); got != "session/inst/alpha" {
		t.Fatalf("moved worktree is on %q, want session/inst/alpha", got)
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
	if err := w.Relocate(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, to, "symbolic-ref", "--short", "HEAD"); got != "session/inst/alpha" {
		t.Fatalf("worktree is on %q, want session/inst/alpha", got)
	}
}
