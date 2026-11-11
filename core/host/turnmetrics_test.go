package host

import (
	"context"
	"io"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
)

func TestTurnMetricsCountStartCompleteAbandon(t *testing.T) {
	a := &fanRecorder{}
	toBridge := make(chan contracts.Event, 4)
	fromBridge := make(chan contracts.Event, 4)
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, toBridge, fromBridge, io.Discard)
	m := metrics.NewRegistry()
	d.metrics = m

	fromBridge <- contracts.Event{T: "reply", Text: "ok", Done: true}
	d.awaitTurn(context.Background(), tokenGuard{})

	d.hangup <- struct{}{}
	d.awaitTurn(context.Background(), tokenGuard{})

	s := m.Snapshot()
	if s.TurnsStarted != 2 {
		t.Fatalf("turns started = %d, want 2", s.TurnsStarted)
	}
	if s.TurnsCompleted != 1 {
		t.Fatalf("turns completed = %d, want 1", s.TurnsCompleted)
	}
	if s.TurnsAbandoned != 1 {
		t.Fatalf("turns abandoned = %d, want 1", s.TurnsAbandoned)
	}
}
