package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type codexMCPServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

type codexMCPConfig struct {
	MCPServers map[string]codexMCPServer `json:"mcpServers"`
}

// renderCodexMCP converts the durable Claude-format mcp.json into Codex's
// project-local config.toml. Only stdio MCP servers are supported.
func renderCodexMCP(raw []byte, worktree string) ([]byte, error) {
	var config codexMCPConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse mcp.json: %w", err)
	}

	names := make([]string, 0, len(config.MCPServers))
	for name := range config.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)

	var out strings.Builder
	for i, name := range names {
		server := config.MCPServers[name]
		if server.Type != "stdio" {
			return nil, fmt.Errorf("unsupported MCP transport %q for server %q", server.Type, name)
		}
		if strings.TrimSpace(server.Command) == "" {
			return nil, fmt.Errorf("missing command for MCP server %q", name)
		}
		if i > 0 {
			out.WriteByte('\n')
		}

		out.WriteString("[mcp_servers.")
		out.WriteString(tomlKey(name))
		out.WriteString("]\ncommand = ")
		out.WriteString(tomlString(strings.ReplaceAll(server.Command, worktreeToken, worktree)))
		out.WriteByte('\n')
		if len(server.Args) > 0 {
			out.WriteString("args = [")
			for i, arg := range server.Args {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(tomlString(strings.ReplaceAll(arg, worktreeToken, worktree)))
			}
			out.WriteString("]\n")
		}
		if len(server.Env) == 0 {
			continue
		}

		out.WriteByte('\n')
		out.WriteString("[mcp_servers.")
		out.WriteString(tomlKey(name))
		out.WriteString(".env]\n")
		envKeys := make([]string, 0, len(server.Env))
		for key := range server.Env {
			envKeys = append(envKeys, key)
		}
		sort.Strings(envKeys)
		for _, key := range envKeys {
			out.WriteString(tomlKey(key))
			out.WriteString(" = ")
			out.WriteString(tomlString(strings.ReplaceAll(server.Env[key], worktreeToken, worktree)))
			out.WriteByte('\n')
		}
	}
	return []byte(out.String()), nil
}

func tomlKey(key string) string {
	if key != "" {
		for _, r := range key {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
				return tomlString(key)
			}
		}
		return key
	}
	return tomlString(key)
}

func tomlString(value string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			out.WriteString(`\\`)
		case '"':
			out.WriteString(`\"`)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&out, `\u%04X`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return out.String()
}
