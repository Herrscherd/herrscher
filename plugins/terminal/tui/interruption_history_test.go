package tui

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func interruptedReplyEvent(turnID, text string) RoutedEvent {
	return RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventReply, Text: text, TurnID: turnID, Done: true, Outcome: contracts.TurnInterrupted}}
}

func interruptionLine(turnID string) contracts.ScrollbackLine {
	return contracts.ScrollbackLine{Role: "notice", TurnID: turnID, Notice: &contracts.Notice{Code: contracts.CodeTurnInterrupted}}
}

func TestAnInterruptedTurnWithoutOutputIsNotNoticedTwiceAfterReload(t *testing.T) {
	history := []contracts.ScrollbackLine{
		{Role: "user", Text: "question", TurnID: "turn-1"},
		interruptionLine("turn-1"),
	}
	pending := []RoutedEvent{interruptedReplyEvent("turn-1", "")}
	if got := unseenPending(history, pending); len(got) != 0 {
		t.Fatalf("unseenPending = %+v, want the live interruption dropped over its history line", got)
	}
}

func TestAnInterruptedTurnWithPartialOutputIsNotShownTwiceAfterReload(t *testing.T) {
	history := []contracts.ScrollbackLine{
		{Role: "user", Text: "question", TurnID: "turn-1"},
		{Role: "assistant", Text: "half a thought", TurnID: "turn-1"},
		interruptionLine("turn-1"),
	}
	pending := []RoutedEvent{interruptedReplyEvent("turn-1", "half a thought")}
	if got := unseenPending(history, pending); len(got) != 0 {
		t.Fatalf("unseenPending = %+v, want the partial reply and its notice dropped over the history", got)
	}
}

func TestAnInterruptedTurnMissingFromHistoryIsStillShown(t *testing.T) {
	history := []contracts.ScrollbackLine{{Role: "user", Text: "question", TurnID: "turn-1"}}
	pending := []RoutedEvent{interruptedReplyEvent("turn-1", "")}
	if got := unseenPending(history, pending); len(got) != 1 {
		t.Fatalf("unseenPending = %+v, want the live interruption kept when history has no notice for it", got)
	}
}
