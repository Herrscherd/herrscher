package bridge

import (
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/skills"
)

func skillRoots(cwd string, extra []string) []string {
	roots := []string{
		filepath.Join(cwd, ".claude", "skills"),
		learnedRoot(cwd),
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".claude", "skills"))
	}
	return append(roots, extra...)
}

func newSkillEngine(backend contracts.Backend) *skills.Engine {
	if n, ok := skillNativeOf(backend); ok && n.NativeSkills() {
		return nil
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		logger.Warn("config unreadable; skills disabled", "err", err)
		return nil
	}
	var extra []string
	if cfg.Skills != nil {
		if cfg.Skills.Enabled != nil && !*cfg.Skills.Enabled {
			return nil
		}
		extra = cfg.Skills.Roots
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return skills.NewEngine(skillRoots(cwd, extra))
}
