package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
)

// ErrHiddenRepository is the cause when git finds no repository for a folder that a .git entry on its path says belongs to one.
var ErrHiddenRepository = clitext.SentinelError("git finds no repository under a .git entry")

// HiddenRepositoryError names the .git entry on the path of a folder git finds no repository for, with the failure git gave.
type HiddenRepositoryError struct {
	Entry string
	Err   error
}

func (e *HiddenRepositoryError) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.Entry, ErrHiddenRepository, e.Err)
}

func (e *HiddenRepositoryError) Unwrap() []error { return []error{ErrHiddenRepository, e.Err} }

const (
	gitFatalExitCode     = 128
	discoveryFailMessage = "not a git repository (or any"
	gitEntryName         = ".git"
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
	failure := fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), err)
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok || !repositoryDiscoveryFailed(exitErr.ExitCode(), stderr.String()) {
		return false, failure
	}
	entry, err := gitEntryOnPath(dir)
	if err != nil {
		return false, err
	}
	if entry != "" {
		return false, &HiddenRepositoryError{Entry: entry, Err: failure}
	}
	return true, nil
}

func repositoryDiscoveryFailed(exitCode int, stderr string) bool {
	return exitCode == gitFatalExitCode && strings.Contains(stderr, discoveryFailMessage)
}

func gitEntryOnPath(dir string) (string, error) {
	current, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		entry := filepath.Join(current, gitEntryName)
		if _, err := os.Lstat(entry); err == nil {
			return entry, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}
