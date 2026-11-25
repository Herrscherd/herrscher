package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func timelineToolStart(id, tool, detail string, tokens int) contracts.Event {
	call := contracts.ToolCall{ID: id, Tool: tool, Detail: detail, State: contracts.ToolCallStarted, At: time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)}
	return contracts.Event{T: eventwire.EventStatus, Text: tool + " " + detail, ToolCall: &call, Tokens: tokens}
}

func timelineToolDone(id string, tokens int) contracts.Event {
	call := contracts.ToolCall{ID: id, State: contracts.ToolCallSucceeded, At: time.Date(2026, 11, 22, 10, 0, 1, 0, time.UTC)}
	return contracts.Event{T: eventwire.EventToolDone, ToolCall: &call, Tokens: tokens}
}

func TestEachTurnStartsItsToolTimelineFresh(t *testing.T) {
	toBridge := make(chan contracts.Event, 4)
	fromBridge := make(chan contracts.Event, 16)
	d, _ := identifiedTestDriver(toBridge, fromBridge)
	var replies []state.TranscriptEntry
	d.sink.Transcript = func(entry state.TranscriptEntry) error {
		if entry.Role == "assistant" {
			replies = append(replies, entry)
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.pump(ctx)
	go func() {
		<-toBridge
		fromBridge <- timelineToolStart("a", "read", "env.go", 4)
		fromBridge <- timelineToolDone("a", 9)
		fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "first", Done: true, Tokens: 9}
		<-toBridge
		fromBridge <- timelineToolStart("a", "grep", "TODO", 12)
		fromBridge <- timelineToolDone("a", 20)
		fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "second", Done: true, Tokens: 20}
	}()

	if reply, ok := d.SeedAndWaitWithTurnID(ctx, "first task", "turn-1"); !ok || reply != "first" {
		t.Fatalf("first turn = %q, %v, want first, true", reply, ok)
	}
	if reply, ok := d.SeedAndWaitWithTurnID(ctx, "second task", "turn-2"); !ok || reply != "second" {
		t.Fatalf("second turn = %q, %v, want second, true", reply, ok)
	}
	if len(replies) != 2 {
		t.Fatalf("assistant entries = %d, want 2", len(replies))
	}
	first, second := replies[0].Tools, replies[1].Tools
	if len(first) != 1 || first[0].Tool != "read" || first[0].Detail != "env.go" || first[0].State != contracts.ToolCallSucceeded || first[0].Tokens != 0 {
		t.Fatalf("first turn tools = %+v, want one finished read of env.go with zero tokens", first)
	}
	if len(second) != 1 || second[0].ID != "a" || second[0].Tool != "grep" || second[0].Detail != "TODO" || second[0].State != contracts.ToolCallSucceeded || second[0].Tokens != 0 {
		t.Fatalf("second turn tools = %+v, want one new finished grep of TODO with zero tokens, not the first turn's calls", second)
	}
}
