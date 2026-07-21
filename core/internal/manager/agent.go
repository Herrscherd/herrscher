package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	backend, _ := in.Lookup("backend")
	cmd, _ := in.Lookup("cmd")
	tagsRaw, _ := in.Lookup("tags")
	var tags []string
	for _, tok := range strings.Fields(strings.ReplaceAll(tagsRaw, ",", " ")) {
		tags = append(tags, tok)
	}
	a, err := h.agents.Create(agent.CreateSpec{Name: name, Soul: soul, MCP: mcp, Backend: backend, Cmd: cmd, Tags: tags})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("🤖 Agent **%s** created at `%s`.", a.Name, a.Home), nil
}

// agentListRun lists the durable companion agents. With --json it emits a
// structured array (name, home, tags, backend) that Neublox reads as its role
// catalog; otherwise a human Markdown list.
func (h *Handler) agentListRun(_ context.Context, in contracts.Input) (string, error) {
	agents, err := h.agents.List()
	if err != nil {
		return "", err
	}
	if in.JSON {
		type row struct {
			Name    string   `json:"name"`
			Home    string   `json:"home"`
			Tags    []string `json:"tags"`
			Backend string   `json:"backend"`
		}
		rows := make([]row, 0, len(agents))
		for _, a := range agents {
			tags := a.Tags
			if tags == nil {
				tags = []string{}
			}
			rows = append(rows, row{Name: a.Name, Home: a.Home, Tags: tags, Backend: a.Backend})
		}
		buf, err := json.Marshal(rows)
		if err != nil {
			return "", err
		}
		return string(buf), nil
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

// agentShowRun returns one agent's catalog record. With --json it emits
// {name, tags, backend, soul}; the soul is read from <home>/SOUL.md.
func (h *Handler) agentShowRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	a, ok := h.agents.Get(slugify(name))
	if !ok {
		return "", fmt.Errorf("no agent %q", name)
	}
	soul, _ := os.ReadFile(filepath.Join(a.Home, "SOUL.md"))
	if in.JSON {
		tags := a.Tags
		if tags == nil {
			tags = []string{}
		}
		buf, err := json.Marshal(struct {
			Name    string   `json:"name"`
			Tags    []string `json:"tags"`
			Backend string   `json:"backend"`
			Soul    string   `json:"soul"`
		}{a.Name, tags, a.Backend, string(soul)})
		if err != nil {
			return "", err
		}
		return string(buf), nil
	}
	return fmt.Sprintf("**%s** (`%s`)\n\n%s", a.Name, a.Home, string(soul)), nil
}

// agentSetSoulRun rewrites an agent's SOUL.md. The edit applies to the next
// session materialized from that agent (homes are the source of truth).
func (h *Handler) agentSetSoulRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", fmt.Errorf("missing name")
	}
	soul, ok := in.Lookup("soul")
	if !ok {
		return "", fmt.Errorf("missing soul")
	}
	if err := h.agents.SetSoul(slugify(name), soul); err != nil {
		return "", err
	}
	return fmt.Sprintf("🖊 Soul updated for **%s** (applies to new sessions).", slugify(name)), nil
}
