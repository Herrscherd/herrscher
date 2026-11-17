package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/outbox"
)

type turnIDPlainGateway struct {
	fanRecorder

	events []contracts.Event
}

func (g *turnIDPlainGateway) Emit(e contracts.Event) {
	g.events = append(g.events, e)
}

type turnIDRoutedGateway struct {
	fanRecorder

	events []contracts.Event
}

func (g *turnIDRoutedGateway) EmitTo(_ contracts.Conversation, e contracts.Event) {
	g.events = append(g.events, e)
}

func TestRecordEntryStampsTheActiveTurn(t *testing.T) {
	var got []state.TranscriptEntry
	d := newSessionDriver("s", nil, make(chan contracts.Event, 8), make(chan contracts.Event, 8), io.Discard)
	d.sink.Transcript = func(e state.TranscriptEntry) { got = append(got, e) }

	d.activeTurnID = "turn-7"
	d.recordEntry("assistant", "hi", 0, turnUsage{})
	d.activeTurnID = ""
	d.recordEntry("notice", "hors tour", 0, turnUsage{})

	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[0].TurnID != "turn-7" {
		t.Fatalf("in-turn entry TurnID = %q, want turn-7", got[0].TurnID)
	}
	if got[1].TurnID != "" {
		t.Fatalf("off-turn entry TurnID = %q, want empty", got[1].TurnID)
	}
}

func TestScrollbackCarriesTurnIDAndOldTranscriptReadsWithout(t *testing.T) {
	partDir := t.TempDir()
	path := state.TranscriptPath(partDir, "s")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"ts":"t","role":"user","text":"avant"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.AppendTranscript(path, state.TranscriptEntry{Ts: "t", Role: "assistant", Text: "ok", TurnID: "turn-7"}); err != nil {
		t.Fatal(err)
	}

	got := scrollbackOf(partDir, "s")
	want := []contracts.ScrollbackLine{
		{Role: "user", Text: "avant"},
		{Role: "assistant", Text: "ok", TurnID: "turn-7"},
	}
	if len(got) != len(want) {
		t.Fatalf("scrollback = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestEveryReplyFanOutCarriesItsTurnID(t *testing.T) {
	plain := &turnIDPlainGateway{}
	routed := &turnIDRoutedGateway{}
	d := newSessionDriver("s1", []contracts.GatewaySet{
		{Gateway: plain, Reader: plain},
		{Gateway: routed, Reader: routed},
	}, nil, nil, io.Discard)
	d.activeTurnID = "turn-7"
	var tapped []contracts.Event
	d.emitTap = func(e contracts.Event) { tapped = append(tapped, e) }

	ctx := context.Background()
	d.fanOutReply(ctx, contracts.Event{T: eventwire.EventReply, Text: "en cours"}, outbox.Frame{})
	d.fanOutReply(ctx, contracts.Event{T: eventwire.EventReply, Text: "fin", Done: true}, outbox.Frame{})

	sinks := map[string][]contracts.Event{
		"tap":    tapped,
		"Emit":   plain.events,
		"EmitTo": routed.events,
	}
	for name, got := range sinks {
		if len(got) != 2 {
			t.Fatalf("%s received %d replies, want 2 (one partial, one Done)", name, len(got))
		}
		for _, e := range got {
			if e.TurnID != "turn-7" {
				t.Fatalf("%s reply %q has TurnID %q, want turn-7", name, e.Text, e.TurnID)
			}
		}
	}
}

func TestTurnRecordsUserAndAssistantWithTheTurnID(t *testing.T) {
	var entries []state.TranscriptEntry
	gateway := &turnIDPlainGateway{}
	fromBridge := make(chan contracts.Event, 1)
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: gateway, Reader: gateway}}, make(chan contracts.Event, 8), fromBridge, io.Discard)
	d.sink.Transcript = func(e state.TranscriptEntry) { entries = append(entries, e) }
	d.terms = func() modelTerms { return modelTerms{rates: sonnetRates()} }
	ctx := context.Background()

	event, guard, ok := d.prepareTurn(ctx, queued{ev: contracts.Event{T: eventwire.EventInput, Text: "bonjour", TurnID: "turn-9"}})
	if !ok {
		t.Fatal("prepareTurn refused the turn")
	}
	fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "salut", Done: true, TokensIn: 1000, Tokens: 500}
	d.runTurn(ctx, event, guard)

	if len(entries) != 2 {
		t.Fatalf("transcript has %d entries, want user and assistant: %+v", len(entries), entries)
	}
	if entries[0].Role != "user" || entries[0].Text != "bonjour" || entries[0].TurnID != "turn-9" {
		t.Fatalf("user entry = %+v, want role user, text bonjour, TurnID turn-9", entries[0])
	}
	if entries[1].Role != "assistant" || entries[1].Text != "salut" || entries[1].TurnID != "turn-9" {
		t.Fatalf("assistant entry = %+v, want role assistant, text salut, TurnID turn-9", entries[1])
	}
	if want := (1000*3.0 + 500*15.0) / 1_000_000; entries[1].Cost != want {
		t.Fatalf("assistant cost = %v, want %v priced at the session's terms", entries[1].Cost, want)
	}
}
