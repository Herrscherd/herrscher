package bridge

import (
	"errors"
	"fmt"
	"os"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/skills"
)

type skillMenuFloorError struct {
	menu  int
	floor int
}

func (e *skillMenuFloorError) Error() string {
	return fmt.Sprintf("skills menu budget of %d tokens is below the %d the menu needs", e.menu, e.floor)
}

func AgentSkillBudget(agentsRoot, name string) (skills.Budget, error) {
	if name == "" {
		return skills.Budget{}, nil
	}
	stored, err := agent.NewStore(agentsRoot).SkillsBudget(name)
	if err != nil {
		return skills.Budget{}, err
	}
	return skills.Budget{Menu: stored.MenuTokens, Expansions: stored.ExpansionTokens}, nil
}

func LocalizeSkillBudgetError(err error, lang i18n.Language) error {
	if err == nil {
		return nil
	}
	messages := newBridgeMessages()
	if broken, ok := errors.AsType[*agent.SkillsBudgetError](err); ok {
		text := messages.render(lang, "skills.budget.malformed", map[string]any{"Agent": broken.Name, "Detail": broken.Err.Error()})
		return fmt.Errorf("%s: %w", text, err)
	}
	if floor, ok := errors.AsType[*skillMenuFloorError](err); ok {
		text := messages.render(lang, "skills.budget.floor", map[string]any{"Menu": floor.menu, "Floor": floor.floor})
		return fmt.Errorf("%s: %w", text, err)
	}
	return err
}

func newSkillEngine(backend contracts.Backend, budget skills.Budget, messages *bridgeMessages) (*skills.Engine, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return newSkillEngineIn(backend, cwd, budget, messages)
}

func newSkillEngineIn(backend contracts.Backend, cwd string, budget skills.Budget, messages *bridgeMessages) (*skills.Engine, error) {
	if n, ok := skillNativeOf(backend); ok && n.NativeSkills() {
		return nil, nil
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		logger.Warn("config unreadable; skills disabled", "err", err)
		return nil, nil
	}
	var extra []string
	if cfg.Skills != nil {
		if cfg.Skills.Enabled != nil && !*cfg.Skills.Enabled {
			return nil, nil
		}
		extra = cfg.Skills.Roots
	}
	eng := skills.NewEngine(skills.Roots(cwd, extra))
	eng.SetBudget(budget)
	if err := checkMenuFloor(eng, budget, messages); err != nil {
		return nil, err
	}
	return eng, nil
}

func checkMenuFloor(eng *skills.Engine, budget skills.Budget, messages *bridgeMessages) error {
	if budget.Menu <= 0 {
		return nil
	}
	floor := max(
		eng.MenuFloor(newSkillNotices(i18n.English, messages)),
		eng.MenuFloor(newSkillNotices(i18n.French, messages)),
	)
	if budget.Menu < floor {
		return &skillMenuFloorError{menu: budget.Menu, floor: floor}
	}
	return nil
}

type skillNotices struct {
	lang     i18n.Language
	messages *bridgeMessages
}

func newSkillNotices(lang i18n.Language, messages *bridgeMessages) skillNotices {
	return skillNotices{lang: lang, messages: messages}
}

func (n skillNotices) MenuMore(hidden int, dirs []string) string {
	return n.messages.render(n.lang, "skills.menu.more", map[string]any{"Count": hidden, "Dirs": strings.Join(dirs, ", ")})
}

func (n skillNotices) ExpansionCut(name, path string) string {
	return n.messages.render(n.lang, "skills.expansion.cut", map[string]any{"Name": name, "Path": path})
}
