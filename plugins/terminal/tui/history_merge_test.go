package tui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func replyEvent(text string) RoutedEvent {
	return turnReplyEvent("", text)
}

func turnReplyEvent(turnID, text string) RoutedEvent {
	return RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventReply, Text: text, TurnID: turnID}}
}

func chunkEvent(text string) RoutedEvent {
	return turnChunkEvent("", text)
}

func turnChunkEvent(turnID, text string) RoutedEvent {
	return RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventChunk, Text: text, TurnID: turnID}}
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

func newHistoryTab(t *testing.T, transcript []contracts.ScrollbackLine) (*model, *tab, tea.Cmd) {
	t.Helper()
	f := &fakeBackend{
		fe:         make(chan RoutedEvent, 1),
		sessions:   []contracts.SessionInfo{{Name: "s", ChannelID: "c"}},
		scrollback: map[string][]contracts.ScrollbackLine{"s": transcript},
	}
	m, load := startModel(f)
	return m, m.tabs["c"], load
}

func TestAReplyInTheQueueAndTheHistoryShowsOnce(t *testing.T) {
	m, tb, load := newHistoryTab(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question"},
		{Role: "assistant", Text: "answer"},
	})
	m.route(replyEvent("answer"))
	for _, msg := range historyMsgs(load) {
		m.Update(msg)
	}
	want := []string{"question", "answer"}
	if got := conversationTexts(tb); !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: the reply both queued and in the history must show once", got, want)
	}
}

func conversationAfterReplay(t *testing.T, transcript []contracts.ScrollbackLine, pending ...RoutedEvent) []string {
	t.Helper()
	m, tb, load := newHistoryTab(t, transcript)
	for _, event := range pending {
		m.route(event)
	}
	for _, msg := range historyMsgs(load) {
		m.Update(msg)
	}
	return conversationTexts(tb)
}

func TestAnIdenticalReplyOfANewTurnIsShown(t *testing.T) {
	got := conversationAfterReplay(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question", TurnID: "t1"},
		{Role: "assistant", Text: "ok", TurnID: "t1"},
	}, turnReplyEvent("t2", "ok"))
	want := []string{"question", "ok", "ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: a reply of a turn the history does not hold must show even when its text repeats a line", got, want)
	}
}

func TestAReplyOfAHeldTurnIsShownOnceWithItsChunks(t *testing.T) {
	got := conversationAfterReplay(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question", TurnID: "t1"},
		{Role: "assistant", Text: "ok", TurnID: "t1"},
	}, turnChunkEvent("t1", "o"), turnChunkEvent("t1", "k"), turnReplyEvent("t1", "ok"))
	want := []string{"question", "ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: the reply the history holds under its TurnID must show once, its chunks dropped with it", got, want)
	}
}

func TestAnotherReplyOfAPartlyHeldTurnIsShown(t *testing.T) {
	got := conversationAfterReplay(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question", TurnID: "t1"},
		{Role: "assistant", Text: "first", TurnID: "t1"},
	}, turnReplyEvent("t1", "second"))
	want := []string{"question", "first", "second"}
	if !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: a turn can hold several replies, so the history holding one must not hide another", got, want)
	}
}

func TestAReplyWithATurnIDIsNotDroppedByALineWithoutOne(t *testing.T) {
	got := conversationAfterReplay(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question"},
		{Role: "assistant", Text: "ok"},
	}, turnReplyEvent("t1", "ok"))
	want := []string{"question", "ok", "ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: text is compared only between lines that carry no TurnID, so a reply with one is never dropped by a legacy line", got, want)
	}
}

func TestAReplyWithoutATurnIDIsNotDroppedByALineWithOne(t *testing.T) {
	got := conversationAfterReplay(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question", TurnID: "t1"},
		{Role: "assistant", Text: "ok", TurnID: "t1"},
	}, replyEvent("ok"))
	want := []string{"question", "ok", "ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: the text fallback only reads the legacy lines at the end of the history, never a line carrying a TurnID", got, want)
	}
}

func TestChunksOfAnAlreadyShownReplyAreDroppedWithIt(t *testing.T) {
	m, tb, load := newHistoryTab(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question"},
		{Role: "assistant", Text: "answer"},
	})
	m.route(chunkEvent("an"))
	m.route(chunkEvent("swer"))
	m.route(replyEvent("answer"))
	for _, msg := range historyMsgs(load) {
		m.Update(msg)
	}
	want := []string{"question", "answer"}
	if got := conversationTexts(tb); !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q: the chunks of a reply the history already holds must go with it", got, want)
	}
}

func TestAQueueWithoutOverlapReplaysInFullAndInOrder(t *testing.T) {
	m, tb, load := newHistoryTab(t, []contracts.ScrollbackLine{
		{Role: "user", Text: "question"},
	})
	m.route(replyEvent("first"))
	m.route(replyEvent("second"))
	for _, msg := range historyMsgs(load) {
		m.Update(msg)
	}
	want := []string{"question", "first", "second"}
	if got := conversationTexts(tb); !slices.Equal(got, want) {
		t.Fatalf("conversation = %q, want %q replayed in full and in order", got, want)
	}
}
