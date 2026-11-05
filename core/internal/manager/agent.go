// Package manager runs the lifecycle of the sessions a chat channel hosts: creation,
// switching, budgets, renames and the commands that inspect them.
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

func (h *Handler) agentCreateRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	raw, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, ErrMissingName
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		return contracts.Result{}, &InvalidValueError{Field: nameParam, Value: raw, Hint: "use letters, digits, - or _ (max 64)"}
	}
	soul, _ := input.Lookup("soul")
	mcp, _ := input.Lookup("mcp")
	backend, _ := input.Lookup("backend")
	cmd, _ := input.Lookup("cmd")
	host, _ := input.Lookup("host")
	tagsRaw, _ := input.Lookup("tags")
	tags := strings.Fields(strings.ReplaceAll(tagsRaw, ",", " "))
	created, err := h.agents.Create(agent.CreateSpec{Name: name, Soul: soul, MCP: mcp, Backend: backend, Cmd: cmd, Host: strings.TrimSpace(host), Tags: tags})
	if err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(fmt.Sprintf("🤖 Agent **%s** created at %#q.", created.Name, created.Home)), nil
}

func (h *Handler) agentListRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	agents, err := h.agents.List()
	if err != nil {
		return contracts.Result{}, err
	}
	if input.JSON {
		type row struct {
			Name    string   `json:"name"`
			Home    string   `json:"home"`
			Tags    []string `json:"tags"`
			Backend string   `json:"backend"`
		}
		rows := make([]row, 0, len(agents))
		for i := range agents {
			listed := &agents[i]
			tags := listed.Tags
			if tags == nil {
				tags = []string{}
			}
			rows = append(rows, row{Name: listed.Name, Home: listed.Home, Tags: tags, Backend: listed.Backend})
		}
		buf, err := json.Marshal(rows)
		if err != nil {
			return contracts.Result{}, err
		}
		return contracts.DataResult(string(buf)), nil
	}
	if len(agents) == 0 {
		return contracts.DataResult("No agents. Create one with `agent create <name>`."), nil
	}
	var out strings.Builder
	out.WriteString("Agents:\n")
	for i := range agents {
		a := &agents[i]
		fmt.Fprintf(&out, "• **%s** (%#q)\n", a.Name, a.Home)
	}
	return contracts.DataResult(out.String()), nil
}

func (h *Handler) agentShowRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, ErrMissingName
	}
	candidate, ok := h.agents.Get(slugify(name))
	if !ok {
		return contracts.Result{}, &agent.NotFoundError{Name: name}
	}
	soul, err := os.ReadFile(filepath.Join(candidate.Home, "SOUL.md"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return contracts.Result{}, fmt.Errorf("read soul: %w", err)
	}
	if input.JSON {
		tags := candidate.Tags
		if tags == nil {
			tags = []string{}
		}
		buf, err := json.Marshal(struct {
			Name    string   `json:"name"`
			Tags    []string `json:"tags"`
			Backend string   `json:"backend"`
			Soul    string   `json:"soul"`
		}{candidate.Name, tags, candidate.Backend, string(soul)})
		if err != nil {
			return contracts.Result{}, err
		}
		return contracts.DataResult(string(buf)), nil
	}
	return contracts.DataResult(fmt.Sprintf("**%s** (%#q)\n\n%s", candidate.Name, candidate.Home, string(soul))), nil
}

func (h *Handler) agentSetSoulRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, ErrMissingName
	}
	soul, ok := input.Lookup("soul")
	if !ok {
		return contracts.Result{}, ErrMissingSoul
	}
	if err := h.agents.SetSoul(slugify(name), soul); err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(fmt.Sprintf("🖊 Soul updated for **%s** (applies to new sessions).", slugify(name))), nil
}
