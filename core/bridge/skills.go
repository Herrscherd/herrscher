package bridge

import (
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/skills"
)

// skillRoots is the ordered skill search path: the session workspace (the bridge
// runs with cwd = workspace), then the user-global skills, then any extra roots
// from config. Earlier roots win de-dup, so a repo skill overrides a global one.
func skillRoots(cwd string, extra []string) []string {
	roots := []string{filepath.Join(cwd, ".claude", "skills")}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".claude", "skills"))
	}
	return append(roots, extra...)
}

// newSkillEngine builds the per-session skill engine, or returns nil when skills
// are disabled: the backend loads skills natively (contracts.SkillNative), or
// config turns the feature off. A nil engine means the hub injects nothing.
func newSkillEngine(backend contracts.Backend) *skills.Engine {
	if n, ok := backend.(contracts.SkillNative); ok && n.NativeSkills() {
		return nil
	}
	// A missing config is not an error (Load returns the zero Config), so an
	// error here means the file is there but malformed — and silently ignoring it
	// would ignore a skills.enabled:false the operator meant to be honored.
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		logger.Warn("config unreadable; falling back to default skill roots", "err", err)
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
