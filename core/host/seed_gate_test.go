package host

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestATapIsRefusedOnceTheSeedShutsItsGate(t *testing.T) {
	var published []string
	runtime := oneShotSeedRuntime{desk: seedDesk(t), persists: newPersistWrites(), messages: newHostMessages(), cliMessages: clitext.NewMessages(), publish: func(_ string, event contracts.Event) {
		published = append(published, event.T)
	}}
	sess := state.Session{Name: "window"}
	driver := newSessionDriver(newHostMessages(), clitext.NewMessages(), sess.Name, nil, make(chan contracts.Event, 1), make(chan contracts.Event, 1), io.Discard)
	gate := attachOneShotTaps(driver, sess, runtime)

	driver.tapEvent(contracts.Event{T: "before"})
	gate.shut()
	driver.tapEvent(contracts.Event{T: "during consolidation"})

	if len(published) != 1 || published[0] != "before" {
		t.Fatalf("published = %v, want only the tap admitted before the gate shut", published)
	}
}

func TestAResumeTokenIsRefusedOnceTheSeedShutsItsGate(t *testing.T) {
	var resumed []string
	runtime := oneShotSeedRuntime{desk: seedDesk(t), persists: newPersistWrites(), messages: newHostMessages(), cliMessages: clitext.NewMessages(), resume: func(_, token string) {
		resumed = append(resumed, token)
	}}
	sess := state.Session{Name: "window"}
	driver := newSessionDriver(newHostMessages(), clitext.NewMessages(), sess.Name, nil, make(chan contracts.Event, 1), make(chan contracts.Event, 1), io.Discard)
	gate := attachOneShotTaps(driver, sess, runtime)

	driver.sink.Resume("before")
	gate.shut()
	driver.sink.Resume("during consolidation")

	if len(resumed) != 1 || resumed[0] != "before" {
		t.Fatalf("resumed = %v, want only the token admitted before the gate shut", resumed)
	}
}

func TestARefusedDeliveryWarnsOncePerKindWithTheSession(t *testing.T) {
	var warned bytes.Buffer
	gate := newSeedGate(newHostMessages(), "window", &warned, fixedLanguageSource(i18n.English))
	gate.shut()
	refused := func() { t.Fatal("a delivery reached the session after the gate shut") }
	gate.admit(lateEventKey, refused)
	gate.admit(lateEventKey, refused)
	gate.admit(lateResumeKey, refused)
	gate.admit(lateResumeKey, refused)

	lines := strings.Split(strings.TrimSuffix(warned.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("warnings = %q, want one line per refused kind", warned.String())
	}
	if !strings.Contains(lines[0], "event") || !strings.Contains(lines[1], "resume token") {
		t.Fatalf("warnings = %q, want the event line then the resume token line", warned.String())
	}
	for _, line := range lines {
		if !strings.Contains(line, "window") {
			t.Fatalf("warning %q does not name the session", line)
		}
	}
}

type probeOrchestrator struct {
	contracts.Orchestrator

	consolidate func(context.Context) error
}

func (p probeOrchestrator) Consolidate(ctx context.Context) error {
	return p.consolidate(ctx)
}

func TestConsolidationRunsWithTheGateShut(t *testing.T) {
	gate := newSeedGate(newHostMessages(), "window", io.Discard, fixedLanguageSource(i18n.English))
	delivered := false
	orch := probeOrchestrator{consolidate: func(context.Context) error {
		gate.admit(lateEventKey, func() { delivered = true })
		return nil
	}}
	consolidateOneShot(context.Background(), orch, gate, state.Session{Name: "window"}, newHostMessages(), io.Discard, fixedLanguageSource(i18n.English))
	if delivered {
		t.Fatal("a tap reached the session during the consolidation: the gate must be shut before Consolidate")
	}
}
