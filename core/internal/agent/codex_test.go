package agent

import (
	"strings"
	"testing"
)

func TestRenderCodexMCP(t *testing.T) {
	worktree := `C:\Users\Kara\Work Trees\session`
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
args = ["serve", "--project", "C:\\Users\\Kara\\Work Trees\\session"]

[mcp_servers.roblox.env]
API_KEY = "value with spaces"
Z_LAST = "C:\\Users\\Kara\\Work Trees\\session"

[mcp_servers.zeta]
command = "zeta"
`,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderCodexMCP([]byte(tt.raw), worktree)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("renderCodexMCP() error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("renderCodexMCP() =\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
