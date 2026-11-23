package timeline

import (
	"errors"
	"strconv"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func gapEvent() contracts.Event {
	return contracts.Event{T: eventwire.EventGap, Value: eventwire.GapExpired}
}

func doneReplyEvent() contracts.Event {
	return contracts.Event{T: eventwire.EventReply, Done: true}
}

func abandonedEvent() contracts.Event {
	return contracts.Event{T: eventwire.EventAbandoned}
}

func closedEvent(value string) contracts.Event {
	return contracts.Event{T: eventwire.EventClosed, Value: value}
}

func settleAll(t *testing.T, r *Recorder, at Handover, events ...contracts.Event) []Call {
	t.Helper()
	var handed []Call
	for index := range events {
		calls, err := r.Settle(events[index], at)
		if err != nil {
			t.Fatalf("Settle(%v) error = %v", events[index].T, err)
		}
		handed = append(handed, calls...)
	}
	return handed
}

func TestAToolDoneHandsItsCallAtTheToolDoneHandover(t *testing.T) {
	var r Recorder
	handed := settleAll(t, &r, AtToolDone, startEvent("a", "env.go"), tokenDone("a", 0))
	if len(handed) != 1 || handed[0].ID != "a" || !handed[0].Finished() {
		t.Fatalf("handed = %+v, want the finished call a at its done", handed)
	}
}

func TestAToolDoneIsNotHandedAtAGap(t *testing.T) {
	var r Recorder
	handed := settleAll(t, &r, AtGap, startEvent("a", "env.go"), tokenDone("a", 0))
	if len(handed) != 0 {
		t.Fatalf("handed = %+v at a tool done, want nothing before the gap", handed)
	}
}

func TestAGapHandsFinishedCallsAndKeepsOpenOnes(t *testing.T) {
	var r Recorder
	settleAll(t, &r, AtGap, startEvent("a", "env.go"), startEvent("b", "go.mod"), tokenDone("b", 0))
	handed := settleAll(t, &r, AtGap, gapEvent())
	if len(handed) != 1 || handed[0].ID != "b" {
		t.Fatalf("handed = %+v at the gap, want only the finished call b", handed)
	}
}

func TestAStartIsNotHandedAtAToolDone(t *testing.T) {
	var r Recorder
	handed := settleAll(t, &r, AtToolDone, startEvent("a", "env.go"))
	if len(handed) != 0 {
		t.Fatalf("handed = %+v at a start, want nothing", handed)
	}
}

func TestADoneReplyEndsTheTurnWithItsOpenCallsUnfinished(t *testing.T) {
	var r Recorder
	handed := settleAll(t, &r, AtGap, startEvent("a", "env.go"), doneReplyEvent())
	if len(handed) != 1 || handed[0].ID != "a" || handed[0].Finished() {
		t.Fatalf("handed = %+v at the turn end, want the open call a unfinished", handed)
	}
}

func TestAReplyThatIsNotDoneKeepsTheTurnOpen(t *testing.T) {
	var r Recorder
	handed := settleAll(t, &r, AtGap, startEvent("a", "env.go"), contracts.Event{T: eventwire.EventReply})
	if len(handed) != 0 {
		t.Fatalf("handed = %+v at a reply that is not done, want nothing", handed)
	}
	if !r.Knows("a") {
		t.Fatal("the turn forgot its call a at a reply that is not done")
	}
}

func TestAbandonedEndsTheTurn(t *testing.T) {
	var r Recorder
	handed := settleAll(t, &r, AtGap, startEvent("a", "env.go"), abandonedEvent())
	if len(handed) != 1 || handed[0].ID != "a" || handed[0].Finished() {
		t.Fatalf("handed = %+v at an abandon, want the open call a unfinished", handed)
	}
}

func TestAClosedSessionEndsTheTurn(t *testing.T) {
	var r Recorder
	handed := settleAll(t, &r, AtToolDone, startEvent("a", "env.go"), closedEvent(""))
	if len(handed) != 1 || handed[0].ID != "a" {
		t.Fatalf("handed = %+v at a close, want the open call a unfinished", handed)
	}
}

func TestARenamedSessionKeepsTheTurn(t *testing.T) {
	var r Recorder
	handed := settleAll(t, &r, AtToolDone, startEvent("a", "env.go"), closedEvent(eventwire.ClosedRenamed))
	if len(handed) != 0 {
		t.Fatalf("handed = %+v at a rename, want the turn kept", handed)
	}
	if !r.Knows("a") {
		t.Fatal("the turn forgot its call a at a rename")
	}
}

func TestEndingTheTurnForgetsItsCallIDs(t *testing.T) {
	var r Recorder
	settleAll(t, &r, AtGap, startEvent("a", "env.go"), doneReplyEvent())
	if r.Knows("a") {
		t.Fatal("the next turn still knows call a after the turn ended")
	}
}

func TestAStartOfAKnownCallIsAReplay(t *testing.T) {
	var r Recorder
	settleAll(t, &r, AtGap, startEvent("a", "env.go"))
	if !r.Replays(startEvent("a", "env.go")) {
		t.Fatal("a start of the known call a is not a replay")
	}
}

func TestAStartOfANewCallIsNotAReplay(t *testing.T) {
	var r Recorder
	settleAll(t, &r, AtGap, startEvent("a", "env.go"))
	if r.Replays(startEvent("b", "go.mod")) {
		t.Fatal("a start of the new call b is taken for a replay")
	}
}

func TestSettleReportsTheBoundOnTheFirstDroppedCall(t *testing.T) {
	var r Recorder
	for i := range MaxCalls {
		if _, err := r.Settle(startEvent(strconv.Itoa(i), ""), AtToolDone); err != nil {
			t.Fatalf("start %d error = %v, want the turn to keep it", i, err)
		}
	}
	if _, err := r.Settle(startEvent("over", ""), AtToolDone); !errors.Is(err, ErrFull) {
		t.Fatalf("start past the bound error = %v, want ErrFull", err)
	}
}
