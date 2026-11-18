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

	if abandoned := waitEmitted(t, f, eventwire.EventAbandoned); abandoned.T != eventwire.EventAbandoned {
		t.Fatalf("event = %+v, want the queued turn abandoned", abandoned)
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
