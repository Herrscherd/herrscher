package host

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

func sizedTurn(at time.Time, tokens ...int) state.TranscriptEntry {
	entry := state.TranscriptEntry{Ts: at.UTC().Format(time.RFC3339), Role: "assistant"}
	for _, count := range tokens {
		entry.Injected = append(entry.Injected, contracts.InjectedPart{Part: contracts.ContextMemory, Tokens: count})
	}
	return entry
}

func TestInjectionOfCountsOnlyTheAssistantTurnsThatCarrySizes(t *testing.T) {
	since := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	userTurn := sizedTurn(since.Add(time.Hour), 900)
	userTurn.Role = "user"
	unsized := state.TranscriptEntry{Ts: since.Add(time.Hour).Format(time.RFC3339), Role: "assistant"}
	entries := []state.TranscriptEntry{
		userTurn,
		unsized,
		sizedTurn(since.Add(-time.Hour), 700),
		sizedTurn(since.Add(2*time.Hour), 100, 0, 50),
		sizedTurn(since.Add(3*time.Hour), 300),
	}
	got := injectionOf(entries, since)
	if got != (usage.Injection{Turns: 2, Tokens: 450}) {
		t.Fatalf("injection = %+v, want two sized turns of 450 tokens in all", got)
	}
}

func TestInjectionOfWithoutSizedTurnsHasNoAverage(t *testing.T) {
	entries := []state.TranscriptEntry{{Ts: time.Now().UTC().Format(time.RFC3339), Role: "assistant"}}
	if _, ok := injectionOf(entries, time.Time{}).Average(); ok {
		t.Fatal("a session of older turns reads an average injection")
	}
}

func TestUsageSourceInjectionsReadsTheTranscriptOfEachSession(t *testing.T) {
	partDir := t.TempDir()
	now := time.Now().UTC()
	if err := state.AppendTranscript(state.TranscriptPath(partDir, "alpha"), sizedTurn(now, 120, 80)); err != nil {
		t.Fatal(err)
	}
	if err := state.AppendTranscript(state.TranscriptPath(partDir, "alpha"), sizedTurn(now, 40)); err != nil {
		t.Fatal(err)
	}
	records := []usage.Record{
		{Session: "alpha", Cost: 1},
		{Session: "beta", Cost: 1},
		{Session: "alpha", Cost: 1},
		{Session: "gamma", Compaction: &usage.Compaction{Stage: "backend"}},
	}
	source := &usageSource{partDir: partDir}
	got := source.injections(records, now.Add(-time.Hour))
	if got["alpha"] != (usage.Injection{Turns: 2, Tokens: 240}) {
		t.Errorf("alpha = %+v, want two turns of 240 tokens in all", got["alpha"])
	}
	if _, ok := got["beta"].Average(); ok {
		t.Errorf("beta has no transcript but reads an average: %+v", got["beta"])
	}
	if _, present := got["gamma"]; present {
		t.Errorf("a compaction run is read as a session: %+v", got)
	}
}
