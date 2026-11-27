package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

const mcpProfilesFile = "mcp-profiles.json"

// RoleWorker is the delegation role of a session a lead spawned. It is the only role known when a session is provisioned: a lead is recognised later, once its workers report.
const RoleWorker = "worker"

// MCPSelection is the set of MCP servers a session keeps for its role. Document is the mcp.json the session receives, Kept and Dropped name the servers of the agent's mcp.json on each side of the profile, sorted by name. Applied is false when no profile matches the role, and then every server is kept.
type MCPSelection struct {
	Role     string
	Applied  bool
	Kept     []string
	Dropped  []string
	Document []byte
}

// UnknownProfileServerError reports a profile that names a server the agent's mcp.json does not declare.
type UnknownProfileServerError struct {
	Role   string
	Server string
}

func (e *UnknownProfileServerError) Error() string {
	return fmt.Sprintf("%s: role %q names MCP server %q, which mcp.json does not declare", mcpProfilesFile, e.Role, e.Server)
}

type rawMCPDocument struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// SelectMCP returns the servers of the agent's mcp.json that a session of the given role keeps. Without a mcp-profiles.json in the agent home, without a profile for the role, or with an empty role, every server is kept and the document is the mcp.json unchanged.
func (a Agent) SelectMCP(role string) (MCPSelection, error) {
	raw, err := os.ReadFile(filepath.Join(a.Home, mcpFile))
	if err != nil {
		return MCPSelection{}, fmt.Errorf("read %s: %w", mcpFile, err)
	}
	keep, applied, err := a.readMCPProfile(role)
	if err != nil {
		return MCPSelection{}, err
	}
	if !applied {
		return MCPSelection{Role: role, Document: raw}, nil
	}
	return filterMCP(raw, role, keep)
}

func (a Agent) readMCPProfile(role string) ([]string, bool, error) {
	if role == "" {
		return nil, false, nil
	}
	raw, err := os.ReadFile(filepath.Join(a.Home, mcpProfilesFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", mcpProfilesFile, err)
	}
	var profiles map[string][]string
	if err := json.Unmarshal(raw, &profiles); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", mcpProfilesFile, err)
	}
	keep, applied := profiles[role]
	return keep, applied, nil
}

func filterMCP(raw []byte, role string, keep []string) (MCPSelection, error) {
	servers, err := ParseMCPServers(raw)
	if err != nil {
		return MCPSelection{}, err
	}
	declared := make(map[string]bool, len(servers))
	for _, server := range servers {
		declared[server.Name] = true
	}
	for _, name := range keep {
		if !declared[name] {
			return MCPSelection{}, &UnknownProfileServerError{Role: role, Server: name}
		}
	}
	var document rawMCPDocument
	if err = json.Unmarshal(raw, &document); err != nil {
		return MCPSelection{}, fmt.Errorf("parse mcp.json: %w", err)
	}
	selection := MCPSelection{Role: role, Applied: true}
	kept := make(map[string]json.RawMessage, len(keep))
	for _, server := range servers {
		if slices.Contains(keep, server.Name) {
			kept[server.Name] = document.MCPServers[server.Name]
			selection.Kept = append(selection.Kept, server.Name)
			continue
		}
		selection.Dropped = append(selection.Dropped, server.Name)
	}
	selection.Document, err = json.MarshalIndent(rawMCPDocument{MCPServers: kept}, "", "  ")
	if err != nil {
		return MCPSelection{}, fmt.Errorf("render mcp.json for role %q: %w", role, err)
	}
	return selection, nil
}
