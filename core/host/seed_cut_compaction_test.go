package host

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	cutSeedBudget       = 2 * time.Second
	lateWriteDelay      = 300 * time.Millisecond
	cutHistoryResult    = "long tool output"
	prunedHistoryResult = "long tool [pruned]"
	pruneKeepChars      = 9
)

type historyEditingBackend struct {
	mu        sync.Mutex
	committed string
	prunes    int
	cutLanded atomic.Bool
}

func newHistoryEditingBackend() *historyEditingBackend {
	return &historyEditingBackend{committed: cutHistoryResult}
}

func (b *historyEditingBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 950})
	return "reply", nil
}

func (b *historyEditingBackend) Close() error { return nil }

func (b *historyEditingBackend) ReadHistory(context.Context) (contracts.History, error) {
	return contracts.History{Messages: []contracts.HistoryMessage{{
		Role:  contracts.RoleAssistant,
		Calls: []contracts.HistoryCall{{ID: "c1", Tool: "Read", Result: b.stored()}},
	}}}, nil
}

func (b *historyEditingBackend) PruneHistory(ctx context.Context, edits []contracts.HistoryEdit) (contracts.PruneOutcome, error) {
	b.mu.Lock()
	b.prunes++
	cut := b.prunes == 1
	staged := pruneText(b.committed, edits)
	b.mu.Unlock()
	if cut {
		b.cutLanded.Store(true)
		<-ctx.Done()
		return contracts.PruneOutcome{}, ctx.Err()
	}
	b.mu.Lock()
	b.committed = staged
	b.mu.Unlock()
	return contracts.PruneOutcome{Edited: len(edits)}, nil
}

func pruneText(text string, edits []contracts.HistoryEdit) string {
	for _, edit := range edits {
		runes := []rune(text)
		if len(runes) > edit.KeepChars {
			text = string(runes[:edit.KeepChars]) + " [pruned]"
		}
	}
	return text
}

func (b *historyEditingBackend) stored() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.committed
}

type historyPruningStage struct{}

func (historyPruningStage) Compact(ctx context.Context, req contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	if _, err := req.History.ReadHistory(ctx); err != nil {
		return contracts.CompactResult{}, err
	}
	_, err := req.History.PruneHistory(ctx, []contracts.HistoryEdit{{CallID: "c1", Action: contracts.PruneResult, KeepChars: pruneKeepChars}})
	return contracts.CompactResult{}, err
}

type seedEmissions struct {
	mu     sync.Mutex
	events []contracts.Event
	stamps []time.Time
}

func (s *seedEmissions) record(_ string, event contracts.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	s.stamps = append(s.stamps, time.Now())
}

func (s *seedEmissions) kinds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	kinds := make([]string, 0, len(s.events))
	for index := range s.events {
		kinds = append(kinds, s.events[index].T)
	}
	return kinds
}

func seedWithHistoryStage(backend contracts.Backend, budget time.Duration, emissions *seedEmissions) (string, error) {
	return seedWithStage(historyPruningStage{}, backend, budget, emissions)
}

func seedWithStage(stage contracts.Compactor, backend contracts.Backend, budget time.Duration, emissions *seedEmissions) (string, error) {
	table := newSessionTable(newHostMessages(), clitext.NewMessages())
	policy := compactPolicy(50, "jev")
	policy.plugins["jev"] = stage
	policy.historyEditors["claude"] = true
	policy.limits = contextwindow.NewLimits("1000", nil)
	table.compaction = policy
	runtime := oneShotSeedRuntime{
		desk:        stubSeedDesk(),
		persists:    newPersistWrites(),
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
		timeout:     budget,
		sessions:    table,
		publish:     emissions.record,
		backend: func(context.Context, state.Session) (contracts.Backend, error) {
			return backend, nil
		},
	}
	sess := state.Session{Name: "seed-cut", Vendor: "claude", ChannelID: "channel"}
	return runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", "turn-cut", nil, runtime)
}

func TestSeedCutDuringAHistoryEditLeavesTheSessionUsable(t *testing.T) {
	backend := newHistoryEditingBackend()
	var emissions seedEmissions
	reply, err := seedWithHistoryStage(backend, cutSeedBudget, &emissions)
	if err != nil || reply != "reply" {
		t.Fatalf("seed = (%q, %v), want the reply the turn produced", reply, err)
	}
	if !backend.cutLanded.Load() {
		t.Fatal("the budget ran out before the history edit started, so the cut did not land inside it")
	}
	if got := backend.stored(); got != cutHistoryResult {
		t.Fatalf("history after the cut = %q, want the edit unpublished and the history as it was", got)
	}
	kinds := emissions.kinds()
	if !slices.Contains(kinds, eventwire.EventCompactionFailed) {
		t.Fatalf("events = %v, want a CompactionFailed for the cut edit", kinds)
	}
	if slices.Contains(kinds, eventwire.EventCompactionDone) {
		t.Fatalf("events = %v, a cut compaction must not report itself done", kinds)
	}
	reply, err = seedWithHistoryStage(backend, time.Minute, &emissions)
	if err != nil || reply != "reply" {
		t.Fatalf("next seed = (%q, %v), want the session to answer after the cut", reply, err)
	}
	if got := backend.stored(); got != prunedHistoryResult {
		t.Fatalf("history after the next turn = %q, want the next compaction to apply its edit", got)
	}
	if !slices.Contains(emissions.kinds(), eventwire.EventCompactionDone) {
		t.Fatalf("events = %v, want the next compaction to report itself done", emissions.kinds())
	}
}

func TestSeedDeliversNothingAfterItReturns(t *testing.T) {
	var emissions seedEmissions
	reply, err := seedWithAutomaticCompactionAndPublish(blockingCompactBackend{}, cutSeedBudget, slowFailureNotice(&emissions))
	if err != nil || reply != "reply" {
		t.Fatalf("seed = (%q, %v), want the reply", reply, err)
	}
	returnedAt := time.Now()
	time.Sleep(2 * lateWriteDelay)
	emissions.mu.Lock()
	defer emissions.mu.Unlock()
	for index, stamp := range emissions.stamps {
		if stamp.After(returnedAt) {
			t.Fatalf("event %q was emitted %v after the seed returned", emissions.events[index].T, stamp.Sub(returnedAt))
		}
	}
}

func slowFailureNotice(emissions *seedEmissions) func(string, contracts.Event) {
	return func(session string, event contracts.Event) {
		if event.T == eventwire.EventCompactionFailed {
			time.Sleep(lateWriteDelay)
		}
		emissions.record(session, event)
	}
}
