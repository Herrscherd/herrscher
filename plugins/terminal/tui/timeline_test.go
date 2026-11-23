package tui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func timelineStart() time.Time {
	return time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
}

func routeToolStart(m *model, id string) {
	call := contracts.ToolCall{ID: id, Tool: "read", Detail: "env.go", State: contracts.ToolCallStarted, At: timelineStart()}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventStatus, Text: "read env.go", ToolCall: &call}})
}

func routeToolDone(m *model, id string, elapsed time.Duration) {
	call := contracts.ToolCall{ID: id, State: contracts.ToolCallSucceeded, At: timelineStart().Add(elapsed)}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventToolDone, ToolCall: &call}})
}

func routeReply(m *model) {
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventReply, Text: "ok", Done: true}})
}

func TestTurnEndShowsItsFinishedCallBeforeTheReply(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	routeToolDone(m, "a", 1500*time.Millisecond)
	routeReply(m)

	transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	want := "✓ read env.go · " + clitext.FormatDuration(i18n.English, 1500*time.Millisecond) + " · tokens: 0"
	if !strings.Contains(transcript, want) {
		t.Fatalf("transcript\n%s\nwant the outcome line %q", transcript, want)
	}
	if strings.Index(transcript, want) > strings.LastIndex(transcript, "ok") {
		t.Fatalf("the outcome line must come before the reply text:\n%s", transcript)
	}
}

func TestTurnEndShowsAnUnfinishedCall(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	routeReply(m)

	transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	if !strings.Contains(transcript, "… read env.go · unfinished") {
		t.Fatalf("transcript\n%s\nwant the open call listed as unfinished", transcript)
	}
}

func TestTurnEndNoticesTheBoundOnce(t *testing.T) {
	m := newTestModel()
	for i := range timeline.MaxCalls + 2 {
		routeToolStart(m, strconv.Itoa(i))
	}
	routeReply(m)

	transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	if n := strings.Count(transcript, "only the first 4096 tool calls"); n != 1 {
		t.Fatalf("bound notice shown %d times, want once", n)
	}
}

func TestTurnEndHasNoTimelineWithoutCalls(t *testing.T) {
	m := newTestModel()
	routeReply(m)

	for _, text := range entryTexts(m.tabs["c"]) {
		if strings.Contains(text, "tokens") || strings.Contains(text, "unfinished") {
			t.Fatalf("entry %q must not come from a timeline when the turn had no calls", text)
		}
	}
}

func TestAbandonedTurnListsItsOpenCallAsUnfinished(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventAbandoned}})

	transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	if !strings.Contains(transcript, "… read env.go · unfinished") {
		t.Fatalf("transcript\n%s\nwant the open call listed as unfinished before the abandon notice", transcript)
	}
	routeToolDone(m, "a", time.Second)
	routeReply(m)
	if after := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n")); strings.Contains(after, "✓ read env.go") {
		t.Fatalf("transcript\n%s\nwant the abandoned call not to reappear in the next turn", after)
	}
}

func TestExpiredGapListsItsOpenCallAsUnfinished(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	tb := m.tabs["c"]
	m.renderInto(tb, contracts.Event{T: eventwire.EventGap, Value: eventwire.GapExpired})

	transcript := ansi.Strip(strings.Join(entryTexts(tb), "\n"))
	if !strings.Contains(transcript, "… read env.go · unfinished") {
		t.Fatalf("transcript\n%s\nwant the call open before the gap listed as unfinished", transcript)
	}
}
