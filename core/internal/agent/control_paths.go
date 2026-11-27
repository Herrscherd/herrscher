package agent

import (
	"slices"
	"strings"
)

const gitDir = ".git"

// ControlPaths lists the first path segment of .git and of every file herrscher
// materializes in a worktree for the agent: a file under one of them steers the
// agent or its repository, so nothing copied from outside may land there.
func ControlPaths() []string {
	paths := []string{gitDir}
	for _, exclude := range materializedGitExcludes() {
		first, _, _ := strings.Cut(strings.TrimPrefix(exclude, "/"), "/")
		if !slices.Contains(paths, first) {
			paths = append(paths, first)
		}
	}
	return paths
}
