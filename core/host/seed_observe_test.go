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

type blockedObserveOrchestrator struct {
	seen    chan context.Context
	release chan struct{}
}

func (o *blockedObserveOrchestrator) Context(context.Context) string { return "" }

func (o *blockedObserveOrchestrator) Observe(ctx context.Context, _ contracts.Prompt, _ string) error {
	o.seen <- ctx
	<-o.release
	return nil
}

func (o *blockedObserveOrchestrator) Consolidate(context.Context) error { return nil }
func (o *blockedObserveOrchestrator) Close() error                      { return nil }

const oneShotReturnDeadline = 10 * time.Second

func TestOneShotSeedReturnsWithinItsGraceWhenObserveNeverReturns(t *testing.T) {
	orch := &blockedObserveOrchestrator{seen: make(chan context.Context, 1), release: make(chan struct{})}
	defer close(orch.release)
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "réponse"}, nil
	}
	sess := state.Session{Name: "solo", ChannelID: "channel"}
	runtime := oneShotSeedRuntime{desk: seedDesk(t), persists: newPersistWrites(), messages: newHostMessages(), cliMessages: clitext.NewMessages(), backend: backend}

	type outcome struct {
		reply string
		err   error
	}
	returned := make(chan outcome, 1)
	go func() {
		reply, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), orch, runtime)
		returned <- outcome{reply: reply, err: err}
	}()

	var turnCtx context.Context
	select {
	case turnCtx = <-orch.seen:
	case <-time.After(oneShotReturnDeadline):
		t.Fatal("the turn was never handed to Observe")
	}
	select {
	case got := <-returned:
		if got.err != nil {
			t.Fatalf("runOneShotSeedWith: %v", got.err)
		}
		if got.reply != "réponse" {
			t.Fatalf("reply = %q, want %q", got.reply, "réponse")
		}
	case <-time.After(oneShotReturnDeadline):
		t.Fatal("the one-shot waited for an Observe that never returns")
	}
	select {
	case <-turnCtx.Done():
	default:
		t.Fatal("the bridge was not stopped when its grace ran out")
	}
}
