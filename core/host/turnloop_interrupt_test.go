package host

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func TestInterruptAbandonsATurnQueuedBehindACompaction(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(92))
	f.driver.requestCompaction("", contracts.Conversation{})
	if req := nextFrame(t, f); req.T != compactwire.EventRequest {
		t.Fatalf("frame = %+v, want the compaction request", req)
	}
	if !f.driver.enqueue(queued{ev: contracts.Event{T: eventwire.EventInput, Who: "alice", Text: "vas-y"}}) {
		t.Fatal("the turn was not queued behind the compaction")
	}

	f.driver.Interrupt()
	f.from <- measuredAnswer(t, 4_000)

	if abandoned := waitEmitted(t, f, eventwire.EventAbandoned); abandoned.Who != "alice" || abandoned.Text != "vas-y" {
		t.Fatalf("event = %+v, want the queued turn abandoned with its author and text", abandoned)
	}
	drainWithoutInput(t, f)

	if !f.driver.enqueue(queued{ev: contracts.Event{T: eventwire.EventInput, Who: "alice", Text: "encore"}}) {
		t.Fatal("the next turn was not queued")
	}
	if next := nextFrame(t, f); next.T != eventwire.EventInput || next.Text != "encore" {
		t.Fatalf("frame = %+v, want the turn submitted after the interrupt to run", next)
	}
}

func drainWithoutInput(t *testing.T, f *compactFixture) {
	t.Helper()
	for {
		select {
		case event := <-f.toBridge:
			if event.T == eventwire.EventInput {
				t.Fatalf("the abandoned turn reached the bridge: %+v", event)
			}
		case <-time.After(quietWindow):
			return
		}
	}
}

func TestInterruptAbandonsATurnQueuedBehindAnAutomaticCompaction(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(80))
	openTurn(t, f, 160_000)
	if req := nextFrame(t, f); req.T != compactwire.EventRequest {
		t.Fatalf("frame = %+v, want the automatic compaction request", req)
	}
	if !f.driver.enqueue(queued{ev: contracts.Event{T: eventwire.EventInput, Who: "alice", Text: "vas-y"}}) {
		t.Fatal("the turn was not queued behind the automatic compaction")
	}

	f.driver.Interrupt()
	f.from <- measuredAnswer(t, 170_000)

	waitEmitted(t, f, eventwire.EventAbandoned)
	drainWithoutInput(t, f)
}

func TestMessageQueuedBeforeCompactRunsBeforeTheCompaction(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(92))
	f.driver.queue <- queued{ev: contracts.Event{T: eventwire.EventInput, Who: "alice", Text: "premier"}}
	if in := nextFrame(t, f); in.T != eventwire.EventInput || in.Text != "premier" {
		t.Fatalf("frame = %+v, want the first turn", in)
	}
	if !f.driver.enqueue(queued{ev: contracts.Event{T: eventwire.EventInput, Who: "alice", Text: "avant"}}) {
		t.Fatal("the message was not queued before the compaction")
	}
	if _, queuedOK := f.driver.requestCompaction("", contracts.Conversation{}); !queuedOK {
		t.Fatal("the compaction was not queued")
	}

	f.from <- contracts.Event{T: eventwire.EventReply, Text: "fini", Done: true}
	if before := nextFrame(t, f); before.T != eventwire.EventInput || before.Text != "avant" {
		t.Fatalf("frame = %+v, want the message queued before /compact to run first", before)
	}
	f.from <- contracts.Event{T: eventwire.EventReply, Text: "fini", Done: true}
	if req := nextFrame(t, f); req.T != compactwire.EventRequest {
		t.Fatalf("frame = %+v, want the compaction after the message", req)
	}

	f.driver.Interrupt()
	f.from <- measuredAnswer(t, 4_000)

	drainWithoutInput(t, f)
	if abandoned := f.emittedOf(eventwire.EventAbandoned); len(abandoned) != 0 {
		t.Fatalf("abandoned events = %+v, want none: the message had already run", abandoned)
	}
}

func TestInterruptAfterTheCompactionEndAbandonsNothing(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(92))
	if _, queuedOK := f.driver.requestCompaction("", contracts.Conversation{}); !queuedOK {
		t.Fatal("the compaction was not queued")
	}
	if req := nextFrame(t, f); req.T != compactwire.EventRequest {
		t.Fatalf("frame = %+v, want the compaction request", req)
	}
	f.from <- measuredAnswer(t, 4_000)
	waitCompactingCleared(t, f)

	if !f.driver.enqueue(queued{ev: contracts.Event{T: eventwire.EventInput, Who: "alice", Text: "apres"}}) {
		t.Fatal("the turn was not queued after the compaction")
	}
	f.driver.Interrupt()

	if in := nextInputFrame(t, f); in.Text != "apres" {
		t.Fatalf("frame = %+v, want the turn queued after the compaction to run", in)
	}
	f.from <- contracts.Event{T: eventwire.EventReply, Text: "fini", Done: true}
	if abandoned := f.emittedOf(eventwire.EventAbandoned); len(abandoned) != 0 {
		t.Fatalf("abandoned events = %+v, want none: the interrupt came after the compaction", abandoned)
	}
}

func nextInputFrame(t *testing.T, f *compactFixture) contracts.Event {
	t.Helper()
	for {
		if event := nextFrame(t, f); event.T != eventwire.EventInterrupt {
			return event
		}
	}
}

func waitCompactingCleared(t *testing.T, f *compactFixture) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.driver.compacting.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the compaction flag stayed set after the compaction ended")
		}
		time.Sleep(time.Millisecond)
	}
}
