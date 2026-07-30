//go:build !windows

package worktree

import (
	"os"
	"testing"
)

func makeDirectoryLink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create directory symlink: %v", err)
	}
}
