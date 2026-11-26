// Package mcpweight measures how many tokens an agent's MCP tool definitions cost, by starting each stdio server of its mcp.json and asking it for its tools.
package mcpweight

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// ReadServers lists the servers of an mcp.json file sorted by name, parsed and checked by the agent package. A missing file has no servers and is not an error.
func ReadServers(path string) ([]agent.MCPServer, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read MCP config: %w", err)
	}
	return agent.ParseMCPServers(raw)
}
