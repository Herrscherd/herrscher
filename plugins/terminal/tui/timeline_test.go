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

func routeToolDone(m *model, elapsed time.Duration) {
	routeCallDone(m, "a", elapsed)
}

func routeCallDone(m *model, id string, elapsed time.Duration) {
	call := contracts.ToolCall{ID: id, State: contracts.ToolCallSucceeded, At: timelineStart().Add(elapsed)}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventToolDone, ToolCall: &call}})
}

func routeReply(m *model) {
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventReply, Text: "ok", Done: true}})
}

func TestTurnEndShowsItsFinishedCallBeforeTheReply(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	routeToolDone(m, 1500*time.Millisecond)
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
	routeToolDone(m, time.Second)
	routeReply(m)
	if after := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n")); strings.Contains(after, "✓ read env.go") {
		t.Fatalf("transcript\n%s\nwant the abandoned call not to reappear in the next turn", after)
	}
}

func TestExpiredGapLeavesItsOpenCallOpenUntilTheTurnEnds(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventGap, Value: eventwire.GapExpired}})
	if transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n")); strings.Contains(transcript, "unfinished") {
		t.Fatalf("transcript\n%s\nlists the open call at the gap, want it at the turn end", transcript)
	}
	routeReply(m)
	transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	if n := strings.Count(transcript, "… read env.go · unfinished"); n != 1 {
		t.Fatalf("transcript\n%s\nlists the open call as unfinished %d times at the turn end, want once", transcript, n)
	}
}

func TestGapReplayFinishesTheCallOnce(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventGap, Value: eventwire.GapExpired}})
	routeToolStart(m, "a")
	routeToolDone(m, time.Second)
	routeReply(m)

	transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	if n := strings.Count(transcript, "✓ read env.go"); n != 1 {
		t.Fatalf("transcript\n%s\nfinishes the call %d times, want its real done once", transcript, n)
	}
	if strings.Contains(transcript, "unfinished") {
		t.Fatalf("transcript\n%s\nlists a call its real done finished as unfinished", transcript)
	}
}

func TestNextTurnShowsAReusedIDAgain(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	routeToolDone(m, time.Second)
	routeReply(m)
	routeToolStart(m, "a")
	routeToolDone(m, time.Second)
	routeReply(m)

	transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	if n := strings.Count(transcript, "✓ read env.go"); n != 2 {
		t.Fatalf("transcript\n%s\nshows the reused call %d times, want once per turn", transcript, n)
	}
}

func TestConcurrentCallsFinishedAroundAGapEachShowOnce(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	routeToolStart(m, "b")
	routeCallDone(m, "b", time.Second)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventGap, Value: eventwire.GapExpired}})
	atGap := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	if strings.Contains(atGap, "unfinished") {
		t.Fatalf("transcript\n%s\nlists the call still open beside a finished one at the gap, want it left open", atGap)
	}
	routeCallDone(m, "a", 2*time.Second)
	routeReply(m)

	transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	if n := strings.Count(transcript, "✓ read env.go"); n != 2 {
		t.Fatalf("transcript\n%s\nshows %d finished calls, want each of the two once", transcript, n)
	}
	if strings.Contains(transcript, "unfinished") {
		t.Fatalf("transcript\n%s\nlists a call its done finished as unfinished", transcript)
	}
}

func TestReplayedStartAfterAGapAddsNoLine(t *testing.T) {
	m := newTestModel()
	routeToolStart(m, "a")
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventGap, Value: eventwire.GapExpired}})
	before := len(m.tabs["c"].entries)
	routeToolStart(m, "a")
	if after := len(m.tabs["c"].entries); after != before {
		t.Fatalf("transcript\n%s\ngained %d lines from the replayed start, want none", ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n")), after-before)
	}
}
