package host

import (
	"context"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/manager"
)

// EnvSelfTools switches off the herrscher MCP server a session gets in its worktree when set to a false spelling.
const EnvSelfTools = "HERRSCHER_SELF_TOOLS"

func selfToolsProvisioner(getenv func(string) string) manager.SelfToolsProvisioner {
	switch strings.ToLower(strings.TrimSpace(getenv(EnvSelfTools))) {
	case "0", "false", "off", "no":
		return nil
	}
	return func(ctx context.Context, worktree string) error {
		return agent.MaterializeSelfTools(ctx, worktree, agent.SelfBin())
	}
}

// SelfToolsCallWait is how long a self tool waits for the daemon: long enough for the operator to answer the question a refused verb becomes.
const SelfToolsCallWait = agent.HookWait + time.Minute
