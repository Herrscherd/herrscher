//go:build !windows

package worktree

import "path/filepath"

func resolveExistingPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
