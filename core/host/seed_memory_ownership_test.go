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

type closeGuardMemory struct {
	closeCalls atomic.Int32
	writes     atomic.Int32
	violations atomic.Int32
	closed     atomic.Bool
	closedCh   chan struct{}
}

func newCloseGuardMemory() *closeGuardMemory {
	return &closeGuardMemory{closedCh: make(chan struct{}, 1)}
}

func (m *closeGuardMemory) Recall(context.Context, string, int) (contracts.Subgraph, error) {
	return contracts.Subgraph{}, nil
}

func (m *closeGuardMemory) Record(context.Context, contracts.Node) error {
	m.writes.Add(1)
	if m.closed.Load() {
		m.violations.Add(1)
	}
	return nil
}

func (m *closeGuardMemory) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return nil, nil
}

func (m *closeGuardMemory) Links(context.Context, string, string, string) error { return nil }
func (m *closeGuardMemory) Unlink(context.Context, string, string) error        { return nil }

func (m *closeGuardMemory) Close() error {
	m.closeCalls.Add(1)
	m.closed.Store(true)
	select {
	case m.closedCh <- struct{}{}:
	default:
	}
	return nil
}

type memoryWritingObserveOrchestrator struct {
	seen    chan struct{}
	release chan struct{}
	memory  contracts.Memory
}

func (o *memoryWritingObserveOrchestrator) Context(context.Context) string { return "" }

func (o *memoryWritingObserveOrchestrator) Observe(ctx context.Context, _ contracts.Prompt, _ string) error {
	o.seen <- struct{}{}
	<-o.release
	return o.memory.Record(ctx, contracts.Node{})
}

func (o *memoryWritingObserveOrchestrator) Consolidate(context.Context) error { return nil }
func (o *memoryWritingObserveOrchestrator) Close() error                      { return nil }

func TestUnjoinedBridgeClosesMemoryOnlyWhenItEnds(t *testing.T) {
	mem := newCloseGuardMemory()
	orch := &memoryWritingObserveOrchestrator{seen: make(chan struct{}, 1), release: make(chan struct{}), memory: mem}
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
		reply, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), orch, runtime, mem)
		returned <- outcome{reply: reply, err: err}
	}()

	select {
	case <-orch.seen:
	case <-time.After(oneShotHandoffWait):
		t.Fatal("the turn was never handed to Observe")
	}
	bound := 2*oneShotTestGrace + oneShotReturnMargin
	select {
	case got := <-returned:
		if elapsed := time.Since(started); elapsed > bound {
			t.Fatalf("the one-shot returned after %v, want within %v", elapsed, bound)
		}
		if got.err != nil {
			t.Fatalf("runOneShotSeedWith: %v", got.err)
		}
		if got.reply != "réponse" {
			t.Fatalf("reply = %q, want %q", got.reply, "réponse")
		}
	case <-time.After(oneShotHandoffWait):
		t.Fatal("the one-shot never returned while Observe was blocked")
	}

	if n := mem.closeCalls.Load(); n != 0 {
		t.Fatalf("memory closed %d time(s) while Observe still runs, want 0", n)
	}

	close(orch.release)
	select {
	case <-mem.closedCh:
	case <-time.After(oneShotHandoffWait):
		t.Fatal("memory was never closed after the bridge ended")
	}
	if n := mem.writes.Load(); n != 1 {
		t.Fatalf("writes = %d, want 1", n)
	}
	if n := mem.violations.Load(); n != 0 {
		t.Fatalf("%d write(s) reached memory after Close", n)
	}
	if n := mem.closeCalls.Load(); n != 1 {
		t.Fatalf("memory closed %d times, want 1", n)
	}
}
