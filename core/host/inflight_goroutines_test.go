package host

import (
	"testing"
	"time"
)

func TestRepeatedStopsDoNotAccumulateWaiters(t *testing.T) {
	var out lockedBuffer
	h := newInflightHub(t, "stop-"+newTurnID(), 20*time.Millisecond, &out)
	end := h.acceptFails.Begin()
	defer end()

	h.awaitInFlight()
	before := goroutinesCreatedIn("inflight.go", "teardown_window.go")
	for range 50 {
		h.awaitInFlight()
	}
	if grown := goroutinesCreatedIn("inflight.go", "teardown_window.go") - before; grown > 5 {
		t.Fatalf("50 stops left %d goroutines behind", grown)
	}
}
