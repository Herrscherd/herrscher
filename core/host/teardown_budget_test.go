package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestBridgeTermGraceIsBelowTeardownBudget(t *testing.T) {
	h := testHub(&hub{messages: newHostMessages(), teardownGrace: defaultTeardownGrace})

	if budget := h.teardownBudget(); supervisor.BridgeTermGrace >= budget {
		t.Fatalf("BridgeTermGrace = %v must stay below the teardown budget %v", supervisor.BridgeTermGrace, budget)
	}
}
