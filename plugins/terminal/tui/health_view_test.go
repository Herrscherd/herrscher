package tui

import (
	"errors"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

func check(name string, state contracts.HealthState) health.Check {
	return health.Check{Name: name, Report: contracts.ProbeReport{State: state}}
}

func names(checks []health.Check) []string {
	out := make([]string, 0, len(checks))
	for index := range checks {
		out = append(out, checks[index].Name)
	}
	return out
}

func TestProblemChecksKeepsOnlyDegradedAndDown(t *testing.T) {
	checks := []health.Check{
		check("ok", contracts.HealthOK),
		check("slow", contracts.HealthDegraded),
		check("off", contracts.HealthOff),
		check("dead", contracts.HealthDown),
	}
	got := names(problemChecks(checks))
	if want := []string{"slow", "dead"}; !slices.Equal(got, want) {
		t.Fatalf("problemChecks() = %v, want %v", got, want)
	}
}

func TestProblemChecksFallsBackToEveryCheckWhenAllAreHealthy(t *testing.T) {
	checks := []health.Check{check("ok", contracts.HealthOK), check("off", contracts.HealthOff)}
	got := names(problemChecks(checks))
	if want := []string{"ok", "off"}; !slices.Equal(got, want) {
		t.Fatalf("problemChecks() = %v, want %v", got, want)
	}
}

func TestEscapeClosesTheHealthOverlay(t *testing.T) {
	m := newTestModel()
	m.health.open = true
	m.handleHealthKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.health.open {
		t.Fatal("escape must close the health overlay")
	}
}

func TestAFailedRefreshMarksTheDaemonUnreachable(t *testing.T) {
	m := newTestModel()
	m.health.busy = true
	m.handleHealthMsg(healthMsg{err: errors.New("socket gone")})
	if !m.health.failed || m.health.ready || m.health.busy {
		t.Fatalf("after a failed refresh: failed=%t ready=%t busy=%t, want failed=true ready=false busy=false",
			m.health.failed, m.health.ready, m.health.busy)
	}
}

func TestASuccessfulRefreshClearsTheFailure(t *testing.T) {
	m := newTestModel()
	m.health.failed = true
	m.handleHealthMsg(healthMsg{snapshot: health.Snapshot{}})
	if m.health.failed || !m.health.ready {
		t.Fatalf("after a good refresh: failed=%t ready=%t, want failed=false ready=true", m.health.failed, m.health.ready)
	}
}

func TestDeepRefreshIsOneShot(t *testing.T) {
	encoded, err := health.Encode(health.Snapshot{ProbedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{replies: map[string]string{
		verbHealth:                       string(encoded),
		verbHealth + " " + argHealthDeep: string(encoded),
	}}
	m := newModel(backend)
	m.health.open = true

	deepMsg, ok := m.toggleHealthDeep()().(healthMsg)
	if !ok {
		t.Fatal("the deep toggle did not answer with a health message")
	}
	m.handleHealthMsg(deepMsg)
	if !deepMsg.deep || !m.health.shownDeep {
		t.Fatalf("deep refresh = %+v, shownDeep = %v, want a deep probe shown", deepMsg, m.health.shownDeep)
	}

	plainMsg, ok := m.healthRefresh()().(healthMsg)
	if !ok {
		t.Fatal("the plain refresh did not answer with a health message")
	}
	m.handleHealthMsg(plainMsg)
	if plainMsg.deep || m.health.shownDeep {
		t.Fatalf("next refresh = %+v, shownDeep = %v, want a plain probe after the one-shot", plainMsg, m.health.shownDeep)
	}
	if want := []string{verbHealth, argHealthDeep}; !slices.Equal(backend.dispatched[0], want) {
		t.Fatalf("first dispatch = %q, want %q", backend.dispatched[0], want)
	}
	if want := []string{verbHealth}; !slices.Equal(backend.dispatched[1], want) {
		t.Fatalf("second dispatch = %q, want %q", backend.dispatched[1], want)
	}
}

func TestHealthTickStaysSilentWhileTheOverlayIsClosed(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.health.open = false
	m.health.at = time.Now().Add(-time.Hour)
	if cmd := m.healthTick(); cmd != nil {
		t.Fatal("healthTick returned a refresh while the overlay is closed")
	}
}
