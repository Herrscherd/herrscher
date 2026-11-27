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

func worktreeWithSubmodule(t *testing.T) (string, string, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	lib := initRepo(t)
	writeFile(t, filepath.Join(lib, ".gitignore"), "/.herrscher-sessions/\n.env\n/build/\n")
	gitIn(t, lib, "commit", "-q", "-am", "ignore")
	repo := initRepo(t)
	gitIn(t, repo, "submodule", "add", "-q", "file://"+lib, "lib")
	gitIn(t, repo, "commit", "-q", "-m", "lib")
	from := bareAlphaWorktree(t, repo)
	gitIn(t, from, "submodule", "update", "--init", "-q")
	gitIn(t, repo, "config", "submodule.lib.url", lib)
	gitIn(t, repo, "config", "submodule.lib.update", "rebase")
	writeFile(t, filepath.Join(from, "lib", ".env"), "TOKEN=secret\n")
	writeFile(t, filepath.Join(from, "lib", "build", "out"), "artifact\n")
	return repo, from, lib
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPlanRefusesAWorktreeWithSubmodulesAndItsCommandsMoveItWithoutLosingAnything(t *testing.T) {
	repo, from, lib := worktreeWithSubmodule(t)
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
	for _, ignored := range []string{".env", filepath.Join("build", "out")} {
		if _, statErr := os.Stat(filepath.Join(to, "lib", ignored)); statErr != nil {
			t.Fatalf("file the submodule ignores lost by the move: %v", statErr)
		}
	}
	if url := gitIn(t, repo, "config", "submodule.lib.url"); url != lib {
		t.Fatalf("submodule.lib.url after the move = %q, want the local %q", url, lib)
	}
	if update := gitIn(t, repo, "config", "submodule.lib.update"); update != "rebase" {
		t.Fatalf("submodule.lib.update after the move = %q, want rebase", update)
	}
}

func TestPlanRefusesABranchStandingWhereTheInstanceBranchFolderGoesAndItsCommandLetsTheMoveThrough(t *testing.T) {
	repo := trappedRepo(t)
	from := bareAlphaWorktree(t, repo)
	gitIn(t, repo, "branch", "session/inst")
	w := NewWorktreer(clitext.NewMessages(), "inst")
	_, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	runShell(t, refusalCommand(t, err, clitext.WorktreeRelocateBranchBlocked))
	plan, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	if err != nil {
		t.Fatalf("plan once the branch is renamed: %v", err)
	}
	if err := w.Relocate(t.Context(), plan); err != nil {
		t.Fatalf("move once the branch is renamed: %v", err)
	}
	if kept := gitIn(t, repo, "rev-parse", "--verify", "kept/session/inst"); kept == "" {
		t.Fatal("the blocking branch is gone instead of renamed")
	}
}
