package host

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func registerBackendKinds(t *testing.T, kinds ...string) {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	for _, kind := range kinds {
		contracts.Default.Register(contracts.Plugin{
			Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryBackend, Config: []contracts.Setting{{Key: "cmd", Default: kind}}},
			Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
				return nil, nil
			},
		})
	}
}

func TestLaunchVendorResolvesTheVendorFromTheRequestThenTheCommand(t *testing.T) {
	registerBackendKinds(t, "claude", "codex")
	t.Setenv("HERRSCHER_BACKEND", "")
	cases := []struct {
		name, vendor, cmd, want string
	}{
		{name: "an explicit vendor beats the command", vendor: "codex", cmd: "claude --x", want: "codex"},
		{name: "the command names the vendor", cmd: "claude --x", want: "claude"},
		{name: "the command behind env names the vendor", cmd: "env HERRSCHER_X=1 /opt/bin/codex", want: "codex"},
		{name: "no hint falls back to the first backend", want: "claude"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := launchVendor(nil, tc.vendor, "", tc.cmd)
			if err != nil {
				t.Fatalf("launchVendor(%q, %q): %v", tc.vendor, tc.cmd, err)
			}
			if got != tc.want {
				t.Fatalf("launchVendor(%q, %q) = %q, want %q", tc.vendor, tc.cmd, got, tc.want)
			}
		})
	}
}

func TestLaunchVendorReadsTheEnvironmentVendorWhenNothingIsRequested(t *testing.T) {
	registerBackendKinds(t, "claude", "codex")
	t.Setenv("HERRSCHER_BACKEND", "codex")
	got, err := launchVendor(nil, "", "", "claude --x")
	if err != nil {
		t.Fatalf("launchVendor: %v", err)
	}
	if got != "codex" {
		t.Fatalf("launchVendor with HERRSCHER_BACKEND=codex = %q, want codex (the decision left open in the review)", got)
	}
}
