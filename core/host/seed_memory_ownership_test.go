package host

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
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
	seen         chan struct{}
	release      chan struct{}
	memory       contracts.Memory
	consolidated atomic.Int32
}

func (o *memoryWritingObserveOrchestrator) Context(context.Context) string { return "" }

func (o *memoryWritingObserveOrchestrator) Observe(ctx context.Context, _ contracts.Prompt, _ string) error {
	o.seen <- struct{}{}
	<-o.release
	return o.memory.Record(ctx, contracts.Node{})
}

func (o *memoryWritingObserveOrchestrator) Consolidate(context.Context) error {
	o.consolidated.Add(1)
	return nil
}
func (o *memoryWritingObserveOrchestrator) Close() error { return nil }

func registerSeedPlugins(t *testing.T, mem contracts.Memory, orch contracts.Orchestrator) *atomic.Int32 {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	var opened atomic.Int32
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "guard-memory", Category: contracts.CategoryMemory},
		Memory: func(context.Context, contracts.PluginConfig) (contracts.Memory, error) {
			opened.Add(1)
			return mem, nil
		},
	})
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "guard-orchestrator", Category: contracts.CategoryOrchestrator},
		Orchestrator: func(context.Context, contracts.PluginConfig, contracts.Memory) (contracts.Orchestrator, error) {
			return orch, nil
		},
	})
	return &opened
}

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

	if n := orch.consolidated.Load(); n != 0 {
		t.Fatalf("consolidated %d time(s) while the bridge was not joined, want 0", n)
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

func TestOneShotSeedClosesMemoryOnlyWhenTheBridgeEnds(t *testing.T) {
	mem := newCloseGuardMemory()
	orch := &memoryWritingObserveOrchestrator{seen: make(chan struct{}, 1), release: make(chan struct{}), memory: mem}
	store := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := store.AddSession(state.Session{Name: "solo", ChannelID: "channel", Incarnation: "solo"}); err != nil {
		t.Fatal(err)
	}
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "réponse"}, nil
	}
	opened := registerSeedPlugins(t, mem, orch)
	runtime := oneShotSeedRuntime{
		desk:        seedDesk(t),
		persists:    newPersistWrites(),
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
		backend:     backend,
		joinGrace:   oneShotTestGrace,
	}

	returned := make(chan error, 1)
	go func() {
		_, err := runOneShotSeedIDWithRuntime(context.Background(), store, "solo", "tâche", newTurnID(), runtime)
		returned <- err
	}()

	select {
	case <-orch.seen:
	case <-time.After(oneShotHandoffWait):
		t.Fatal("the turn was never handed to Observe")
	}
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(oneShotHandoffWait):
		t.Fatal("the one-shot never returned while Observe was blocked")
	}
	if n := mem.closeCalls.Load(); n != 0 {
		t.Fatalf("memory closed %d time(s) at return while Observe still runs, want 0", n)
	}
	if n := opened.Load(); n != 1 {
		t.Fatalf("the seed opened memory %d time(s), want 1 through the registry", n)
	}

	close(orch.release)
	select {
	case <-mem.closedCh:
	case <-time.After(oneShotHandoffWait):
		t.Fatal("memory was never closed after the bridge ended")
	}
	if n := mem.violations.Load(); n != 0 {
		t.Fatalf("%d write(s) reached memory after Close", n)
	}
	if n := mem.closeCalls.Load(); n != 1 {
		t.Fatalf("memory closed %d times, want 1", n)
	}
}

func TestOneShotSeedWithoutDeskOpensNothing(t *testing.T) {
	store := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := store.AddSession(state.Session{Name: "solo", ChannelID: "channel", Incarnation: "solo"}); err != nil {
		t.Fatal(err)
	}
	opened := registerSeedPlugins(t, newCloseGuardMemory(), nil)
	runtime := oneShotSeedRuntime{
		persists:    newPersistWrites(),
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
	}
	_, err := runOneShotSeedIDWithRuntime(t.Context(), store, "solo", "tâche", newTurnID(), runtime)
	if !errors.Is(err, errSeedWithoutApprovalDesk) {
		t.Fatalf("err = %v, want %v", err, errSeedWithoutApprovalDesk)
	}
	if n := opened.Load(); n != 0 {
		t.Fatalf("the seed opened memory %d time(s) before refusing, want 0", n)
	}
}

func TestRefusedSeedClosesMemoryHandedToIt(t *testing.T) {
	mem := newCloseGuardMemory()
	runtime := oneShotSeedRuntime{persists: newPersistWrites(), messages: newHostMessages(), cliMessages: clitext.NewMessages()}
	_, err := runOneShotSeedWithIDRuntime(t.Context(), state.Session{Name: "solo", ChannelID: "channel"}, "tâche", newTurnID(), nil, runtime, mem)
	if !errors.Is(err, errSeedWithoutApprovalDesk) {
		t.Fatalf("err = %v, want %v", err, errSeedWithoutApprovalDesk)
	}
	if n := mem.closeCalls.Load(); n != 1 {
		t.Fatalf("memory closed %d times on the refusal, want 1", n)
	}
}

func TestLocalOneShotReportsAFailedLedgerWrite(t *testing.T) {
	partDir := filepath.Join(t.TempDir(), "part")
	if err := os.WriteFile(partDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	messages := newHostMessages()
	runtime := oneShotSeedRuntime{
		localRun:    true,
		desk:        seedDesk(t),
		persists:    newPersistWrites(),
		messages:    messages,
		cliMessages: clitext.NewMessages(),
		warnings:    &warnings,
	}
	ledger := localUsageLedger(partDir, state.NewState(filepath.Join(t.TempDir(), "state.json")), messages, &warnings)
	runtime.recordUsage = ledger.oneShotRecorder(runtime)
	driver := &sessionDriver{messages: messages, cliMessages: runtime.cliMessages, persists: runtime.persists}
	gate := attachOneShotTaps(driver, state.Session{Name: "solo"}, runtime)
	driver.sink.Usage([]usage.Record{{In: 3, Out: 2}})
	gate.shut()
	if !strings.Contains(warnings.String(), "usage not recorded") || !strings.Contains(warnings.String(), "solo") {
		t.Fatalf("warnings = %q, want the warn.session_usage line for session solo", warnings.String())
	}
}
