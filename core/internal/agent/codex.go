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

func renderCodexMCP(raw []byte, worktree string) ([]byte, error) {
	var config codexMCPConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse mcp.json: %w", err)
	}

	var out strings.Builder
	for i, name := range sortedKeys(config.MCPServers) {
		server := config.MCPServers[name]
		if err := checkCodexMCPServer(name, server); err != nil {
			return nil, err
		}
		if i > 0 {
			out.WriteByte('\n')
		}
		writeCodexMCPServer(&out, name, server, worktree)
	}
	return []byte(out.String()), nil
}

func checkCodexMCPServer(name string, server codexMCPServer) error {
	if strings.TrimSpace(name) == "" {
		return ErrEmptyMCPServerName
	}
	if server.Type != "" && server.Type != "stdio" {
		return fmt.Errorf("%w %q for server %q", ErrUnsupportedMCPTransport, server.Type, name)
	}
	if strings.TrimSpace(server.Command) == "" {
		return fmt.Errorf("%w %q", ErrMissingMCPCommand, name)
	}
	return nil
}

func writeCodexMCPServer(out *strings.Builder, name string, server codexMCPServer, worktree string) {
	table := "[mcp_servers." + tomlKey(name) + "]\n"
	out.WriteString(table)
	out.WriteString("command = " + substituteWorktree(server.Command, worktree) + "\n")
	if len(server.Args) > 0 {
		out.WriteString("args = [")
		for i, arg := range server.Args {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(substituteWorktree(arg, worktree))
		}
		out.WriteString("]\n")
	}
	if len(server.Env) == 0 {
		return
	}
	out.WriteByte('\n')
	out.WriteString("[mcp_servers." + tomlKey(name) + ".env]\n")
	for _, key := range sortedKeys(server.Env) {
		out.WriteString(tomlKey(key) + " = " + substituteWorktree(server.Env[key], worktree) + "\n")
	}
}

func substituteWorktree(value, worktree string) string {
	return tomlString(strings.ReplaceAll(value, worktreeToken, worktree))
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func tomlKey(key string) string {
	if key == "" || !allBareKeyRunes(key) {
		return tomlString(key)
	}
	return key
}

func allBareKeyRunes(key string) bool {
	for _, r := range key {
		if !isBareKeyRune(r) {
			return false
		}
	}
	return true
}

func isBareKeyRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
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
