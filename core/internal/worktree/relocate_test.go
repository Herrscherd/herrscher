package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func bareAlphaWorktree(t *testing.T, repo string) string {
	t.Helper()
	path := filepath.Join(repo, sessionsDir, "alpha")
	gitIn(t, repo, "worktree", "add", path, "-b", "session/alpha")
	return path
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func relocationFailureKey(t *testing.T, err error) string {
	t.Helper()
	failure, ok := errors.AsType[FailureError](err)
	if !ok {
		t.Fatalf("err = %v, want a FailureError", err)
	}
	return string(failure.Key)
}

func TestRelocateMovesABareWorktreeAndRenamesItsBranch(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	if err := os.WriteFile(filepath.Join(from, "draft.txt"), []byte("uncommitted"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewWorktreer(clitext.NewMessages(), "inst")
	plan, err := w.PlanBareRelocation(t.Context(), "alpha", from, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Relocate(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	to := w.Path(repo, "alpha")
	if plan.ToPath != to || plan.ToBranch != "session/inst/alpha" {
		t.Fatalf("plan = %+v, want %s on session/inst/alpha", plan, to)
	}
	if got := gitIn(t, to, "symbolic-ref", "--short", "HEAD"); got != "session/inst/alpha" {
		t.Fatalf("moved worktree is on %q, want session/inst/alpha", got)
	}
	if data, err := os.ReadFile(filepath.Join(to, "draft.txt")); err != nil || string(data) != "uncommitted" {
		t.Fatalf("uncommitted work after the move = %q, %v", data, err)
	}
	if _, err := os.Lstat(from); !os.IsNotExist(err) {
		t.Fatalf("old worktree still present: %v", err)
	}
}

func TestPlanBareRelocationRefusesAnExistingTarget(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	w := NewWorktreer(clitext.NewMessages(), "inst")
	if err := os.MkdirAll(w.Path(repo, "alpha"), 0o750); err != nil {
		t.Fatal(err)
	}
	_, err := w.PlanBareRelocation(t.Context(), "alpha", from, false)
	if key := relocationFailureKey(t, err); key != clitext.WorktreeRelocateTargetExists {
		t.Fatalf("refusal key = %s, want %s", key, clitext.WorktreeRelocateTargetExists)
	}
}

func TestPlanBareRelocationRefusesAnExistingTargetBranch(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	gitIn(t, repo, "branch", "session/inst/alpha")
	_, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), "alpha", from, false)
	if key := relocationFailureKey(t, err); key != clitext.WorktreeRelocateBranchExists {
		t.Fatalf("refusal key = %s, want %s", key, clitext.WorktreeRelocateBranchExists)
	}
}

func TestPlanBareRelocationRefusesABranchThatTracksARemote(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	gitIn(t, repo, "config", "branch.session/alpha.remote", "origin")
	gitIn(t, repo, "config", "branch.session/alpha.merge", "refs/heads/session/alpha")
	_, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), "alpha", from, false)
	if key := relocationFailureKey(t, err); key != clitext.WorktreeRelocateUpstream {
		t.Fatalf("refusal key = %s, want %s", key, clitext.WorktreeRelocateUpstream)
	}
}

func TestPlanBareRelocationRefusesAWorktreeOnAnotherBranch(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	gitIn(t, from, "switch", "-q", "-c", "elsewhere")
	_, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), "alpha", from, false)
	if key := relocationFailureKey(t, err); key != clitext.WorktreeOtherBranch {
		t.Fatalf("refusal key = %s, want %s", key, clitext.WorktreeOtherBranch)
	}
}

func TestPlanBareRelocationHasNothingLeftAfterAnEarlierMove(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	w := NewWorktreer(clitext.NewMessages(), "inst")
	first, err := w.PlanBareRelocation(t.Context(), "alpha", from, false)
	if err != nil {
		t.Fatal(err)
	}
	if relocateErr := w.Relocate(t.Context(), first); relocateErr != nil {
		t.Fatal(relocateErr)
	}
	again, err := w.PlanBareRelocation(t.Context(), "alpha", from, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Pending() || again.ToPath != first.ToPath {
		t.Fatalf("second plan = %+v, want nothing pending toward %s", again, first.ToPath)
	}
}

func TestRelocateMovesABareScratchDirectory(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, scratchDir, "notes")
	if err := os.MkdirAll(from, 0o700); err != nil {
		t.Fatal(err)
	}
	w := NewWorktreer(clitext.NewMessages(), "inst")
	plan, err := w.PlanBareRelocation(context.Background(), "notes", from, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Relocate(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(w.ScratchPath(root, "notes")); err != nil || !info.IsDir() {
		t.Fatalf("scratch directory not at %s: %v", w.ScratchPath(root, "notes"), err)
	}
}

func TestInBareLayoutTellsTheOldLayoutFromTheInstanceOne(t *testing.T) {
	cases := []struct {
		path    string
		scratch bool
		want    bool
	}{
		{"/w/app/.herrscher-sessions/alpha", false, true},
		{"/w/app/.herrscher-sessions/inst/alpha", false, false},
		{"/w/.herrscher-scratch/alpha", true, true},
		{"/w/.herrscher-scratch/inst/alpha", true, false},
		{"/w/app", false, false},
		{"", false, false},
	}
	for _, tc := range cases {
		if got := InBareLayout("alpha", tc.path, tc.scratch); got != tc.want {
			t.Errorf("InBareLayout(%q, scratch=%v) = %v, want %v", tc.path, tc.scratch, got, tc.want)
		}
	}
}
