package host

import (
	"strings"

	"github.com/Herrscherd/herrscher/core/config"
)

// EnvContextDelta sends each host block only when it changed since the previous turn, for a session whose backend keeps its history. Unset, the configuration decides; a false spelling turns it off.
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
