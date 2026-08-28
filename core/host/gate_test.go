package host

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// gateRegistry installs a two-backend registry: one that gates per tool call,
// one that declares it cannot and says why.
func gateRegistry(t *testing.T) {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind: "claude", Category: contracts.CategoryBackend,
			Capabilities: contracts.Capabilities{Gate: contracts.GrainTool},
		},
	})
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind: "cursor", Category: contracts.CategoryBackend,
			Capabilities: contracts.Capabilities{
				Gate:    contracts.GrainNone,
				GateWhy: "cursor-agent exposes no permission hook",
			},
		},
	})
}

func TestGateForReadsTheManifest(t *testing.T) {
	gateRegistry(t)

	if grain, _ := GateFor("claude"); grain != string(contracts.GrainTool) {
		t.Fatalf("claude grain = %q, want tool", grain)
	}
	grain, why := GateFor("cursor")
	if grain != "" {
		t.Fatalf("cursor grain = %q, want nothing", grain)
	}
	if !strings.Contains(why, "permission hook") {
		t.Fatalf("cursor gives no reason an operator can act on: %q", why)
	}
}

// A vendor nothing here knows about must read as ungated with a reason, not as
// gated by default: claiming enforcement that does not exist is the one answer
// that gets an operator hurt.
func TestGateForUnknownVendorSaysSoRatherThanAssuming(t *testing.T) {
	gateRegistry(t)

	for _, vendor := range []string{"", "gemini"} {
		grain, why := GateFor(vendor)
		if grain != "" {
			t.Fatalf("vendor %q reported grain %q", vendor, grain)
		}
		if why == "" {
			t.Fatalf("vendor %q was refused without a reason", vendor)
		}
	}
}
