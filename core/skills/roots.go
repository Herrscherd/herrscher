package skills

import (
	"os"
	"path/filepath"
)

// Roots is the search path of a session whose working directory is cwd: its
// repository skills, its learned skills, the user's skills, then the extra roots
// of the config. The engine and the /skills panel both read this list, so the
// panel lists every skill the menu can hide.
func Roots(cwd string, extra []string) []string {
	roots := []string{
		filepath.Join(cwd, ".claude", "skills"),
		LearnedRoot(cwd),
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".claude", "skills"))
	}
	return append(roots, extra...)
}
