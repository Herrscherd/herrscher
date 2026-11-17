package host

import (
	"context"
	"io"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestATapIsRefusedOnceTheSeedShutsItsGate(t *testing.T) {
	var published []string
	runtime := oneShotSeedRuntime{publish: func(_ string, event contracts.Event) {
		published = append(published, event.T)
	}}
	sess := state.Session{Name: "window"}
	driver := newSessionDriver(sess.Name, nil, make(chan contracts.Event, 1), make(chan contracts.Event, 1), io.Discard)
	gate := attachOneShotTaps(driver, sess, runtime)

	driver.tapEvent(contracts.Event{T: "before"})
	gate.shut()
	driver.tapEvent(contracts.Event{T: "during consolidation"})

	if len(published) != 1 || published[0] != "before" {
		t.Fatalf("published = %v, want only the tap admitted before the gate shut", published)
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
	gate := &seedGate{}
	delivered := false
	orch := probeOrchestrator{consolidate: func(context.Context) error {
		gate.admit(func() { delivered = true })
		return nil
	}}
	consolidateOneShot(context.Background(), orch, gate, state.Session{Name: "window"}, io.Discard, fixedLanguageSource(i18n.English))
	if delivered {
		t.Fatal("a tap reached the session during the consolidation: the gate must be shut before Consolidate")
	}
}
