package health_test

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

func rtkSnapshot(facts health.RTK) health.Snapshot {
	return health.Snapshot{Checks: []health.Check{{
		Group:  health.GroupCore,
		Name:   health.ServiceRTK,
		Report: contracts.ProbeReport{State: contracts.HealthOK},
		Facts:  health.Facts{RTK: &facts},
	}}}
}

func TestRTKFactsSurviveTheWireAndNameTheirState(t *testing.T) {
	raw, err := health.Encode(rtkSnapshot(health.RTK{Present: true, Version: "1.0.1", UserHook: true}))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := health.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Checks[0].Facts.RTK; got == nil || !got.Present || got.Version != "1.0.1" || !got.UserHook {
		t.Fatalf("decoded rtk facts = %+v, from %s", got, raw)
	}
	if out := tableOf(t, decoded); !strings.Contains(out, "rtk 1.0.1") || !strings.Contains(out, "your own Claude hook") {
		t.Fatalf("table:\n%s\nwant the version and the user hook said", out)
	}
}

func TestRTKFactsSayWhenRTKIsAbsent(t *testing.T) {
	out := tableOf(t, rtkSnapshot(health.RTK{}))
	if !strings.Contains(out, "not on PATH") {
		t.Fatalf("table:\n%s\nwant the absence said", out)
	}
}

func TestRTKFactsShowTheVersionWhenPresent(t *testing.T) {
	out := tableOf(t, rtkSnapshot(health.RTK{Present: true, Version: "1.0.1"}))
	if !strings.Contains(out, "rtk 1.0.1") || strings.Contains(out, "your own Claude hook") {
		t.Fatalf("table:\n%s\nwant the version and no user hook line", out)
	}
}
