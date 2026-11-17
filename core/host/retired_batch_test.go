package host

import (
	"testing"
	"time"
)

func TestRetireesGetAFreshWindowAfterTheInFlightWait(t *testing.T) {
	var out lockedBuffer
	h := newInflightHub(t, "retiree-"+newTurnID(), time.Second, &out)
	batch := h.newBatch()
	t.Cleanup(batch.cancel)

	<-batch.window.ends
	done := make(chan struct{})
	time.AfterFunc(50*time.Millisecond, func() { close(done) })
	batch.retired = []retirement{{name: "retiree", live: liveSession{done: done}}}
	batch.await(h)

	if out.String() != "" {
		t.Fatalf("a retiree that finished within its own grace was reported as overrun: %q", out.String())
	}
}
