package worktree

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func refusalCommand(t *testing.T, err error, key clitext.Key) string {
	t.Helper()
	if got := relocationFailureKey(t, err); got != string(key) {
		t.Fatalf("plan refused with %s, want %s: %v", got, key, err)
	}
	failure, _ := errors.AsType[FailureError](err)
	command, ok := failure.Params[clitext.ParamCommand].(string)
	if !ok || command == "" {
		t.Fatalf("refusal %v carries no command", err)
	}
	return command
}

func runShell(t *testing.T, command string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", command, err, out)
	}
}

func TestPlanRefusesALockedWorktreeAndItsUnlockCommandLetsTheMoveThrough(t *testing.T) {
	repo := initRepo(t)
	from := bareAlphaWorktree(t, repo)
	gitIn(t, repo, "worktree", "lock", "--reason", "kept by a tool", from)
	w := NewWorktreer(clitext.NewMessages(), "inst")
	_, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	runShell(t, refusalCommand(t, err, clitext.WorktreeRelocateLocked))
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if err != nil {
		t.Fatalf("plan once unlocked: %v", err)
	}
	if err := w.Relocate(t.Context(), plan); err != nil {
		t.Fatalf("move once unlocked: %v", err)
	}
}

func worktreeWithSubmodule(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	lib := initRepo(t)
	repo := initRepo(t)
	gitIn(t, repo, "submodule", "add", "-q", "file://"+lib, "lib")
	gitIn(t, repo, "commit", "-q", "-m", "lib")
	from := bareAlphaWorktree(t, repo)
	gitIn(t, from, "submodule", "update", "--init", "-q")
	return repo, from
}

func TestPlanRefusesAWorktreeWithSubmodulesAndItsCommandsMoveIt(t *testing.T) {
	repo, from := worktreeWithSubmodule(t)
	w := NewWorktreer(clitext.NewMessages(), "inst")
	_, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	runShell(t, refusalCommand(t, err, clitext.WorktreeRelocateSubmodules))
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if err != nil {
		t.Fatalf("plan once moved by hand: %v", err)
	}
	to := w.Path(repo, "alpha")
	if !plan.DirAtTarget() || plan.MovesDir() || plan.RenamesBranch() {
		t.Fatalf("plan after the commands = %+v, want the worktree already at %s with its branch renamed", plan, to)
	}
	if branch := gitIn(t, to, "symbolic-ref", "--short", "HEAD"); branch != "session/inst/alpha" {
		t.Fatalf("moved worktree is on %q", branch)
	}
	if top := gitIn(t, filepath.Join(to, "lib"), "rev-parse", "--show-toplevel"); top != filepath.Join(to, "lib") {
		t.Fatalf("submodule top level after the move = %q", top)
	}
	if status := gitIn(t, to, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Fatalf("moved worktree is not clean: %q", status)
	}
}
