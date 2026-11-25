package skills

import "path/filepath"

const learnedHomeDir = ".herrscher"

func LearnedHome(dir string) string {
	return filepath.Join(dir, learnedHomeDir)
}

func LearnedRoot(dir string) string {
	return filepath.Join(LearnedHome(dir), ".claude", "skills")
}

func LegacyLearnedRoot(dir string) string {
	return filepath.Join(LearnedHome(dir), "skills")
}
