package tui

import (
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func replyEvent(text string) RoutedEvent {
	return RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventReply, Text: text}}
}

func chunkEvent(text string) RoutedEvent {
	return RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventChunk, Text: text}}
}

func conversationTexts(tb *tab) []string {
	var texts []string
	for i := range tb.entries {
		if role := tb.entries[i].role; role == roleYou || role == roleAgent {
			texts = append(texts, tb.entries[i].text)
		}
	}
	return texts
}

func newHistoryTab(t *testing.T, transcript []contracts.ScrollbackLine) (*model, *tab) {
	t.Helper()
	f := &fakeBackend{
		fe:         make(chan RoutedEvent, 1),
		sessions:   []contracts.SessionInfo{{Name: "s", ChannelID: "c"}},
		scrollback: map[string][]contracts.ScrollbackLine{"s": transcript},
	}
	m := newModel(f)
	m.Update(sessionsMsg{seq: m.sessionsIssued, infos: f.sessions})
	return m, m.tabs["c"]
}

func TestAReplyInTheQueueAndTheHistoryShowsOnce(t *testing.T) {
	m, tb := newHistoryTab(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question"},
		{Role: "assistant", Text: "answer"},
	})
	historyCmd := m.route(replyEvent("answer"))
	for _, msg := range historyMsgs(historyCmd) {
		m.Update(msg)
	}
	want := []string{"question", "answer"}
	if got := conversationTexts(tb); !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: the reply both queued and in the history must show once", got, want)
	}
}

func TestAnIdenticalReplyAfterTheHistoryEndIsDroppedUntilTurnIDLands(t *testing.T) {
	m, tb := newHistoryTab(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question"},
		{Role: "assistant", Text: "ok"},
	})
	historyCmd := m.route(replyEvent("ok"))
	for _, msg := range historyMsgs(historyCmd) {
		m.Update(msg)
	}
	want := []string{"question", "ok"}
	if got := conversationTexts(tb); !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: without TurnID an identical reply after the history end is dropped as a duplicate", got, want)
	}
}

func TestChunksOfAnAlreadyShownReplyAreDroppedWithIt(t *testing.T) {
	m, tb := newHistoryTab(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question"},
		{Role: "assistant", Text: "answer"},
	})
	historyCmd := m.route(chunkEvent("an"))
	m.route(chunkEvent("swer"))
	m.route(replyEvent("answer"))
	for _, msg := range historyMsgs(historyCmd) {
		m.Update(msg)
	}
	want := []string{"question", "answer"}
	if got := conversationTexts(tb); !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: the chunks of a reply the history already holds must go with it", got, want)
	}
	for i := range tb.entries {
		if tb.entries[i].streaming {
			t.Fatalf("entry %d still streams after the history: the chunks of a reply the history already holds must not stay open", i)
		}
	}
}

func TestAQueueWithoutOverlapReplaysInFullAndInOrder(t *testing.T) {
	m, tb := newHistoryTab(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question"},
	})
	historyCmd := m.route(replyEvent("first"))
	m.route(replyEvent("second"))
	for _, msg := range historyMsgs(historyCmd) {
		m.Update(msg)
	}
	want := []string{"question", "first", "second"}
	if got := conversationTexts(tb); !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q replayed in full and in order", got, want)
	}
}
