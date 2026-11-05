package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func sonnetRates() modelRates {
	return modelRates{input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75}
}

func TestEstimateTurnCostKeepsBackendReportedCost(t *testing.T) {
	tokens := priceableTokens{uncachedInput: 1000, output: 500}
	if got := estimateTurnCost(0.42, tokens, sonnetRates()); got != 0.42 {
		t.Fatalf("estimateTurnCost = %v, want the backend's 0.42 untouched", got)
	}
}

func TestEstimateTurnCostPricesEveryCategory(t *testing.T) {
	tokens := priceableTokens{uncachedInput: 1000, output: 500, cacheRead: 2000, cacheWrite: 400}
	want := (1000*3.0 + 500*15.0 + 2000*0.3 + 400*3.75) / 1_000_000
	if got := estimateTurnCost(0, tokens, sonnetRates()); got != want {
		t.Fatalf("estimateTurnCost = %v, want %v", got, want)
	}
}

func TestEstimateTurnCostBlocksOnUnknownPrice(t *testing.T) {
	tokens := priceableTokens{uncachedInput: 1000, output: 500}
	partial := modelRates{input: 3}
	if got := estimateTurnCost(0, tokens, partial); got != 0 {
		t.Fatalf("estimateTurnCost = %v, want 0: a partial estimate must never be shown as a cost", got)
	}
}

func TestCompleteTurnStoresEstimatedCostInTranscript(t *testing.T) {
	var entries []state.TranscriptEntry
	d := newSessionDriver("s1", nil, nil, nil)
	d.sink = sessionSink{Transcript: func(e state.TranscriptEntry) { entries = append(entries, e) }}
	d.terms = func() modelTerms { return modelTerms{rates: sonnetRates()} }
	event := contracts.Event{T: "reply", Text: "hello", Done: true, TokensIn: 1000, Tokens: 500}

	d.completeTurn(context.Background(), event, time.Now(), 0)

	want := (1000*3.0 + 500*15.0) / 1_000_000
	if len(entries) != 1 || entries[0].Cost != want {
		t.Fatalf("transcript entries = %+v, want one assistant entry costing %v", entries, want)
	}
}
