//go:build windows

package worktree

import (
	"os/exec"
	"testing"
)

func makeDirectoryLink(t *testing.T, link, target string) {
	t.Helper()
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create directory junction: %v\n%s", err, out)
	}
}
