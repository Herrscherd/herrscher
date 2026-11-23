package timeline

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func callAt() time.Time {
	return time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
}

func startEvent(id, detail string) contracts.Event {
	return contracts.Event{T: eventwire.EventStatus, ToolCall: &contracts.ToolCall{ID: id, Tool: "read", Detail: detail, State: contracts.ToolCallStarted, At: callAt()}}
}

func failedEvent(errText string) contracts.Event {
	return contracts.Event{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{ID: "a", State: contracts.ToolCallFailed, Error: errText, At: callAt().Add(time.Second)}}
}

func TestDetailAndErrorAreCutAtTheBound(t *testing.T) {
	var r Recorder
	long := strings.Repeat("a", MaxFieldBytes+100)
	started, _, err := r.Apply(startEvent("a", long))
	if err != nil {
		t.Fatal(err)
	}
	if len(started.Detail) != MaxFieldBytes {
		t.Fatalf("detail is %d bytes, want %d", len(started.Detail), MaxFieldBytes)
	}
	finished, _, err := r.Apply(failedEvent(long))
	if err != nil {
		t.Fatal(err)
	}
	if len(finished.Error) != MaxFieldBytes {
		t.Fatalf("error is %d bytes, want %d", len(finished.Error), MaxFieldBytes)
	}
}

func TestShortDetailAndErrorAreKeptWhole(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(startEvent("a", "env.go")); err != nil {
		t.Fatal(err)
	}
	finished, _, err := r.Apply(failedEvent("exit 2"))
	if err != nil {
		t.Fatal(err)
	}
	if finished.Detail != "env.go" || finished.Error != "exit 2" {
		t.Fatalf("call = %+v, want its detail and error unchanged", finished)
	}
}

func TestBoundNeverSplitsACharacter(t *testing.T) {
	var r Recorder
	text := strings.Repeat("a", MaxFieldBytes-1) + "é"
	started, _, err := r.Apply(startEvent("a", text))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("a", MaxFieldBytes-1); started.Detail != want {
		t.Fatalf("detail is %d bytes, want the %d bytes before the split character", len(started.Detail), len(want))
	}
	if !utf8.ValidString(started.Detail) {
		t.Fatalf("detail %q is not valid UTF-8", started.Detail)
	}
}

func tokenStart(id string, tokens int) contracts.Event {
	return contracts.Event{T: eventwire.EventStatus, Tokens: tokens, ToolCall: &contracts.ToolCall{ID: id, Tool: "read", State: contracts.ToolCallStarted, At: callAt()}}
}

func tokenDone(id string, tokens int) contracts.Event {
	return contracts.Event{T: eventwire.EventToolDone, Tokens: tokens, ToolCall: &contracts.ToolCall{ID: id, State: contracts.ToolCallSucceeded, At: callAt().Add(time.Second)}}
}

func chunkEvent(tokens int) contracts.Event {
	return contracts.Event{T: eventwire.EventChunk, Text: "x", Tokens: tokens}
}

