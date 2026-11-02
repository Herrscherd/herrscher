package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func (h *Handler) agentCreateRun(_ context.Context, in contracts.Input) (string, error) {
	raw, ok := in.Lookup("name")
	if !ok {
		return "", ErrMissingName
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		return "", &InvalidValueError{Field: "name", Value: raw, Hint: "use letters, digits, - or _ (max 64)"}
	}
	soul, _ := in.Lookup("soul")
	mcp, _ := in.Lookup("mcp")
	backend, _ := in.Lookup("backend")
	cmd, _ := in.Lookup("cmd")
	host, _ := in.Lookup("host")
	tagsRaw, _ := in.Lookup("tags")
	tags := strings.Fields(strings.ReplaceAll(tagsRaw, ",", " "))
	a, err := h.agents.Create(agent.CreateSpec{Name: name, Soul: soul, MCP: mcp, Backend: backend, Cmd: cmd, Host: strings.TrimSpace(host), Tags: tags})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("🤖 Agent **%s** created at `%s`.", a.Name, a.Home), nil
}

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

func (h *Handler) agentShowRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", ErrMissingName
	}
	a, ok := h.agents.Get(slugify(name))
	if !ok {
		return "", &agent.NotFoundError{Name: name}
	}
	soul, err := os.ReadFile(filepath.Join(a.Home, "SOUL.md"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read soul: %w", err)
	}
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

func (h *Handler) agentSetSoulRun(_ context.Context, in contracts.Input) (string, error) {
	name, ok := in.Lookup("name")
	if !ok {
		return "", ErrMissingName
	}
	soul, ok := in.Lookup("soul")
	if !ok {
		return "", ErrMissingSoul
	}
	if err := h.agents.SetSoul(slugify(name), soul); err != nil {
		return "", err
	}
	return fmt.Sprintf("🖊 Soul updated for **%s** (applies to new sessions).", slugify(name)), nil
}
