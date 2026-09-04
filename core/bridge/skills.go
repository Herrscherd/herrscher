package bridge

import (
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/skills"
)

// skillRoots is the ordered skill search path: the session workspace (the bridge
// runs with cwd = workspace), then the skills this agent taught itself, then the
// user-global skills, then any extra roots from config.
//
// The order is the policy, and Discover's de-duplication by name, earlier root
// winning, is what enforces it. A repository's own skill beats one the agent
// taught itself, so a self-authored procedure can never shadow one the project
// committed. A learned skill beats a machine-wide playbook, because this agent's
// experience on this project is more specific than an instruction to the machine.
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

// newSkillEngine builds the per-session skill engine, or returns nil when skills
// are disabled: the backend loads skills natively (contracts.SkillNative), or
// config turns the feature off. A nil engine means the hub injects nothing.
func newSkillEngine(backend contracts.Backend) *skills.Engine {
	if n, ok := backend.(contracts.SkillNative); ok && n.NativeSkills() {
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
