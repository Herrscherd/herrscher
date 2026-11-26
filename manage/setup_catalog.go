package manage

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/language"
)

type setupModule int

const (
	moduleTerminal setupModule = iota
	moduleDiscord
	moduleTelegram
	moduleClaude
	moduleCodex
	moduleCursor
	moduleObsidian
	moduleBasic
	moduleLLM
	moduleSuperset
	moduleJev
)

const (
	kindTerminal = "terminal"
	kindClaude   = "claude"
	kindCodex    = "codex"
	kindCursor   = "cursor"
	kindObsidian = "obsidian"
	kindBasic    = "basic"
	kindLLM      = "llm"
	kindSuperset = "superset"
	kindJev      = "jev"
	verbLogin    = "login"
)

const (
	envDiscordToken = "DISCORD_BOT_TOKEN"
	envDiscordUser  = "DISCORD_USER_ID"
	envTelegramBot  = "TELEGRAM_BOT_TOKEN"
	envTelegramUser = "TELEGRAM_USER_ID"
	envTelegramMany = "TELEGRAM_ALLOWED_USERS"
	envTelegramAll  = "TELEGRAM_ALLOW_ALL_USERS"
	envCursorKey    = "CURSOR_API_KEY"
	envVault        = "OBSIDIAN_VAULT"
	envAnthropicKey = contracts.EnvAnthropicAPIKey
	envLanguage     = language.EnvVar
	envPolicy       = host.EnvRoutePolicy
	envModel        = host.EnvDefaultModel
	envGatewayURL   = host.EnvGatewayURL
	envGatewayToken = host.EnvGatewayToken
	fieldCodexKey   = "codex api key"
)

const (
	discordName   = "Discord"
	discordPortal = "discord.com/developers"
)

const telegramName = "Telegram"

func discordParams() clitext.Params { return clitext.Params{clitext.ParamName: discordName} }

func telegramParams() clitext.Params { return clitext.Params{clitext.ParamName: telegramName} }

type moduleSpec struct {
	params   clitext.Params
	category string
	kind     string
	about    clitext.Key
}

func moduleSpecs() map[setupModule]moduleSpec {
	return map[setupModule]moduleSpec{
		moduleTerminal: {nil, catGateway, kindTerminal, clitext.SetupModuleTerminal},
		moduleDiscord:  {discordParams(), catGateway, kindDiscord, clitext.SetupModuleChatBot},
		moduleTelegram: {nil, catGateway, kindTelegram, clitext.SetupModuleTelegram},
		moduleClaude:   {nil, catBackend, kindClaude, clitext.SetupModuleClaude},
		moduleCodex:    {nil, catBackend, kindCodex, clitext.SetupModuleCodex},
		moduleCursor:   {nil, catBackend, kindCursor, clitext.SetupModuleCursor},
		moduleObsidian: {nil, catMemory, kindObsidian, clitext.SetupModuleObsidian},
		moduleBasic:    {nil, catOrchestrator, kindBasic, clitext.SetupModuleBasic},
		moduleLLM:      {nil, catExtractor, kindLLM, clitext.SetupModuleLLM},
		moduleSuperset: {nil, catSkills, kindSuperset, clitext.SetupModuleSuperset},
		moduleJev:      {nil, catCompactor, kindJev, clitext.SetupModuleJev},
	}
}

func (mod setupModule) spec() moduleSpec { return moduleSpecs()[mod] }

func (mod setupModule) path() string {
	if mod == moduleTerminal {
		return inTree()[0]
	}
	spec := mod.spec()
	return catalog()[spec.category][spec.kind]
}

type moduleGroup struct {
	title   clitext.Key
	members []setupModule
	multi   bool
}

