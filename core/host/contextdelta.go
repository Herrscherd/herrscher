package host

import (
	"strings"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// EnvContextDelta sets the daemon's default for the context delta: a block is sent only when it changed since the previous turn, for a session whose backend keeps its history. Unset, the configuration decides; a false spelling turns it off. An agent's own setting wins over it.
const EnvContextDelta = "HERRSCHER_CONTEXT_DELTA"

// ContextDeltaOn reports whether an environment value turns the context delta on.
func ContextDeltaOn(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

func contextDeltaSetting(getenv func(string) string, configured config.Config) bool {
	if asked := strings.TrimSpace(getenv(EnvContextDelta)); asked != "" {
		return ContextDeltaOn(asked)
	}
	return configured.ContextDeltaEnabled()
}

// AgentContextDelta reports whether the context delta is on for the agent named name, whose home lies under agentsRoot. The agent's <home>/context-delta file wins over the daemon default global; an agent without the file, or with an unknown value, gets the default.
func AgentContextDelta(agentsRoot, name string, global bool) bool {
	found, ok := agent.NewStore(agentsRoot).Get(name)
	if !ok {
		return global
	}
	switch found.ContextDelta {
	case agent.ContextDeltaOn:
		return true
	case agent.ContextDeltaOff:
		return false
	case agent.ContextDeltaInherit:
	}
	return global
}
