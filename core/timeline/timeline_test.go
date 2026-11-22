package timeline

import (
	"errors"
	"strconv"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func t0() time.Time {
	return time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
}

func startEvent(id string, at time.Duration, tokens int) contracts.Event {
	return contracts.Event{T: eventwire.EventStatus, Tokens: tokens, ToolCall: &contracts.ToolCall{ID: id, Tool: "read", Detail: id, State: contracts.ToolCallStarted, At: t0().Add(at)}}
}

func doneEvent(id string, state contracts.ToolCallState, at time.Duration, tokens int) contracts.Event {
	return contracts.Event{T: eventwire.EventToolDone, Tokens: tokens, ToolCall: &contracts.ToolCall{ID: id, State: state, At: t0().Add(at)}}
}

func chunkEvent(tokens int) contracts.Event {
	return contracts.Event{T: eventwire.EventChunk, Text: "x", Tokens: tokens}
}

func resetEvent() contracts.Event {
	return contracts.Event{T: eventwire.EventReset}
}

func feed(t *testing.T, r *Recorder, events ...contracts.Event) {
	t.Helper()
	for i := range events {
		if _, _, err := r.Apply(events[i]); err != nil {
			t.Fatalf("Apply(%s) = %v", events[i].T, err)
		}
	}
}

func TestFirstCallHasNoTokensAndCloses(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", 0, 40), doneEvent("a", contracts.ToolCallSucceeded, 1500*time.Millisecond, 90))
	calls := r.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	call := calls[0]
	if call.Tokens != 0 {
		t.Fatalf("first call tokens = %d, want 0", call.Tokens)
	}
	if !call.Finished() || call.State != contracts.ToolCallSucceeded {
		t.Fatalf("call = %+v, want succeeded and finished", call)
	}
	if call.DurationMs == nil || *call.DurationMs != 1500 {
		t.Fatalf("duration = %v, want 1500 ms", call.DurationMs)
	}
}

func TestFailedCallKeepsItsError(t *testing.T) {
	var r Recorder
	failed := doneEvent("a", contracts.ToolCallFailed, time.Second, 0)
	failed.ToolCall.Error = "exit 2"
	feed(t, &r, startEvent("a", 0, 0), failed)
	call := r.Calls()[0]
	if call.State != contracts.ToolCallFailed || call.Error != "exit 2" {
		t.Fatalf("call = %+v, want failed with its error", call)
	}
}

func TestUnknownDoneIsIgnoredAndMovesNoTokens(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", 0, 10), doneEvent("ghost", contracts.ToolCallSucceeded, time.Second, 999), startEvent("b", time.Second, 25))
	calls := r.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if calls[1].Tokens != 25-10 {
		t.Fatalf("second call tokens = %d, want %d: an unknown done must not move the mark", calls[1].Tokens, 25-10)
	}
	if calls[0].Finished() {
		t.Fatal("call a must stay open: the done named another ID")
	}
}

func TestSecondStartOfKnownIDOpensNothing(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", 0, 5), startEvent("a", time.Second, 50))
	if n := len(r.Calls()); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
}

func TestSecondStartDoesNotMoveTokens(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", 0, 5), startEvent("a", time.Second, 500), startEvent("b", 2*time.Second, 20))
	if got := r.Calls()[1].Tokens; got != 15 {
		t.Fatalf("second call tokens = %d, want 15: a second start is not a transition", got)
	}
}

func TestTokensCountFromPreviousTransition(t *testing.T) {
	var r Recorder
	feed(t, &r,
		startEvent("a", 0, 10),
		doneEvent("a", contracts.ToolCallSucceeded, time.Second, 30),
		startEvent("b", 2*time.Second, 70),
		chunkEvent(90),
		startEvent("c", 3*time.Second, 100),
	)
	want := []int{0, 70 - 30, 100 - 70}
	for i, call := range r.Calls() {
		if call.Tokens != want[i] {
			t.Fatalf("call %d tokens = %d, want %d", i, call.Tokens, want[i])
		}
	}
}

func TestResetKeepsOpenCallsUnfinished(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", 0, 0), resetEvent(), doneEvent("a", contracts.ToolCallSucceeded, time.Second, 4))
	calls := r.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Finished() || calls[0].DurationMs != nil {
		t.Fatalf("call = %+v, want unfinished with no duration", calls[0])
	}
}

func TestIDReusedAfterResetOpensANewCall(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", 0, 0), resetEvent(), startEvent("a", time.Second, 0), doneEvent("a", contracts.ToolCallSucceeded, 2*time.Second, 0))
	calls := r.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2: an ID reused after a reset is a new call", len(calls))
	}
	if calls[0].Finished() || !calls[1].Finished() {
		t.Fatalf("calls = %+v, want the first unfinished and the second finished", calls)
	}
}

func TestResetLeavesCumulativeCountUnchanged(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", 0, 40), resetEvent(), startEvent("b", time.Second, 60))
	if got := r.Calls()[1].Tokens; got != 20 {
		t.Fatalf("call after reset tokens = %d, want 20: the reset must not move the count", got)
	}
}

func TestResultLowerThanMarkGivesNegativeTokens(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", 0, 100), chunkEvent(300), startEvent("b", time.Second, 50))
	if got := r.Calls()[1].Tokens; got != 50-100 {
		t.Fatalf("call after a lower result = %d, want %d: the count follows the result as sent", got, 50-100)
	}
}

func TestUnfinishedCallAtEndOfTurnHasNoOutcome(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", 0, 0), startEvent("b", time.Second, 3), doneEvent("b", contracts.ToolCallSucceeded, 2*time.Second, 3))
	calls := r.Calls()
	if calls[0].Finished() || calls[0].State != contracts.ToolCallStarted || calls[0].DurationMs != nil {
		t.Fatalf("call a = %+v, want started, unfinished, no duration", calls[0])
	}
	if !calls[1].Finished() {
		t.Fatal("call b must be finished")
	}
}

func TestBoundKeepsMaxCallsAndReportsFullOnce(t *testing.T) {
	var r Recorder
	fullReports := 0
	for i := range MaxCalls + 3 {
		id := strconv.Itoa(i)
		if _, _, err := r.Apply(startEvent(id, time.Duration(i), i)); err != nil {
			if !errors.Is(err, ErrFull) {
				t.Fatalf("Apply = %v, want ErrFull", err)
			}
			fullReports++
		}
	}
	if n := len(r.Calls()); n != MaxCalls {
		t.Fatalf("calls = %d, want %d", n, MaxCalls)
	}
	if fullReports != 1 {
		t.Fatalf("ErrFull reported %d times, want once", fullReports)
	}
}

func TestBoundDoesNotCountUnknownDones(t *testing.T) {
	var r Recorder
	for i := range MaxCalls {
		feed(t, &r, startEvent(strconv.Itoa(i), 0, 0))
	}
	feed(t, &r, doneEvent("ghost", contracts.ToolCallSucceeded, 0, 0))
	if n := len(r.Calls()); n != MaxCalls {
		t.Fatalf("calls = %d, want %d", n, MaxCalls)
	}
}