func moduleGroups() []moduleGroup {
	return []moduleGroup{
		{clitext.SetupCategoryGateway, []setupModule{moduleTerminal, moduleDiscord, moduleTelegram}, true},
		{clitext.SetupCategoryBackend, []setupModule{moduleClaude, moduleCodex, moduleCursor}, true},
		{clitext.SetupCategoryMemory, []setupModule{moduleObsidian}, false},
		{clitext.SetupCategoryOrchestrator, []setupModule{moduleBasic}, false},
		{clitext.SetupCategoryExtractor, []setupModule{moduleLLM}, false},
		{clitext.SetupCategorySkills, []setupModule{moduleSuperset}, true},
		{clitext.SetupCategoryCompactor, []setupModule{moduleJev}, false},
	}
}

type moduleSet map[setupModule]bool

func defaultModules() moduleSet {
	set := make(moduleSet)
	for _, mod := range []setupModule{moduleTerminal, moduleDiscord, moduleClaude, moduleObsidian, moduleBasic, moduleLLM, moduleSuperset, moduleJev} {
		set[mod] = true
	}
	return set
}

func modulesFromPaths(paths []string) (moduleSet, []string) {
	byPath := make(map[string]setupModule)
	for _, group := range moduleGroups() {
		for _, mod := range group.members {
			byPath[mod.path()] = mod
		}
	}
	set := make(moduleSet)
	set[moduleTerminal] = true
	var extras []string
	for _, path := range paths {
		if mod, known := byPath[path]; known {
			set[mod] = true
			continue
		}
		extras = append(extras, path)
	}
	return set, extras
}

func (set moduleSet) toggle(group moduleGroup, mod setupModule) {
	if mod == moduleTerminal {
		return
	}
	if group.multi {
		set[mod] = !set[mod]
		return
	}
	set.unset(group)
	set[mod] = true
}

func (set moduleSet) unset(group moduleGroup) {
	for _, member := range group.members {
		delete(set, member)
	}
}

func (set moduleSet) count() int {
	total := 0
	for _, on := range set {
		if on {
			total++
		}
	}
	return total
}

func (set moduleSet) paths(extras []string) []string {
	var paths []string
	for _, group := range moduleGroups() {
		for _, mod := range group.members {
			if set[mod] {
				paths = append(paths, mod.path())
			}
		}
	}
	paths = append(paths, extras...)
	slices.Sort(paths)
	return slices.Compact(paths)
}

func (set moduleSet) backendKinds() map[string]bool {
	kinds := make(map[string]bool)
	for _, mod := range []setupModule{moduleClaude, moduleCodex, moduleCursor} {
		if set[mod] {
			kinds[mod.spec().kind] = true
		}
	}
	return kinds
}

type settingsDraft struct {
	initial map[string]string
	edits   map[string]string
}

func newDraft(initial map[string]string) settingsDraft {
	return settingsDraft{initial: initial, edits: make(map[string]string)}
}

func (draft settingsDraft) value(key string) string {
	if edited, ok := draft.edits[key]; ok {
		return edited
	}
	return draft.initial[key]
}

func (draft settingsDraft) set(key, value string) {
	if value == draft.initial[key] {
		delete(draft.edits, key)
		return
	}
	draft.edits[key] = value
}

func (draft settingsDraft) written() map[string]string {
	out := make(map[string]string, len(draft.edits))
	for key, value := range draft.edits {
		switch key {
		case fieldCodexKey:
		case envVault:
			out[key] = expandHome(value)
		default:
			out[key] = value
		}
	}
	return out
}

func setupEnvKeys() []string {
	return []string{envDiscordToken, envDiscordUser, envTelegramBot, envTelegramUser, envTelegramMany, envTelegramAll, envAnthropicKey, envCursorKey, envVault, envPolicy, envModel, envGatewayURL, envGatewayToken, envLanguage}
}

func secretKeys() map[string]bool {
	return map[string]bool{envDiscordToken: true, envTelegramBot: true, envAnthropicKey: true, envCursorKey: true, envGatewayToken: true, fieldCodexKey: true}
}

func readSettings(getenv func(string) string) map[string]string {
	values := make(map[string]string)
	for _, key := range setupEnvKeys() {
		if value := getenv(key); value != "" {
			values[key] = value
		}
	}
	return values
}

func expandHome(path string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(path), "~")
	if !ok || (rest != "" && rest[0] != '/') {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, rest)
}
