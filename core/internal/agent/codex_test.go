package agent

import (
	"strings"
	"testing"
)

func TestRenderCodexMCP(t *testing.T) {
	worktree := `C:\Users\Ada\Work Trees\session`
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{
			name: "renders Windows command arguments environment and substitution deterministically",
			raw: `{
  "mcpServers": {
    "zeta": {"type": "stdio", "command": "zeta"},
    "roblox": {
      "type": "stdio",
      "command": "C:\\Program Files\\Roblox Tools\\mcp.exe",
      "args": ["serve", "--project", "{{WORKTREE}}"],
      "env": {"Z_LAST": "{{WORKTREE}}", "API_KEY": "value with spaces"}
    }
  }
}`,
			want: `[mcp_servers.roblox]
command = "C:\\Program Files\\Roblox Tools\\mcp.exe"
args = ["serve", "--project", "C:\\Users\\Ada\\Work Trees\\session"]

[mcp_servers.roblox.env]
API_KEY = "value with spaces"
Z_LAST = "C:\\Users\\Ada\\Work Trees\\session"

[mcp_servers.zeta]
command = "zeta"
`,
		},
		{
			name: "treats a type-less server as stdio",
			raw:  `{"mcpServers":{"roblox":{"command":"mcp"}}}`,
			want: "[mcp_servers.roblox]\ncommand = \"mcp\"\n",
		},
		{
			name:    "rejects an empty server name",
			raw:     `{"mcpServers":{"":{"type":"stdio","command":"mcp"}}}`,
			wantErr: "empty MCP server name",
		},
		{
			name:    "rejects malformed JSON",
			raw:     `{`,
			wantErr: "mcp.json",
		},
		{
			name:    "rejects unsupported MCP transport",
			raw:     `{"mcpServers":{"remote":{"type":"sse","command":"remote"}}}`,
			wantErr: "unsupported MCP transport",
		},
		{
			name:    "rejects missing command",
			raw:     `{"mcpServers":{"broken":{"type":"stdio","command":""}}}`,
			wantErr: "missing command",
		},
		{
			name:    "reports an unsupported transport before a later server's missing command",
			raw:     `{"mcpServers":{"a":{"type":"http"},"b":{"command":""}}}`,
			wantErr: "unsupported MCP transport",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkCodexMCPCase(t, tt.raw, tt.want, tt.wantErr, worktree)
		})
	}
}

func checkCodexMCPCase(t *testing.T, raw, want, wantErr, worktree string) {
	t.Helper()
	servers, err := parseCodexMCPServers([]byte(raw))
	var got []byte
	if err == nil {
		got = renderCodexMCP(servers, worktree)
	}
	if wantErr != "" {
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("renderCodexMCP() error = %v, want error containing %q", err, wantErr)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("renderCodexMCP() =\n%s\nwant:\n%s", got, want)
	}
}
