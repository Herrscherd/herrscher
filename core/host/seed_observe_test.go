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

const (
	oneShotReturnMargin = 2 * time.Second
	oneShotTestGrace    = 50 * time.Millisecond
	oneShotHandoffWait  = 10 * time.Second
)

func TestOneShotSeedReturnsWithinItsGraceWhenObserveNeverReturns(t *testing.T) {
	orch := &blockedObserveOrchestrator{seen: make(chan context.Context, 1), release: make(chan struct{})}
	defer close(orch.release)
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "réponse"}, nil
	}
	sess := state.Session{Name: "solo", ChannelID: "channel"}
	runtime := oneShotSeedRuntime{desk: seedDesk(t), persists: newPersistWrites(), messages: newHostMessages(), cliMessages: clitext.NewMessages(), backend: backend, joinGrace: oneShotTestGrace}

	type outcome struct {
		reply string
		err   error
	}
	returned := make(chan outcome, 1)
	started := time.Now()
	go func() {
		reply, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), orch, runtime)
		returned <- outcome{reply: reply, err: err}
	}()

	var turnCtx context.Context
	select {
	case turnCtx = <-orch.seen:
	case <-time.After(oneShotHandoffWait):
		t.Fatal("the turn was never handed to Observe")
	}
	bound := 2*oneShotTestGrace + oneShotReturnMargin
	select {
	case got := <-returned:
		if elapsed := time.Since(started); elapsed < 2*oneShotTestGrace {
			t.Fatalf("the one-shot returned after %v, before its two grace periods of %v", elapsed, 2*oneShotTestGrace)
		}
		if elapsed := time.Since(started); elapsed > bound {
			t.Fatalf("the one-shot returned after %v, want within %v", elapsed, bound)
		}
		if got.err != nil {
			t.Fatalf("runOneShotSeedWith: %v", got.err)
		}
		if got.reply != "réponse" {
			t.Fatalf("reply = %q, want %q", got.reply, "réponse")
		}
	case <-time.After(bound + oneShotHandoffWait):
		t.Fatal("the one-shot waited for an Observe that never returns")
	}
	select {
	case <-turnCtx.Done():
	default:
		t.Fatal("the bridge was not stopped when its grace ran out")
	}
}

type gatedObserveOrchestrator struct {
	cancelled chan struct{}
	release   chan struct{}
	events    chan string
}

func (o *gatedObserveOrchestrator) Context(context.Context) string { return "" }

func (o *gatedObserveOrchestrator) Observe(ctx context.Context, _ contracts.Prompt, _ string) error {
	<-ctx.Done()
	close(o.cancelled)
	<-o.release
	o.events <- "observed"
	return nil
}

func (o *gatedObserveOrchestrator) Consolidate(context.Context) error {
	o.events <- "consolidated"
	return nil
}

func (o *gatedObserveOrchestrator) Close() error {
	o.events <- "closed"
	return nil
}

const gatedHoldWindow = 300 * time.Millisecond

func TestOneShotSeedConsolidatesOnlyAfterTheBridgeReturns(t *testing.T) {
	orch := &gatedObserveOrchestrator{cancelled: make(chan struct{}), release: make(chan struct{}), events: make(chan string, 8)}
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "réponse"}, nil
	}
	sess := state.Session{Name: "solo", ChannelID: "channel"}
	runtime := oneShotSeedRuntime{desk: seedDesk(t), persists: newPersistWrites(), messages: newHostMessages(), cliMessages: clitext.NewMessages(), backend: backend, joinGrace: 2 * gatedHoldWindow}

	returned := make(chan error, 1)
	go func() {
		_, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), orch, runtime)
		returned <- err
	}()

	select {
	case <-orch.cancelled:
	case <-time.After(oneShotHandoffWait):
		t.Fatal("the bridge was never stopped")
	}
	select {
	case event := <-orch.events:
		t.Fatalf("%s before the bridge returned from Observe", event)
	case <-time.After(gatedHoldWindow):
	}
	close(orch.release)

	want := []string{"observed", "consolidated", "closed"}
	for _, event := range want {
		select {
		case got := <-orch.events:
			if got != event {
				t.Fatalf("event = %q, want %q", got, event)
			}
		case <-time.After(oneShotHandoffWait):
			t.Fatalf("no %q event", event)
		}
	}
	if err := <-returned; err != nil {
		t.Fatalf("runOneShotSeedWith: %v", err)
	}
}

func TestBridgeEndWithinStopsWaitingWhenItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bridgeErr := make(chan error)
	type outcome struct {
		ended bool
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		ended, err := bridgeEndWithin(ctx, bridgeErr, time.Hour)
		done <- outcome{ended: ended, err: err}
	}()
	select {
	case got := <-done:
		if got.ended {
			t.Fatal("bridgeEndWithin reports the bridge ended, but it never did")
		}
		if got.err != nil {
			t.Fatal(got.err)
		}
	case <-time.After(oneShotHandoffWait):
		t.Fatal("bridgeEndWithin kept waiting after its context was done")
	}
}
