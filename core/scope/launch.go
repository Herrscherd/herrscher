package scope

import (
	"context"
	"os"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
)

// Setting keys of the launch scope. They are declared here rather than in the
// terminal plugin because two callers need the same answer: the plugin, which
// opens the default window, and the binary, which creates the session for
// `herrscher "<task>"` before any plugin is instantiated. A session opened one
// way has to learn exactly as much as one opened the other.
const (
	SetLearn      = "learn"
	SetExtractor  = "extractor"
	SetEvery      = "consolidate-every"
	SetMemAgent   = "memory-agent"
	SetProject    = "project"
	SetProjectPin = "project-pin"

	// PinAtLaunch is the project-pin value that takes the directory's answer as
	// final. Anything else means the session's first prompt may revise it.
	PinAtLaunch = "launch"

	defaultEvery = 10
)

// LaunchSettings declares what a launch can be told. Every one is optional and
// carries a default, so a build with no environment learns, under the project it
// is standing in, as itself.
func LaunchSettings() []contracts.Setting {
	return []contracts.Setting{
		{
			Key: SetLearn, Env: "TERMINAL_LEARN", Default: "true",
			Help: clitext.SettingTerminalLearn,
		},
		{
			Key: SetExtractor, Env: "TERMINAL_EXTRACTOR", Default: "llm",
			Help: clitext.SettingTerminalExtractor,
		},
		{
			Key: SetEvery, Env: "TERMINAL_CONSOLIDATE_EVERY", Default: strconv.Itoa(defaultEvery),
			Help: clitext.SettingTerminalConsolidate,
		},
		{
			Key: SetMemAgent, Env: "TERMINAL_MEMORY_AGENT", Default: "tui",
			Help: clitext.SettingTerminalMemoryAgent,
		},
		{
			Key: SetProject, Env: "TERMINAL_PROJECT", Default: "",
			Help: clitext.SettingTerminalProject,
		},
		{
			Key: SetProjectPin, Env: "TERMINAL_PROJECT_PIN", Default: "first-turn",
			Help: clitext.SettingTerminalProjectPin,
		},
	}
}

// Launch is what a launch decided about the session it is about to open: where
// what it learns is filed, and whether that decision is final. A zero Launch is
// a session that records nothing, which is what TERMINAL_LEARN=false asks for.
type Launch struct {
	Project   string
	Agent     string
	Extractor string
	Every     int
	Pinned    bool
}

// LaunchFrom reads the decision out of a resolved plugin configuration.
func LaunchFrom(ctx context.Context, cfg contracts.PluginConfig) Launch {
	if !boolSetting(cfg, SetLearn, true) {
		return Launch{}
	}
	launch := Launch{
		Agent:     cfg.Get(SetMemAgent),
		Extractor: cfg.Get(SetExtractor),
		Every:     intSetting(cfg, SetEvery, defaultEvery),
	}
	if p := Name(cfg.Get(SetProject)); p != "" {
		launch.Project, launch.Pinned = p, true
		return launch
	}
	launch.Project = ProjectFromDir(ctx, cwd())
	launch.Pinned = launch.Project != "" && cfg.Get(SetProjectPin) == PinAtLaunch
	return launch
}

// LaunchFromEnv is LaunchFrom against the process environment, for the callers
// that run before any plugin is instantiated. A configuration that will not
// resolve leaves the session unscoped rather than failing the launch: not
// learning is a loss, not opening at all is a broken tool.
func LaunchFromEnv(ctx context.Context) Launch {
	cfg, err := contracts.Resolve(LaunchSettings(), envx.Getenv)
	if err != nil {
		return Launch{}
	}
	return LaunchFrom(ctx, cfg)
}

// Apply fills in the memory roots on a session spec, and nothing else: none of
// these fields places the session.
func (l Launch) Apply(spec *contracts.CreateSession) {
	spec.MemoryProject = l.Project
	spec.MemoryAgent = l.Agent
	spec.ProjectPinned = l.Pinned
	spec.Extractor = l.Extractor
	spec.ConsolidateEvery = l.Every
}

// Args is Apply for the callers that reach session create as a verb rather than
// as a struct. Empty when there is nothing to say, so a launch that learns
// nothing dispatches exactly the argv it did before this existed.
func (l Launch) Args() []string {
	var args []string
	if l.Extractor == "" {
		return nil
	}
	if l.Project != "" {
		args = append(args, "--memory_project", l.Project)
	}
	if l.Agent != "" {
		args = append(args, "--memory_agent", l.Agent)
	}
	if l.Pinned {
		args = append(args, "--project_pinned")
	}
	args = append(args, "--extractor", l.Extractor)
	if l.Every > 0 {
		args = append(args, "--consolidate_every", strconv.Itoa(l.Every))
	}
	return args
}

func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

func boolSetting(cfg contracts.PluginConfig, key string, def bool) bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(cfg.Get(key)))
	if err != nil {
		return def
	}
	return enabled
}

func intSetting(cfg contracts.PluginConfig, key string, def int) int {
	count, err := strconv.Atoi(strings.TrimSpace(cfg.Get(key)))
	if err != nil || count < 0 {
		return def
	}
	return count
}