func replyEvent(tokens int) contracts.Event {
	return contracts.Event{T: eventwire.EventReply, Done: true, Tokens: tokens}
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

func TestSecondStartOfKnownIDOpensNothing(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(startEvent("a", "env.go")); err != nil {
		t.Fatal(err)
	}
	_, touched, err := r.Apply(startEvent("a", "env.go"))
	if err != nil {
		t.Fatal(err)
	}
	if touched {
		t.Fatal("a second start of a known ID touched a call, want nothing opened")
	}
	if n := len(r.Calls()); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
}

func TestResetKeepsOpenCallsUnfinished(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(startEvent("a", "env.go")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Apply(contracts.Event{T: eventwire.EventReset}); err != nil {
		t.Fatal(err)
	}
	_, touched, err := r.Apply(failedEvent("exit 2"))
	if err != nil {
		t.Fatal(err)
	}
	if touched {
		t.Fatal("a tool_done after a reset closed the call it started before, want it ignored")
	}
	calls := r.Calls()
	if len(calls) != 1 || calls[0].Finished() {
		t.Fatalf("calls = %+v, want the one call still unfinished", calls)
	}
}

func TestReusedIDAfterResetOpensANewCall(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(startEvent("a", "env.go")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Apply(contracts.Event{T: eventwire.EventReset}); err != nil {
		t.Fatal(err)
	}
	_, touched, err := r.Apply(startEvent("a", "env.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !touched || len(r.Calls()) != 2 {
		t.Fatalf("calls = %d after a reused ID, want a new second call", len(r.Calls()))
	}
}

func TestTokensCountFromPreviousTransition(t *testing.T) {
	var r Recorder
	first, _, err := r.Apply(tokenStart("a", 10))
	if err != nil {
		t.Fatal(err)
	}
	if first.Tokens != 0 {
		t.Fatalf("first call tokens = %d, want 0", first.Tokens)
	}
	if _, _, err = r.Apply(tokenDone("a", 18)); err != nil {
		t.Fatal(err)
	}
	second, _, err := r.Apply(tokenStart("b", 25))
	if err != nil {
		t.Fatal(err)
	}
	if second.Tokens != 7 {
		t.Fatalf("second call tokens = %d, want 7, counted from the previous tool_done at 18", second.Tokens)
	}
}

func TestBoundKeepsMaxCallsAndReportsFullOnce(t *testing.T) {
	var r Recorder
	for i := range MaxCalls {
		if _, _, err := r.Apply(startEvent(strconv.Itoa(i), "x")); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	_, _, err := r.Apply(startEvent("overflow-1", "x"))
	if !errors.Is(err, ErrFull) {
		t.Fatalf("first call past the bound: err = %v, want ErrFull", err)
	}
	for _, id := range []string{"overflow-2", "overflow-3"} {
		if _, _, err := r.Apply(startEvent(id, "x")); err != nil {
			t.Fatalf("call %s past the bound: err = %v, want ErrFull reported once", id, err)
		}
	}
	if n := len(r.Calls()); n != MaxCalls {
		t.Fatalf("calls = %d, want %d kept", n, MaxCalls)
	}
}

func TestUnknownDoneIsIgnoredAndMovesNoTokens(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(tokenStart("a", 10)); err != nil {
		t.Fatal(err)
	}
	_, touched, err := r.Apply(tokenDone("zz", 99))
	if err != nil {
		t.Fatal(err)
	}
	if touched {
		t.Fatal("a tool_done for an unknown ID touched a call")
	}
	next, _, err := r.Apply(tokenStart("b", 20))
	if err != nil {
		t.Fatal(err)
	}
	if next.Tokens != 10 {
		t.Fatalf("tokens = %d, want 10: the unknown tool_done at 99 must not move the mark", next.Tokens)
	}
}

func TestReplayedStartAfterAGapOpensNothing(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(startEvent("a", "env.go")); err != nil {
		t.Fatal(err)
	}
	if _, touched, err := r.Apply(startEvent("a", "env.go")); err != nil || touched {
		t.Fatalf("replayed start: touched %v, err %v, want nothing opened", touched, err)
	}
	if n := len(r.Calls()); n != 1 || r.Calls()[0].Finished() {
		t.Fatalf("calls = %+v, want the one call still open", r.Calls())
	}
}

func TestDoneAfterAGapFinishesTheOpenCall(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(startEvent("a", "env.go")); err != nil {
		t.Fatal(err)
	}
	if got := r.Drain(); len(got) != 0 {
		t.Fatalf("Drain before the done = %+v, want the open call held back", got)
	}
	if _, touched, err := r.Apply(failedEvent("exit 2")); err != nil || !touched {
		t.Fatalf("done after the gap: touched %v, err %v, want the open call closed", touched, err)
	}
	if got := r.Drain(); len(got) != 1 || !got[0].Finished() {
		t.Fatalf("Drain after the done = %+v, want the finished call", got)
	}
}

func TestRepeatedDoneOfAFinishedCallIsIgnored(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(startEvent("a", "env.go")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Apply(failedEvent("exit 2")); err != nil {
		t.Fatal(err)
	}
	if _, touched, err := r.Apply(failedEvent("exit 2")); err != nil || touched {
		t.Fatalf("second done: touched %v, err %v, want nothing closed", touched, err)
	}
	if got := r.Drain(); len(got) != 1 {
		t.Fatalf("Drain = %+v, want the call once", got)
	}
}

func TestDrainHandsFinishedCallsOnceAndEndHandsTheRest(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(startEvent("a", "env.go")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Apply(failedEvent("exit 2")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Apply(startEvent("b", "main.go")); err != nil {
		t.Fatal(err)
	}
	if got := r.Drain(); len(got) != 1 || got[0].ID != "a" || !got[0].Finished() {
		t.Fatalf("Drain = %+v, want only the finished call a", got)
	}
	if got := r.Drain(); len(got) != 0 {
		t.Fatalf("second Drain = %+v, want nothing again", got)
	}
	got := r.End()
	if len(got) != 1 || got[0].ID != "b" || got[0].Finished() {
		t.Fatalf("End = %+v, want the open call b, unfinished", got)
	}
	if got := r.End(); len(got) != 0 {
		t.Fatalf("second End = %+v, want nothing again", got)
	}
}

func TestBoundFieldReturnsTheCutAsItsOwnString(t *testing.T) {
	long := strings.Repeat("a", MaxFieldBytes+100)
	cut := BoundField(long)
	if len(cut) != MaxFieldBytes {
		t.Fatalf("cut is %d bytes, want %d", len(cut), MaxFieldBytes)
	}
}

func TestDoneWithoutACountLeavesTheRunningCountAlone(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 10), chunkEvent(30), tokenDone("a", 0))
	next, _, err := r.Apply(tokenStart("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	if next.Tokens != 10 {
		t.Fatalf("tokens = %d, want 10: a done reporting no count must not move the count back to the mark", next.Tokens)
	}
}

func TestChunkCountIsKeptWhenTheNextStartReportsNone(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 10), chunkEvent(40), tokenStart("b", 0))
	if got := r.Calls()[1].Tokens; got != 30 {
		t.Fatalf("tokens = %d, want 30: the chunk at 40 is counted from the mark at 10 even though the next start reports no count", got)
	}
}

func TestLowerCountFromAReplyIsTakenAsSent(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 100), chunkEvent(150), replyEvent(120), tokenStart("b", 120))
	if got := r.Calls()[1].Tokens; got != 20 {
		t.Fatalf("tokens = %d, want 20: the reply at 120 replaces the running count of 150, as the bridge does", got)
	}
}

func TestLowerCountFromAStartIsTakenAsSent(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 100), chunkEvent(150), tokenStart("b", 120))
	if got := r.Calls()[1].Tokens; got != 20 {
		t.Fatalf("tokens = %d, want 20: the start at 120 replaces the running count of 150", got)
	}
}

func TestCountBelowTheMarkGivesNegativeTokens(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 100), tokenStart("b", 80), tokenStart("c", 95))
	calls := r.Calls()
	if calls[1].Tokens != -20 {
		t.Fatalf("call b tokens = %d, want -20: the count follows the value as sent", calls[1].Tokens)
	}
	if calls[2].Tokens != 15 {
		t.Fatalf("call c tokens = %d, want 15: counted from the lower count of 80", calls[2].Tokens)
	}
}

