package skills

import "path/filepath"

const learnedHomeDir = ".herrscher"

// LearnedHome is the directory a vendor is given so it loads the skills learned in dir.
func LearnedHome(dir string) string {
	return filepath.Join(dir, learnedHomeDir)
}

// LearnedRoot is where the skills learned in dir are projected, one SKILL.md per skill.
func LearnedRoot(dir string) string {
	return filepath.Join(LearnedHome(dir), ".claude", "skills")
}

// LegacyLearnedRoot is where an earlier release projected them, removed at the next projection.
func LegacyLearnedRoot(dir string) string {
	return filepath.Join(LearnedHome(dir), "skills")
}
