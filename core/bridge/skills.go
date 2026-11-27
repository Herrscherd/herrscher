package bridge

import (
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
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
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return newSkillEngineIn(backend, cwd)
}

func newSkillEngineIn(backend contracts.Backend, cwd string) *skills.Engine {
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
	eng := skills.NewEngine(skillRoots(cwd, extra))
	eng.SetBudget(skillBudget(cfg.Skills))
	return eng
}

func skillBudget(cfg *config.SkillsConfig) skills.Budget {
	if cfg == nil || cfg.Budget == nil {
		return skills.Budget{}
	}
	return skills.Budget{Menu: cfg.Budget.MenuTokens, Expansions: cfg.Budget.ExpansionTokens}
}

type skillNotices struct {
	lang     i18n.Language
	messages *bridgeMessages
}

func newSkillNotices(lang i18n.Language) skillNotices {
	return skillNotices{lang: lang, messages: newBridgeMessages()}
}

func (n skillNotices) MenuMore(hidden int) string {
	return n.messages.render(n.lang, "skills.menu.more", map[string]any{"Count": hidden})
}

func (n skillNotices) ExpansionCut(name, path string) string {
	return n.messages.render(n.lang, "skills.expansion.cut", map[string]any{"Name": name, "Path": path})
}
