// Package mcpweight measures how many tokens an agent's MCP tool definitions cost, by starting each stdio server of its mcp.json and asking it for its tools.
package mcpweight

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
)

// TransportStdio is the transport of a server started as a local process.
const TransportStdio = "stdio"

// ErrConfig reports an mcp.json that is not valid JSON or does not match the expected shape.
var ErrConfig = errors.New("the MCP config is malformed")

// Server is one entry of an mcp.json file.
type Server struct {
	Name    string
	Type    string
	URL     string
	Command string
	Args    []string
	Env     map[string]string
}

type fileConfig struct {
	MCPServers map[string]fileServer `json:"mcpServers"`
}

type fileServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// ReadServers lists the servers of an mcp.json file sorted by name. A missing file has no servers and is not an error.
func ReadServers(path string) ([]Server, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read MCP config: %w", err)
	}
	var config fileConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	servers := make([]Server, 0, len(config.MCPServers))
	for name, entry := range config.MCPServers {
		servers = append(servers, Server{Name: name, Type: entry.Type, URL: entry.URL, Command: entry.Command, Args: entry.Args, Env: entry.Env})
	}
	slices.SortFunc(servers, func(left, right Server) int { return cmp.Compare(left.Name, right.Name) })
	return servers, nil
}

// Transport names how the server is reached: its declared type, else stdio for a command, else url for a URL.
func (s Server) Transport() string {
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
func (s Server) Stdio() bool {
	return s.Transport() == TransportStdio
}
