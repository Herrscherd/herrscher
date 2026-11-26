package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	gitFatalExitCode = 128
	notARepoMessage  = "not a git repository"
)

// OutsideAnyRepository reports whether dir belongs to no git repository. Any other failure of git is returned as an error, never as outside.
func OutsideAnyRepository(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--git-dir")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == gitFatalExitCode && strings.Contains(stderr.String(), notARepoMessage) {
		return true, nil
	}
	return false, fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), err)
}
