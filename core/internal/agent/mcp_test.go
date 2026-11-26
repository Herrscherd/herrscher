package agent

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseMCPServersSortsByName(t *testing.T) {
	servers, err := ParseMCPServers([]byte(`{"mcpServers":{"zeta":{"command":"z"},"alpha":{"command":"a"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	if want := []string{"alpha", "zeta"}; !slices.Equal(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

func TestParseMCPServersRefusesAnEmptyName(t *testing.T) {
	_, err := ParseMCPServers([]byte(`{"mcpServers":{" ":{"command":"mcp"}}}`))
	if !errors.Is(err, ErrEmptyMCPServerName) {
		t.Fatalf("ParseMCPServers error = %v, want ErrEmptyMCPServerName", err)
	}
}

func TestParseMCPServersRefusesALocalServerWithoutCommand(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "type-less", raw: `{"mcpServers":{"broken":{"args":["x"]}}}`},
		{name: "declared stdio", raw: `{"mcpServers":{"broken":{"type":"stdio","command":" "}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMCPServers([]byte(tt.raw))
			if !errors.Is(err, ErrMissingMCPCommand) {
				t.Fatalf("ParseMCPServers error = %v, want ErrMissingMCPCommand", err)
			}
		})
	}
}

func TestParseMCPServersKeepsRemoteServersWithoutCommand(t *testing.T) {
	servers, err := ParseMCPServers([]byte(`{"mcpServers":{"remote":{"type":"http","url":"https://example.invalid/mcp"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Transport() != "http" || servers[0].Stdio() {
		t.Fatalf("servers = %+v, want one http server that is not stdio", servers)
	}
}

func TestParseMCPServersRefusesMalformedJSON(t *testing.T) {
	_, err := ParseMCPServers([]byte(`{"mcpServers":`))
	if err == nil || !strings.Contains(err.Error(), "parse mcp.json") {
		t.Fatalf("ParseMCPServers error = %v, want a parse error", err)
	}
}

func TestMCPServerTransport(t *testing.T) {
	tests := []struct {
		name   string
		server MCPServer
		want   string
	}{
		{name: "declared type wins", server: MCPServer{Type: "sse", Command: "x"}, want: "sse"},
		{name: "command means stdio", server: MCPServer{Command: "x"}, want: TransportStdio},
		{name: "url means url", server: MCPServer{URL: "https://example.invalid"}, want: "url"},
		{name: "nothing is unknown", server: MCPServer{}, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.server.Transport(); got != tt.want {
				t.Fatalf("Transport() = %q, want %q", got, tt.want)
			}
		})
	}
}
