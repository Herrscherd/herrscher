package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func recordedTurn(t *testing.T, events ...contracts.Event) state.TranscriptEntry {
	t.Helper()
	a := &fanRecorder{}
	a.feed("vas-y")
	toBridge := make(chan contracts.Event, 8)
	fromBridge := make(chan contracts.Event, 8)
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, toBridge, fromBridge)
	entries := make(chan state.TranscriptEntry, 8)
	d.sink.Transcript = func(e state.TranscriptEntry) { entries <- e }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.run(ctx)

	select {
	case in := <-toBridge:
		if in.T != "input" {
			t.Fatalf("first frame to the bridge = %+v, want input", in)
		}
	case <-time.After(time.Second):
		t.Fatal("driver never opened a turn")
	}
	for _, e := range events {
		fromBridge <- e
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-entries:
			if e.Role == "assistant" {
				return e
			}
		case <-deadline:
			t.Fatal("the turn was never recorded")
		}
	}
}

func TestTurnRecordsTheLiveContextNotTheBillingTotals(t *testing.T) {
	got := recordedTurn(t,
		contracts.Event{T: "chunk", Text: "un", TokensIn: 5, CacheRead: 40_000},
		contracts.Event{T: "chunk", Text: "deux", TokensIn: 5, CacheRead: 60_000},
		contracts.Event{T: "reply", Text: "fini", Done: true, TokensIn: 11, CacheRead: 334_662, CacheCreate: 5_831},
	)
	if got.CtxTokens != 60_005 {
		t.Fatalf("ctx_tokens = %d, want the last mid-turn prompt (60005)", got.CtxTokens)
	}
	if got.CacheRead != 334_662 {
		t.Fatalf("cache_read = %d, want the turn's billing total kept intact", got.CacheRead)
	}
}

func TestASingleMessageTurnRecordsItsOwnPrompt(t *testing.T) {
	got := recordedTurn(t,
		contracts.Event{T: "reply", Text: "fini", Done: true, TokensIn: 11, CacheRead: 40_000, CacheCreate: 500},
	)
	if got.CtxTokens != 40_511 {
		t.Fatalf("ctx_tokens = %d, want the turn's only prompt (40511)", got.CtxTokens)
	}
}
