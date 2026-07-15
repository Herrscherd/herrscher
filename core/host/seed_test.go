package host

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type seedSpyOrchestrator struct {
	consolidated bool
	closed       bool
}

func (s *seedSpyOrchestrator) Context(context.Context) string                          { return "" }
func (s *seedSpyOrchestrator) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (s *seedSpyOrchestrator) Consolidate(context.Context) error {
	s.consolidated = true
	return nil
}
func (s *seedSpyOrchestrator) Close() error {
	s.closed = true
	return nil
}

func TestRunOneShotSeedWithConsolidatesAndCloses(t *testing.T) {
	old := oneShotBackendFactory
	t.Cleanup(func() { oneShotBackendFactory = old })
	oneShotBackendFactory = func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{}, nil
	}

	sess := state.Session{Name: "solo", ChannelID: "channel"}
	spy := &seedSpyOrchestrator{}
	if _, err := runOneShotSeedWith(context.Background(), sess, "tâche", spy); err != nil {
		t.Fatalf("runOneShotSeedWith: %v", err)
	}
	if !spy.consolidated {
		t.Fatal("expected consolidation after the one-shot turn")
	}
	if !spy.closed {
		t.Fatal("expected orchestrator close after the one-shot turn")
	}
}

type seedBackend struct{}

func (seedBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "reply", nil
}
func (seedBackend) Close() error { return nil }
