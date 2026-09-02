package scope

import (
	"os"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"

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
		{Key: SetLearn, Env: "TERMINAL_LEARN", Default: "true",
			Help: "the session a launch opens learns: false restores a session that records nothing"},
		{Key: SetExtractor, Env: "TERMINAL_EXTRACTOR", Default: "llm",
			Help: "registered curation extractor that distils the transcript"},
		{Key: SetEvery, Env: "TERMINAL_CONSOLIDATE_EVERY", Default: strconv.Itoa(defaultEvery),
			Help: "turns between consolidation passes (0 = manual/idle only)"},
		{Key: SetMemAgent, Env: "TERMINAL_MEMORY_AGENT", Default: "tui",
			Help: "memory root for what the window learns as itself"},
		{Key: SetProject, Env: "TERMINAL_PROJECT", Default: "",
			Help: "force the memory project and pin it, instead of resolving one"},
		{Key: SetProjectPin, Env: "TERMINAL_PROJECT_PIN", Default: "first-turn",
			Help: "when the project is settled: first-turn (the prompt may revise it) | launch (the directory is final)"},
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
func LaunchFrom(cfg contracts.PluginConfig) Launch {
	if !boolSetting(cfg, SetLearn, true) {
		return Launch{}
	}
	l := Launch{
		Agent:     cfg.Get(SetMemAgent),
		Extractor: cfg.Get(SetExtractor),
		Every:     intSetting(cfg, SetEvery, defaultEvery),
	}
	if p := Name(cfg.Get(SetProject)); p != "" {
		// An operator who named the project is not guessing. The name is folded
		// first: session create refuses one with a space in it, and a refused
		// create is a window that opens on nothing.
		l.Project, l.Pinned = p, true
		return l
	}
	l.Project = ProjectFromDir(cwd())
	l.Pinned = l.Project != "" && cfg.Get(SetProjectPin) == PinAtLaunch
	return l
}

// LaunchFromEnv is LaunchFrom against the process environment, for the callers
// that run before any plugin is instantiated. A configuration that will not
// resolve leaves the session unscoped rather than failing the launch: not
// learning is a loss, not opening at all is a broken tool.
func LaunchFromEnv() Launch {
	cfg, err := contracts.Resolve(LaunchSettings(), envx.Getenv)
	if err != nil {
		return Launch{}
	}
	return LaunchFrom(cfg)
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
	var a []string
	if l.Extractor == "" {
		return nil
	}
	if l.Project != "" {
		a = append(a, "--memory_project", l.Project)
	}
	if l.Agent != "" {
		a = append(a, "--memory_agent", l.Agent)
	}
	if l.Pinned {
		a = append(a, "--project_pinned")
	}
	a = append(a, "--extractor", l.Extractor)
	if l.Every > 0 {
		a = append(a, "--consolidate_every", strconv.Itoa(l.Every))
	}
	return a
}

// cwd is the directory herrscher was launched in — what the operator means by
// "here". An unreadable one is not an error worth failing a launch over; it
// simply means the session starts with no project.
func cwd() string {
	d, err := os.Getwd()
	if err != nil {
		return ""
	}
	return d
}

// boolSetting reads a declared boolean setting, falling back to def for anything
// strconv does not recognise — a typo in an environment variable must not decide
// whether the window learns.
func boolSetting(cfg contracts.PluginConfig, key string, def bool) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(cfg.Get(key)))
	if err != nil {
		return def
	}
	return v
}

// intSetting reads a declared integer setting, falling back to def for anything
// unparseable or negative.
func intSetting(cfg contracts.PluginConfig, key string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(cfg.Get(key)))
	if err != nil || n < 0 {
		return def
	}
	return n
}
