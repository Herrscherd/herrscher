package skills

import (
	"os"
	"path/filepath"
)

const learnedHomeDir = ".herrscher"

// LearnedHome is the directory a vendor is given so it loads the skills learned in dir.
func LearnedHome(dir string) string {
	return filepath.Join(dir, learnedHomeDir)
}

// LearnedRoot is where the skills learned in dir are projected, one SKILL.md per skill.
func LearnedRoot(dir string) string {
	return filepath.Join(LearnedHome(dir), ".claude", "skills")
}

// LinkedLearnedDir names the first directory of the projection under dir that is a symbolic link, or "".
func LinkedLearnedDir(dir string) string {
	home := LearnedHome(dir)
	for _, candidate := range []string{home, filepath.Join(home, ".claude"), LearnedRoot(dir), LegacyLearnedRoot(dir)} {
		if info, err := os.Lstat(candidate); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return candidate
		}
	}
	return ""
}

// LegacyLearnedRoot is where an earlier release projected them, removed at the next projection.
func LegacyLearnedRoot(dir string) string {
	return filepath.Join(LearnedHome(dir), "skills")
}
