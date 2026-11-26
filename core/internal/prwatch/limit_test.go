package prwatch

import (
	"testing"
	"time"
)

func TestHoldKeepsTheLatestPause(t *testing.T) {
	clock := newFakeClock()
	later := clock.Now().Add(10 * time.Minute)
	nearer := clock.Now().Add(2 * time.Minute)
	limit := &Limit{}

	limit.Hold(later)
	limit.Hold(nearer)

	if !limit.Until().Equal(later) {
		t.Fatalf("limit holds until %v after a nearer hold, want the pause in progress %v", limit.Until(), later)
	}
}
