package host

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestVendorOfCmdReadsTheBinaryBehindEnvAndQuotes(t *testing.T) {
	plugins := []contracts.Plugin{testBackendPlugin("claude")}
	cases := map[string]string{
		"claude --model x":             "claude",
		"/usr/bin/claude":              "claude",
		"env HERRSCHER_X=1 claude --x": "claude",
		"env A=1 B=2 /opt/bin/claude":  "claude",
		"FOO=1 claude --x":             "claude",
		"env -i claude":                "claude",
		"env -u HOME claude":           "claude",
		"env -C /tmp claude":           "claude",
		"env -- claude":                "claude",
		"env -i FOO=1 -u HOME claude":  "claude",
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
