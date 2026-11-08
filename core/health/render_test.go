package health_test

import (
	"bytes"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/health"
)

func TestTableListsEveryCheckUnderItsGroup(t *testing.T) {
	snapshot := health.Snapshot{Checks: []health.Check{
		{Group: health.GroupGateways, Name: "fakegate", Report: contracts.ProbeReport{State: contracts.HealthOK}},
		{Group: health.GroupBackends, Name: "claude", Report: contracts.ProbeReport{State: contracts.HealthDown}},
	}}
	var out bytes.Buffer
	if err := snapshot.Table(&out, i18n.English); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	for _, name := range []string{"fakegate", "claude"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("Table() output does not list %q:\n%s", name, out.String())
		}
	}
}

func TestDetailsShowsTheRemedyOfACheck(t *testing.T) {
	const command = "herrscher account add claude work"
	check := health.Check{
		Group:   health.GroupBackends,
		Name:    "claude",
		Account: "work",
		Report: contracts.ProbeReport{
			State:  contracts.HealthOff,
			Remedy: contracts.Notice{Code: contracts.CodeHealthRemedyRun, Args: []contracts.Arg{contracts.Text("Command", command)}},
		},
	}
	var out bytes.Buffer
	if err := (health.Snapshot{}).Details(&out, []health.Check{check}, i18n.English); err != nil {
		t.Fatalf("Details() error = %v", err)
	}
	if !strings.Contains(out.String(), command) {
		t.Errorf("Details() output does not carry the remedy %q:\n%s", command, out.String())
	}
}
