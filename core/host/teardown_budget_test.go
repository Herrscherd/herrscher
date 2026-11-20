package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestBridgeTermGraceIsBelowTeardownBudget(t *testing.T) {
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), teardownGrace: defaultTeardownGrace})

	if budget := h.teardownBudget(); supervisor.BridgeTermGrace >= budget {
		t.Fatalf("BridgeTermGrace = %v must stay below the teardown budget %v", supervisor.BridgeTermGrace, budget)
	}
}
