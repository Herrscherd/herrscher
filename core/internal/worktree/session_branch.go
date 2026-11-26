package worktree

import (
	"context"
	"path/filepath"
)

// BranchReader reads which branch a session worktree has checked out.
type BranchReader interface {
	Branch(name string) string
	BranchAt(ctx context.Context, path string) (string, error)
}

// SessionBranch returns the branch checked out in the session worktree
// recorded at path. A renamed session keeps the worktree and branch it was
// created with, so when the worktree cannot be read the branch is named after
// the recorded directory, never after the session's current name, and the
// read error comes back beside it.
func SessionBranch(ctx context.Context, worktrees BranchReader, path string) (string, error) {
	branch, err := worktrees.BranchAt(ctx, path)
	if err != nil || branch == "" {
		return worktrees.Branch(filepath.Base(path)), err
	}
	return branch, nil
}
