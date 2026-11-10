package host

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestVendorOfCmdReadsTheBinaryBehindEnvAndQuotes(t *testing.T) {
	plugin := contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "claude", Category: contracts.CategoryBackend, Config: []contracts.Setting{{Key: "cmd", Default: "claude"}}},
		Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
			return nil, nil
		},
	}
	plugins := []contracts.Plugin{plugin}
	cases := map[string]string{
		"claude --model x":             "claude",
		"/usr/bin/claude":              "claude",
		"env HERRSCHER_X=1 claude --x": "claude",
		"env A=1 B=2 /opt/bin/claude":  "claude",
		`"/opt/my tools/claude" --x`:   "claude",
		`'/opt/my tools/claude' --x`:   "claude",
		"codex --x":                    "",
		"env":                          "",
		"":                             "",
	}
	for cmd, want := range cases {
		if got := vendorOfCmd(cmd, plugins); got != want {
			t.Errorf("vendorOfCmd(%q) = %q, want %q", cmd, got, want)
		}
	}
}
