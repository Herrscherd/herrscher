package agent

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// TransportStdio is the transport of an MCP server started as a local process.
const TransportStdio = "stdio"

// MCPServer is one entry of an agent's mcp.json, validated by ParseMCPServers.
type MCPServer struct {
	Name    string
	Type    string
	URL     string
	Command string
	Args    []string
	Env     map[string]string
}

type mcpDocument struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// ParseMCPServers reads the servers of an mcp.json document sorted by name. It refuses an empty server name, and a server that runs as a local process without a command.
func ParseMCPServers(raw []byte) ([]MCPServer, error) {
	var config mcpDocument
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse mcp.json: %w", err)
	}
	servers := make([]MCPServer, 0, len(config.MCPServers))
	for _, name := range slices.Sorted(maps.Keys(config.MCPServers)) {
		entry := config.MCPServers[name]
		server := MCPServer{Name: name, Type: entry.Type, URL: entry.URL, Command: entry.Command, Args: entry.Args, Env: entry.Env}
		if err := checkMCPServer(server); err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, nil
}

func checkMCPServer(server MCPServer) error {
	if strings.TrimSpace(server.Name) == "" {
		return ErrEmptyMCPServerName
	}
	runsLocally := server.Type == "" || server.Type == TransportStdio
	if runsLocally && strings.TrimSpace(server.Command) == "" {
		return fmt.Errorf("%w %q", ErrMissingMCPCommand, server.Name)
	}
	return nil
}

// Transport names how the server is reached: its declared type, else stdio for a command, else url for a URL.
func (s MCPServer) Transport() string {
	switch {
	case s.Type != "":
		return s.Type
	case s.Command != "":
		return TransportStdio
	case s.URL != "":
		return "url"
	}
	return "unknown"
}

// Stdio reports whether the server is started as a local process.
func (s MCPServer) Stdio() bool {
	return s.Transport() == TransportStdio
}