func TestResultLowerThanMarkGivesNegativeTokens(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 100), chunkEvent(300), tokenStart("b", 50))
	if got := r.Calls()[1].Tokens; got != 50-100 {
		t.Fatalf("call after a lower result = %d, want %d: the count follows the result as sent", got, 50-100)
	}
}

func TestFirstCallHasNoTokensAndCloses(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 40), tokenDone("a", 90))
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
	if call.DurationMs == nil || *call.DurationMs != 1000 {
		t.Fatalf("duration = %v, want 1000 ms", call.DurationMs)
	}
}

func TestFailedCallKeepsItsError(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", "env.go"), failedEvent("exit 2"))
	call := r.Calls()[0]
	if call.State != contracts.ToolCallFailed || call.Error != "exit 2" {
		t.Fatalf("call = %+v, want failed with its error", call)
	}
}

func TestUnfinishedCallAtEndOfTurnHasNoOutcome(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 0), tokenStart("b", 3), tokenDone("b", 3))
	calls := r.Calls()
	if calls[0].Finished() || calls[0].State != contracts.ToolCallStarted || calls[0].DurationMs != nil {
		t.Fatalf("call a = %+v, want started, unfinished, no duration", calls[0])
	}
	if !calls[1].Finished() {
		t.Fatal("call b must be finished")
	}
}

func TestSecondStartDoesNotMoveTokens(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 5), tokenStart("a", 500), tokenStart("b", 20))
	if got := r.Calls()[1].Tokens; got != 15 {
		t.Fatalf("second call tokens = %d, want 15: a second start is not a transition", got)
	}
}

func TestResetLeavesCumulativeCountUnchanged(t *testing.T) {
	var r Recorder
	feed(t, &r, tokenStart("a", 40), resetEvent(), tokenStart("b", 60))
	if got := r.Calls()[1].Tokens; got != 20 {
		t.Fatalf("call after reset tokens = %d, want 20: the reset must not move the count", got)
	}
}

func TestBoundDoesNotCountUnknownDones(t *testing.T) {
	var r Recorder
	for i := range MaxCalls {
		feed(t, &r, startEvent(strconv.Itoa(i), ""))
	}
	feed(t, &r, tokenDone("ghost", 0))
	if n := len(r.Calls()); n != MaxCalls {
		t.Fatalf("calls = %d, want %d", n, MaxCalls)
	}
}

func TestToolDoneWithUnfinishedStateDoesNotCloseTheCall(t *testing.T) {
	var r Recorder
	feed(t, &r, startEvent("a", "env.go"), contracts.Event{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{ID: "a", State: contracts.ToolCallStarted, At: callAt().Add(time.Second)}})
	call := r.Calls()[0]
	if call.Finished() || call.DurationMs != nil {
		t.Fatalf("call = %+v, want still open: a done whose state is not finished must not close it", call)
	}
}
