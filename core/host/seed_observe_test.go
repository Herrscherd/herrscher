package host

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const slowObserveDelay = 200 * time.Millisecond

type slowObserveOrchestrator struct {
	observed atomic.Bool
}

func (o *slowObserveOrchestrator) Context(context.Context) string { return "" }

func (o *slowObserveOrchestrator) Observe(ctx context.Context, _ contracts.Prompt, _ string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(slowObserveDelay):
	}
	o.observed.Store(true)
	return nil
}

func (o *slowObserveOrchestrator) Consolidate(context.Context) error { return nil }
func (o *slowObserveOrchestrator) Close() error                      { return nil }

func TestOneShotSeedReturnsOnlyAfterTheTurnIsObserved(t *testing.T) {
	orch := &slowObserveOrchestrator{}
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "réponse"}, nil
	}
	sess := state.Session{Name: "solo", ChannelID: "channel"}
	runtime := oneShotSeedRuntime{desk: seedDesk(t), persists: newPersistWrites(), messages: newHostMessages(), cliMessages: clitext.NewMessages(), backend: backend}

	got, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), orch, runtime)
	if err != nil {
		t.Fatalf("runOneShotSeedWith: %v", err)
	}
	if got != "réponse" {
		t.Fatalf("reply = %q, want %q", got, "réponse")
	}
	if !orch.observed.Load() {
		t.Fatal("the one-shot returned before the turn was observed: its memory would be lost")
	}
}
