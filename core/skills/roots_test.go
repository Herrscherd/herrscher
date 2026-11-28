package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootsPutRepoLearnedGlobalThenExtra(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator), "work", "repo")
	roots := Roots(cwd, []string{"/opt/x"})
	if roots[0] != filepath.Join(cwd, ".claude", "skills") {
		t.Fatalf("the repository skills must come first, got %v", roots)
	}
	if roots[1] != LearnedRoot(cwd) {
		t.Fatalf("the learned root must come second, got %v", roots)
	}
	if roots[len(roots)-1] != "/opt/x" {
		t.Fatalf("extra roots must come last, got %v", roots)
	}
}

func TestRootsWithoutHomeOmitTheGlobalRoot(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("the home directory resolves without $HOME on this platform")
	}
	roots := Roots("/work/repo", nil)
	if len(roots) != 2 {
		t.Fatalf("without a home directory the roots are the repository and the learned root only, got %v", roots)
	}
}
