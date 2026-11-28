package host

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type usageReplyBackend struct{}

func (usageReplyBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 950})
	return "reply", nil
}

func (usageReplyBackend) Close() error { return nil }

type blockingCompactBackend struct{ usageReplyBackend }

func (blockingCompactBackend) Compact(ctx context.Context, _ contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	<-ctx.Done()
	return contracts.CompactResult{}, ctx.Err()
}

type countingCompactBackend struct {
	usageReplyBackend

	calls *atomic.Int32
}

func (b countingCompactBackend) Compact(context.Context, contracts.CompactRequest, func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	b.calls.Add(1)
	return contracts.CompactResult{TokensAfter: 50_000, TokensMeasured: true}, nil
}

func seedWithAutomaticCompactionAndPublish(backend contracts.Backend, timeout time.Duration, publish func(string, contracts.Event)) (string, error) {
	table := newSessionTable(newHostMessages(), clitext.NewMessages(), nil, nil)
	policy := compactPolicy(50, contracts.CompactStageBackend)
	policy.limits = contextwindow.NewLimits("1000", nil)
	table.compaction = policy
	runtime := oneShotSeedRuntime{
		desk:        stubSeedDesk(),
		persists:    newPersistWrites(),
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
		timeout:     timeout,
		sessions:    table,
		publish:     publish,
		backend: func(context.Context, state.Session) (contracts.Backend, error) {
			return backend, nil
		},
	}
	sess := state.Session{Name: "seed-a", Vendor: "claude", ChannelID: "channel"}
	return runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", "turn-1", nil, runtime)
}

func TestSeedReturnsItsReplyWhenTheAutomaticCompactionOutlivesTheSeed(t *testing.T) {
	for range 5 {
		reply, err := seedWithAutomaticCompactionAndPublish(blockingCompactBackend{}, cutSeedBudget, nil)
		if err != nil {
			t.Fatalf("seed error = %v, want the reply that the turn already produced", err)
		}
		if reply != "reply" {
			t.Fatalf("reply = %q, want %q", reply, "reply")
		}
	}
}

func TestSeedReleasesTheBridgeOnlyAfterTheAutomaticCompactionIsServed(t *testing.T) {
	for range 200 {
		var calls atomic.Int32
		reply, err := seedWithAutomaticCompactionAndPublish(countingCompactBackend{calls: &calls}, time.Minute, nil)
		if err != nil || reply != "reply" {
			t.Fatalf("seed = (%q, %v), want the reply", reply, err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("compaction calls when the seed returned = %d, want 1: the bridge was released before the turn", got)
		}
	}
}

type contextDeafCompactBackend struct {
	usageReplyBackend

	release <-chan struct{}
}

func (b contextDeafCompactBackend) Compact(context.Context, contracts.CompactRequest, func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	<-b.release
	return contracts.CompactResult{}, context.Canceled
}

func TestSeedReturnsWhenACompactionIgnoresItsContext(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := time.Now()
	reply, err := seedWithAutomaticCompactionAndPublish(contextDeafCompactBackend{release: release}, cutSeedBudget, nil)
	if err != nil || reply != "reply" {
		t.Fatalf("seed = (%q, %v), want the reply the turn produced", reply, err)
	}
	waits := cutSeedBudget + 2*seedJoinGrace
	if elapsed := time.Since(started); elapsed > 2*waits {
		t.Fatalf("seed returned after %v, want it within twice its waits (%v)", elapsed, waits)
	}
}
