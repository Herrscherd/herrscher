package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func watchStart() time.Time {
	return time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
}

func toolStart(seq uint64, id string) host.DaemonEvent {
	return watchEvent(seq, "a", contracts.Event{T: eventwire.EventStatus, Text: "read env.go", ToolCall: &contracts.ToolCall{ID: id, Tool: "read", Detail: "env.go", State: contracts.ToolCallStarted, At: watchStart()}})
}

func toolDone(seq uint64, id string, elapsed time.Duration, state contracts.ToolCallState) host.DaemonEvent {
	return watchEvent(seq, "a", contracts.Event{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{ID: id, State: state, At: watchStart().Add(elapsed)}})
}

func replyDone(seq uint64) host.DaemonEvent {
	return watchEvent(seq, "a", contracts.Event{T: eventwire.EventReply, Text: "ok", Done: true})
}

func sessionClosed(seq uint64) host.DaemonEvent {
	return watchEvent(seq, "a", contracts.Event{T: eventwire.EventClosed})
}

func TestFollowSessionRendersToolOutcomeWithoutADuplicateStatus(t *testing.T) {
	out, err := followed(t, false, toolStart(1, "a"), toolDone(2, "a", 1500*time.Millisecond, contracts.ToolCallSucceeded), replyDone(3), sessionClosed(4))
	if err != nil {
		t.Fatal(err)
	}
	line := "  ✓ read env.go · " + clitext.FormatDuration(i18n.English, 1500*time.Millisecond) + " · tokens: 0\n"
	if !strings.Contains(out, line) {
		t.Fatalf("output\n%q\nwant the outcome line\n%q", out, line)
	}
	if n := strings.Count(out, "read env.go"); n != 2 {
		t.Fatalf("\"read env.go\" appears %d times, want 2: the status and the outcome, no third line", n)
	}
}

func TestFollowSessionRendersAFailedCallWithItsError(t *testing.T) {
	failed := toolDone(2, "a", time.Second, contracts.ToolCallFailed)
	failed.ToolCall.Error = "exit 2"
	out, err := followed(t, false, toolStart(1, "a"), failed, replyDone(3), sessionClosed(4))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "  ✗ read env.go · "+clitext.FormatDuration(i18n.English, time.Second)+" · exit 2\n") {
		t.Fatalf("output %q does not show the failed call with its error", out)
	}
}

func TestFollowSessionListsUnfinishedCallsAtTheReply(t *testing.T) {
	out, err := followed(t, false, toolStart(1, "a"), replyDone(2), sessionClosed(3))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "  … read env.go · unfinished\n") {
		t.Fatalf("output %q does not list the open call as unfinished", out)
	}
}

func TestFollowSessionKeepsTheBoundNoticeOnce(t *testing.T) {
	events := make([]host.DaemonEvent, 0, timeline.MaxCalls+4)
	for i := range timeline.MaxCalls + 2 {
		events = append(events, toolStart(uint64(i+1), strconv.Itoa(i)))
	}
	events = append(events, replyDone(uint64(timeline.MaxCalls+3)), sessionClosed(uint64(timeline.MaxCalls+4)))
	out, err := followed(t, false, events...)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "only the first 4096 tool calls"); n != 1 {
		t.Fatalf("bound notice printed %d times, want once", n)
	}
}

func TestFollowSessionJSONCarriesTheToolCall(t *testing.T) {
	out, err := followed(t, true, toolStart(1, "a"), sessionClosed(2))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"tool_call":{"id":"a"`, `"state":"started"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json output %q lacks %s", out, want)
		}
	}
}

func TestFollowSessionTextMatchesEnglishCatalogueLine(t *testing.T) {
	want := fmt.Sprintf("  ✓ read env.go · %s · tokens: 0", clitext.FormatDuration(i18n.English, 2*time.Second))
	out, err := followed(t, false, toolStart(1, "a"), toolDone(2, "a", 2*time.Second, contracts.ToolCallSucceeded), replyDone(3), sessionClosed(4))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, want+"\n") {
		t.Fatalf("output %q lacks %q", out, want)
	}
}

func TestFollowSessionClosesAnAbandonedTurnsOpenCallAsUnfinished(t *testing.T) {
	out, err := followed(t, false, toolStart(1, "a"), watchEvent(2, "a", contracts.Event{T: eventwire.EventAbandoned}), toolDone(3, "a", time.Second, contracts.ToolCallSucceeded), replyDone(4), sessionClosed(5))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "  … read env.go · unfinished\n") {
		t.Fatalf("output %q does not close the abandoned turn's open call as unfinished", out)
	}
	if strings.Contains(out, "✓ read env.go") {
		t.Fatalf("output %q lets the abandoned turn's call finish in the next turn", out)
	}
}

func TestFollowSessionClosesOpenCallsOnAnExpiredGap(t *testing.T) {
	out, err := followed(t, false, toolStart(1, "a"), watchEvent(2, "a", contracts.Event{T: eventwire.EventGap, Value: eventwire.GapExpired}), toolDone(3, "a", time.Second, contracts.ToolCallSucceeded), replyDone(4), sessionClosed(5))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "  … read env.go · unfinished\n") {
		t.Fatalf("output %q does not close the call open before the gap as unfinished", out)
	}
	if strings.Contains(out, "✓ read env.go") {
		t.Fatalf("output %q lets a call lost in the gap finish later", out)
	}
}
