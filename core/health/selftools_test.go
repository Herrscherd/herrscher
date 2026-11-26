package health_test

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

func selfToolsSnapshot(facts health.SelfTools) health.Snapshot {
	return health.Snapshot{Checks: []health.Check{{
		Group:  health.GroupCore,
		Name:   health.ServiceSelfTools,
		Report: contracts.ProbeReport{State: contracts.HealthOK},
		Facts:  health.Facts{SelfTools: &facts},
	}}}
}

func TestSelfToolsFactsSurviveTheWireAndShowTheirWeight(t *testing.T) {
	raw, err := health.Encode(selfToolsSnapshot(health.SelfTools{Enabled: true, Exposure: "core", Tools: 18, SchemaBytes: 9214}))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := health.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Checks[0].Facts.SelfTools; got == nil || got.Tools != 18 || got.SchemaBytes != 9214 || !got.Enabled {
		t.Fatalf("decoded self tools facts = %+v, from %s", got, raw)
	}
	if out := tableOf(t, decoded); !strings.Contains(out, "18 tools") || !strings.Contains(out, "9214 bytes") {
		t.Fatalf("table:\n%s\nwant the tool count and their weight", out)
	}
	if out := tableOf(t, selfToolsSnapshot(health.SelfTools{})); !strings.Contains(out, "sessions get no herrscher MCP server") {
		t.Fatalf("table:\n%s\nwant the off state said", out)
	}
}
