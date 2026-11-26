package agent

import (
	"fmt"
	"sort"
	"strings"
)

func renderCodexMCP(servers []MCPServer, worktree string) []byte {
	var out strings.Builder
	for index, server := range servers {
		if index > 0 {
			out.WriteByte('\n')
		}
		writeCodexMCPServer(&out, server, worktree)
	}
	return []byte(out.String())
}

func checkCodexTransport(server MCPServer) error {
	if server.Type != "" && server.Type != TransportStdio {
		return fmt.Errorf("%w %q for server %q", ErrUnsupportedMCPTransport, server.Type, server.Name)
	}
	return nil
}

func writeCodexMCPServer(out *strings.Builder, server MCPServer, worktree string) {
	table := "[mcp_servers." + tomlKey(server.Name) + "]\n"
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
	out.WriteString("[mcp_servers." + tomlKey(server.Name) + ".env]\n")
	for _, key := range sortedKeys(server.Env) {
		out.WriteString(tomlKey(key) + " = " + substituteWorktree(server.Env[key], worktree) + "\n")
	}
}

func substituteWorktree(value, worktree string) string {
	return tomlString(strings.ReplaceAll(value, worktreeToken, worktree))
}

func sortedKeys[V any](entries map[string]V) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
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
	for _, character := range value {
		switch character {
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
			if character < 0x20 || character == 0x7f {
				fmt.Fprintf(&out, `\u%04X`, character)
			} else {
				out.WriteRune(character)
			}
		}
	}
	out.WriteByte('"')
	return out.String()
}
