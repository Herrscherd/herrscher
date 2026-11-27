package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const trappedFolderName = "re po $HOME 'x'"

func trappedRepo(t *testing.T) string {
	t.Helper()
	trapped := filepath.Join(t.TempDir(), trappedFolderName)
	if err := os.Rename(initRepo(t), trapped); err != nil {
		t.Fatal(err)
	}
	return trapped
}

func refusalParam(t *testing.T, err error, key clitext.Key, param string) string {
	t.Helper()
	refusalCommand(t, err, key)
	failure, _ := errors.AsType[FailureError](err)
	command, ok := failure.Params[param].(string)
	if !ok || command == "" {
		t.Fatalf("refusal %v carries no %s", err, param)
	}
	return command
}

func planTrappedAlpha(t *testing.T, from string) error {
	t.Helper()
	_, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	return err
}

func TestTheUnsetUpstreamCommandRunsOnAFolderWithSpacesDollarsAndQuotes(t *testing.T) {
	repo := trappedRepo(t)
	from := bareAlphaWorktree(t, repo)
	gitIn(t, repo, "config", "branch.session/alpha.remote", "origin")
	gitIn(t, repo, "config", "branch.session/alpha.merge", "refs/heads/session/alpha")
	runShell(t, refusalCommand(t, planTrappedAlpha(t, from), clitext.WorktreeRelocateUpstream))
	if err := planTrappedAlpha(t, from); err != nil {
		t.Fatalf("plan once the tracking is dropped: %v", err)
	}
}

func pushedTrappedAlpha(t *testing.T) string {
	t.Helper()
	repo := trappedRepo(t)
	from := bareAlphaWorktree(t, repo)
	remote := filepath.Join(t.TempDir(), "re mote $x 'y'.git")
	gitIn(t, filepath.Dir(remote), "init", "-q", "--bare", remote)
	gitIn(t, repo, "remote", "add", "origin", remote)
	gitIn(t, repo, "push", "-q", "origin", "session/alpha")
	return from
}

func TestTheRemoteBranchCommandsRunOnAFolderWithSpacesDollarsAndQuotes(t *testing.T) {
	for _, param := range []string{clitext.ParamCommand, clitext.ParamRecovery} {
		t.Run(param, func(t *testing.T) {
			from := pushedTrappedAlpha(t)
			runShell(t, refusalParam(t, planTrappedAlpha(t, from), clitext.WorktreeRelocateRemoteBranch, param))
			if err := planTrappedAlpha(t, from); err != nil {
				t.Fatalf("plan once the remote branch is dropped: %v", err)
			}
		})
	}
}

func TestTheSubmoduleCommandsRunOnAFolderWithSpacesDollarsAndQuotes(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	lib := initRepo(t)
	repo := trappedRepo(t)
	gitIn(t, repo, "submodule", "add", "-q", "file://"+lib, "lib")
	gitIn(t, repo, "commit", "-q", "-m", "lib")
	from := bareAlphaWorktree(t, repo)
	gitIn(t, from, "submodule", "update", "--init", "-q")
	runShell(t, refusalCommand(t, planTrappedAlpha(t, from), clitext.WorktreeRelocateSubmodules))
	if err := planTrappedAlpha(t, from); err != nil {
		t.Fatalf("plan once moved by hand: %v", err)
	}
}
