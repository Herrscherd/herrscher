package manager

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// agentCreateRun creates a durable companion agent: a home directory seeded with
// SOUL.md (persona), mcp.json (an optional stdio MCP server) and settings.json
// (zero-prompt). The agent is later materialized into a session worktree.
func (h *Handler) agentCreateRun(_ context.Context, in contracts.Input) (string, error) {
	raw, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid name %q — use letters, digits, - or _ (max 64)", raw)
	}
	soul, _ := in.Lookup("soul")
	mcp, _ := in.Lookup("mcp")
	a, err := h.agents.Create(agent.CreateSpec{Name: name, Soul: soul, MCP: mcp})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("🤖 Agent **%s** created at `%s`.", a.Name, a.Home), nil
}

// agentListRun lists the durable companion agents.
func (h *Handler) agentListRun(_ context.Context, _ contracts.Input) (string, error) {
	agents, err := h.agents.List()
	if err != nil {
		return "", err
	}
	if len(agents) == 0 {
		return "No agents. Create one with `agent create <name>`.", nil
	}
	out := "Agents:\n"
	for _, a := range agents {
		out += fmt.Sprintf("• **%s** (`%s`)\n", a.Name, a.Home)
	}
	return out, nil
}
