package host

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

type plainReplyBackend struct{}

func (plainReplyBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "reply", nil
}

func (plainReplyBackend) Close() error { return nil }

type lateUsageBackend struct {
	release <-chan struct{}
	sent    chan struct{}
}

func (b lateUsageBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	go func() {
		<-b.release
		onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 10})
		close(b.sent)
	}()
	return "reply", nil
}

func (lateUsageBackend) Close() error { return nil }

type stampedLog struct {
	mu     sync.Mutex
	kinds  []string
	stamps []time.Time
}

func (l *stampedLog) note(kind string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.kinds = append(l.kinds, kind)
	l.stamps = append(l.stamps, time.Now())
}

func (l *stampedLog) since(at time.Time) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var late []string
	for index, stamp := range l.stamps {
		if stamp.After(at) {
			late = append(late, l.kinds[index])
		}
	}
	return late
}

func (l *stampedLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.kinds)
}

func seedSharingSession(backend contracts.Backend, budget time.Duration, log *stampedLog) (string, error) {
	table := newSessionTable(newHostMessages(), clitext.NewMessages())
	policy := compactPolicy(50, contracts.CompactStageBackend)
	policy.limits = contextwindow.NewLimits("1000", nil)
	table.compaction = policy
	runtime := oneShotSeedRuntime{
		persists: newPersistWrites(),
		messages: newHostMessages(),
		timeout:  budget,
		sessions: table,
		publish:  func(string, contracts.Event) { log.note(eventwire.EventReply) },
		record:   func(string, state.TranscriptEntry) { log.note("transcript") },
		recordUsage: func(state.Session, []usage.Record) {
			log.note("usage")
		},
		backend: func(context.Context, state.Session) (contracts.Backend, error) {
			return backend, nil
		},
	}
	sess := state.Session{Name: "seed-a", Vendor: "claude", ChannelID: "channel"}
	return runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", "turn-1", nil, runtime)
}

func TestSeedWritesNothingAfterItReturnsEvenWhenABackendSpeaksLate(t *testing.T) {
	release := make(chan struct{})
	sent := make(chan struct{})
	var first stampedLog
	reply, err := seedSharingSession(lateUsageBackend{release: release, sent: sent}, cutSeedBudget, &first)
	if err != nil || reply != "reply" {
		t.Fatalf("seed = (%q, %v), want the reply the turn produced", reply, err)
	}
	returned := time.Now()
	before := first.snapshot()
	close(release)
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("the late backend event was never sent, so the late write is not tested")
	}
	time.Sleep(lateWriteDelay)
	if late := first.since(returned); len(late) != 0 {
		t.Fatalf("the first seed wrote %v after it returned, want nothing (before return: %v)", late, before)
	}
	var same stampedLog
	reply, err = seedSharingSession(plainReplyBackend{}, time.Minute, &same)
	if err != nil || reply != "reply" {
		t.Fatalf("next seed = (%q, %v), want the session to answer", reply, err)
	}
	if slices.Contains(same.snapshot(), "usage") && !slices.Contains(first.snapshot(), "usage") {
		t.Fatalf("the next seed of the same name shows usage it did not produce: %v", same.snapshot())
	}
}

func TestOneShotTapsRecordNothingOnceTheSeedHasReturned(t *testing.T) {
	var recorded []string
	runtime := oneShotSeedRuntime{
		persists:    newPersistWrites(),
		messages:    newHostMessages(),
		publish:     func(string, contracts.Event) { recorded = append(recorded, "publish") },
		record:      func(string, state.TranscriptEntry) { recorded = append(recorded, "transcript") },
		recordUsage: func(state.Session, []usage.Record) { recorded = append(recorded, "usage") },
	}
	driver := &sessionDriver{messages: newHostMessages(), cliMessages: clitext.NewMessages(), persists: newPersistWrites()}
	gate := attachOneShotTaps(driver, state.Session{Name: "seed-a"}, runtime)

	driver.emitTap(contracts.Event{T: eventwire.EventReply})
	driver.sink.Transcript(state.TranscriptEntry{})
	driver.sink.Usage([]usage.Record{{In: 1}})
	if want := []string{"publish", "transcript", "usage"}; !slices.Equal(recorded, want) {
		t.Fatalf("while the seed runs the taps recorded %v, want %v", recorded, want)
	}

	gate.shut()
	driver.emitTap(contracts.Event{T: eventwire.EventReply})
	driver.sink.Transcript(state.TranscriptEntry{})
	driver.sink.Usage([]usage.Record{{In: 1}})
	if want := []string{"publish", "transcript", "usage"}; !slices.Equal(recorded, want) {
		t.Fatalf("after the seed returned the taps recorded %v, want nothing more than %v", recorded, want)
	}
}
