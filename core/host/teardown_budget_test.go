package host

import (
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestBridgeTermGraceIsBelowTeardownBudget(t *testing.T) {
	h := &hub{teardownGrace: defaultTeardownGrace}

	started := time.Now()
	window, cancel := h.newGrace()
	defer cancel()
	select {
	case <-window.expired:
	case <-time.After(defaultTeardownGrace):
		t.Fatal("teardown budget did not expire within the teardown grace")
	}
	budget := time.Since(started)

	if supervisor.BridgeTermGrace >= budget {
		t.Fatalf("BridgeTermGrace = %v must stay below the teardown budget %v", supervisor.BridgeTermGrace, budget)
	}
}
