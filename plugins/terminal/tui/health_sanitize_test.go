package tui

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

func TestHealthBodyNeutralisesACheckName(t *testing.T) {
	m := newTestModel()
	m.health.ready = true
	m.health.snapshot = health.Snapshot{Checks: []health.Check{
		{Group: health.GroupBackends, Name: "claude\x1b]0;pwn\a", Report: contracts.ProbeReport{State: contracts.HealthDown}},
	}}
	out := strings.Join(m.healthBody(), "\n")
	if strings.Contains(out, "\x1b]0;pwn") {
		t.Fatalf("healthBody() carries the raw OSC of the check name:\n%q", out)
	}
	if want := `claude\x1b]0;pwn\a`; !strings.Contains(out, want) {
		t.Errorf("healthBody() does not show the check name as %q:\n%s", want, out)
	}
}

func TestHealthBodyKeepsTheEscapeLowerCaseInAnUpperCasedTitle(t *testing.T) {
	m := newTestModel()
	m.health.ready = true
	m.health.snapshot = health.Snapshot{
		CompactStack: []string{"jev\x1b[2J"},
		Checks: []health.Check{
			{Group: health.GroupCompaction, Name: "compact", Report: contracts.ProbeReport{State: contracts.HealthOK}},
		},
	}
	out := strings.Join(m.healthBody(), "\n")
	if strings.Contains(out, `\X1B`) {
		t.Fatalf("healthBody() uppercases the escape of a section title:\n%s", out)
	}
	if want := `\x1b[2J`; !strings.Contains(out, want) {
		t.Errorf("healthBody() does not show the section title escape as %q:\n%s", want, out)
	}
}
