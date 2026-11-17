package host

import (
	"runtime"
	"testing"
	"time"
)

func TestRepeatedStopsDoNotAccumulateWaiters(t *testing.T) {
	var out lockedBuffer
	h := newInflightHub(t, "stop-"+newTurnID(), 20*time.Millisecond, &out)
	end := h.acceptFails.Begin()
	defer end()

	h.awaitInFlight()
	before := runtime.NumGoroutine()
	for range 50 {
		h.awaitInFlight()
	}
	if grown := runtime.NumGoroutine() - before; grown > 5 {
		t.Fatalf("50 stops left %d goroutines behind", grown)
	}
}
