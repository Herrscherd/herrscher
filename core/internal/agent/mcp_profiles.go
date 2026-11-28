package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const mcpProfilesFile = "mcp-profiles.json"

const mcpServersKey = "mcpServers"

// RoleWorker is the delegation role of a session a lead spawned. It is the only role known when a session is provisioned: a lead is recognised later, once its workers report.
const RoleWorker = "worker"

func assignableRoles() []string {
	return []string{RoleWorker}
}

// AssignableRoles lists the roles the provisioning assigns. They are the only keys an mcp-profiles.json may hold.
func AssignableRoles() []string {
	return assignableRoles()
}

// DelegationRole is the role of a session given the name of the session that spawned it: RoleWorker for a session a lead spawned, empty for a top-level session.
func DelegationRole(parent string) string {
	if parent == "" {
		return ""
	}
	return RoleWorker
}

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

// UnassignableRoleError reports a role the provisioning never assigns. InProfile tells whether the role is a key of mcp-profiles.json, the file being wrong, or the role asked for by a caller such as mcp weight.
type UnassignableRoleError struct {
	Role      string
	InProfile bool
}

func (e *UnassignableRoleError) Error() string {
	if e.InProfile {
		return fmt.Sprintf("%s: role %q is not assignable, accepted roles: %s", mcpProfilesFile, e.Role, strings.Join(assignableRoles(), ", "))
	}
	return fmt.Sprintf("role %q is not assignable, accepted roles: %s", e.Role, strings.Join(assignableRoles(), ", "))
}

// NullProfileEntryError reports a role whose entry in mcp-profiles.json is null instead of a list of server names.
type NullProfileEntryError struct {
	Role string
}

func (e *NullProfileEntryError) Error() string {
	return fmt.Sprintf("%s: the entry of role %q is null, want a list of server names", mcpProfilesFile, e.Role)
}

type rawMCPServers struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// SelectMCP returns the servers of the agent's mcp.json that a session of the given role keeps. Without a profile for the role, or with an empty role, every server is kept and the document is the mcp.json unchanged. The role must be assignable, and the mcp-profiles.json of the agent, when it exists, must be valid for every role.
func (a Agent) SelectMCP(role string) (MCPSelection, error) {
	if role != "" && !slices.Contains(assignableRoles(), role) {
		return MCPSelection{}, &UnassignableRoleError{Role: role}
	}
	profiles, err := a.readMCPProfiles()
	if err != nil {
		return MCPSelection{}, err
	}
	raw, err := os.ReadFile(filepath.Join(a.Home, mcpFile))
	if err != nil {
		return MCPSelection{}, fmt.Errorf("read %s: %w", mcpFile, err)
	}
	keep, applied := profiles[role]
	if role == "" || !applied {
		return MCPSelection{Role: role, Document: raw}, nil
	}
	return filterMCP(raw, role, keep)
}

func (a Agent) readMCPProfiles() (map[string][]string, error) {
	raw, err := os.ReadFile(filepath.Join(a.Home, mcpProfilesFile))
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string][]string), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", mcpProfilesFile, err)
	}
	var profiles map[string][]string
	if err := json.Unmarshal(raw, &profiles); err != nil {
		return nil, fmt.Errorf("parse %s: %w", mcpProfilesFile, err)
	}
	for _, role := range slices.Sorted(maps.Keys(profiles)) {
		if !slices.Contains(assignableRoles(), role) {
			return nil, &UnassignableRoleError{Role: role, InProfile: true}
		}
		if profiles[role] == nil {
			return nil, &NullProfileEntryError{Role: role}
		}
	}
	return profiles, nil
}

func filterMCP(raw []byte, role string, keep []string) (MCPSelection, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return MCPSelection{}, fmt.Errorf("parse mcp.json: %w", err)
	}
	if document == nil {
		document = make(map[string]json.RawMessage)
	}
	var declared rawMCPServers
	if err := json.Unmarshal(raw, &declared); err != nil {
		return MCPSelection{}, fmt.Errorf("parse mcp.json: %w", err)
	}
	for _, name := range keep {
		if _, ok := declared.MCPServers[name]; !ok {
			return MCPSelection{}, &UnknownProfileServerError{Role: role, Server: name}
		}
	}
	selection := MCPSelection{Role: role, Applied: true}
	kept := make(map[string]json.RawMessage, len(keep))
	for _, name := range slices.Sorted(maps.Keys(declared.MCPServers)) {
		if slices.Contains(keep, name) {
			kept[name] = declared.MCPServers[name]
			selection.Kept = append(selection.Kept, name)
			continue
		}
		selection.Dropped = append(selection.Dropped, name)
	}
	servers, err := encodeJSON(kept)
	if err != nil {
		return MCPSelection{}, fmt.Errorf("render mcp.json for role %q: %w", role, err)
	}
	document[mcpServersKey] = servers
	selection.Document, err = encodeJSON(document)
	if err != nil {
		return MCPSelection{}, fmt.Errorf("render mcp.json for role %q: %w", role, err)
	}
	if _, err := ParseMCPServers(selection.Document); err != nil {
		return MCPSelection{}, err
	}
	return selection, nil
}

func encodeJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
