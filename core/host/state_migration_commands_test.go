package host

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func runShellCommand(t *testing.T, command string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", command, err, out)
	}
}

func movedRepositoryWorktree(t *testing.T) string {
	t.Helper()
	repo := tempRepo(t)
	worktreePath := filepath.Join(repo, ".herrscher-sessions", "inst", "alpha")
	gitOutput(t, repo, "worktree", "add", "-q", worktreePath, "-b", "session/inst/alpha")
	moved := filepath.Join(t.TempDir(), "re po $HOME 'x'")
	if err := os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(moved, ".herrscher-sessions", "inst", "alpha")
}

func TestTheCommandsOfACheckpointInspectRefusalRunOnAFolderWithSpacesDollarsAndQuotes(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	worktreePath := movedRepositoryWorktree(t)
	sessions := []state.Session{{Name: "alpha", Incarnation: migratedIncarnation, Worktree: worktreePath, Dir: worktreePath}}

	_, err := planCheckpointRekeys(t.Context(), clitext.NewMessages(), sessions)
	refusal, ok := errors.AsType[*stateMigrationRefusedError](err)
	if !ok || refusal.key != clitext.MigrationCheckpointInspect {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.MigrationCheckpointInspect)
	}
	for _, param := range []string{clitext.ParamTrust, clitext.ParamRecovery, clitext.ParamCommand} {
		command, isText := refusal.params[param].(string)
		if !isText {
			t.Fatalf("refusal carries no %s: %+v", param, refusal.params)
		}
		runShellCommand(t, command)
	}
	if trusted := gitOutput(t, worktreePath, "config", "--global", "--get-all", "safe.directory"); trusted != worktreePath {
		t.Fatalf("safe.directory = %q, want %q", trusted, worktreePath)
	}
	if _, err := planCheckpointRekeys(t.Context(), clitext.NewMessages(), sessions); err != nil {
		t.Fatalf("plan once the commands ran: %v", err)
	}
}
