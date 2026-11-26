package host

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestUsageRecordsSplitsByModel(t *testing.T) {
	at := time.Date(2026, time.November, 8, 9, 0, 0, 0, time.UTC)
	event := contracts.Event{
		Models: []contracts.ModelUsage{
			{Model: "opus", In: 10, Out: 20, CacheRead: 30, CacheCreate: 40, Cost: 0.25},
			{Model: "haiku", In: 1, Out: 2},
		},
		TokensIn: 11,
		Cost:     0.25,
	}
	got := usageRecords("s1", at, "fallback", event)
	if len(got) != 2 {
		t.Fatalf("records = %d, want 2", len(got))
	}
	first := got[0]
	if first.Session != "s1" || first.Model != "opus" || first.In != 10 || first.Out != 20 || first.CacheRead != 30 || first.CacheCreate != 40 || first.Cost != 0.25 || !first.Time.Equal(at) {
		t.Errorf("first record = %+v", first)
	}
	if got[1].Model != "haiku" {
		t.Errorf("second model = %q, want haiku", got[1].Model)
	}
}

func TestUsageRecordsFallsBackToSessionModel(t *testing.T) {
	event := contracts.Event{TokensIn: 5, Tokens: 6, CacheRead: 7, CacheCreate: 8, Cost: 0.1}
	got := usageRecords("s1", time.Now(), "opus", event)
	if len(got) != 1 {
		t.Fatalf("records = %d, want 1", len(got))
	}
	if got[0].Model != "opus" || got[0].In != 5 || got[0].Out != 6 || got[0].CacheRead != 7 || got[0].CacheCreate != 8 || got[0].Cost != 0.1 {
		t.Errorf("record = %+v", got[0])
	}
}

func TestUsageRecordsSkipsEmptyTurn(t *testing.T) {
	if got := usageRecords("s1", time.Now(), "opus", contracts.Event{}); got != nil {
		t.Fatalf("records = %+v, want none", got)
	}
}

func sizedParts(tokens ...int) []contracts.InjectedPart {
	parts := make([]contracts.InjectedPart, 0, len(tokens))
	for _, count := range tokens {
		parts = append(parts, contracts.InjectedPart{Part: contracts.ContextMemory, Tokens: count})
	}
	return parts
}

func TestUsageRecordsStampsTheInjectedTotalOnTheFirstModelOnly(t *testing.T) {
	event := contracts.Event{
		Models: []contracts.ModelUsage{
			{Model: "opus", In: 10, Cost: 0.25},
			{Model: "haiku", In: 1},
		},
		Injected: sizedParts(100, 0, 50, 25, 25),
	}
	got := usageRecords("s1", time.Now(), "fallback", event)
	if len(got) != 2 {
		t.Fatalf("records = %d, want 2", len(got))
	}
	if !got[0].InjectedMeasured || got[0].InjectedTokens != 200 {
		t.Errorf("first record = measured %t, %d tokens; want measured, 200 tokens", got[0].InjectedMeasured, got[0].InjectedTokens)
	}
	if got[1].InjectedMeasured || got[1].InjectedTokens != 0 {
		t.Errorf("second record carries the turn's injection again: %+v", got[1])
	}
}

func TestUsageRecordsMeasuresATurnOfFiveZeroBlocks(t *testing.T) {
	event := contracts.Event{TokensIn: 5, Injected: sizedParts(0, 0, 0, 0, 0)}
	got := usageRecords("s1", time.Now(), "opus", event)
	if len(got) != 1 {
		t.Fatalf("records = %d, want 1", len(got))
	}
	if !got[0].InjectedMeasured || got[0].InjectedTokens != 0 {
		t.Errorf("record = %+v, want a measured turn of 0 tokens", got[0])
	}
}

func TestUsageRecordsLeavesATurnWithoutSizesUnmeasured(t *testing.T) {
	event := contracts.Event{TokensIn: 5, Tokens: 6}
	got := usageRecords("s1", time.Now(), "opus", event)
	if len(got) != 1 {
		t.Fatalf("records = %d, want 1", len(got))
	}
	if got[0].InjectedMeasured || got[0].InjectedTokens != 0 {
		t.Errorf("record = %+v, want an unmeasured turn", got[0])
	}
}
