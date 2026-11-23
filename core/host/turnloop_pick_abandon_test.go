package host

import (
	"context"
	"testing"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/eventwire"
)

func TestPickTurnCutBeforeBridgeIsExportedOnceAsAbandoned(t *testing.T) {
	collector, exporter := startTelemetryCollector(t)
	d := telemetryDriver(exporter, claudeTerms)
	hangup := make(chan struct{})
	close(hangup)
	d.hangup = hangup

	d.runTurn(context.Background(), contracts.Event{T: eventwire.EventPick}, tokenGuard{})
	flushTelemetry(t, exporter)

	if d.turnSpan != (turnSpan{}) {
		t.Errorf("turnSpan = %+v after an abandoned turn, want it cleared", d.turnSpan)
	}
	var turns []*tracepb.Span
	for _, span := range collector.all() {
		if span.GetName() == "invoke_agent alpha" {
			turns = append(turns, span)
		}
	}
	if len(turns) != 1 {
		t.Fatalf("exported %d turn spans for one cut pick, want 1", len(turns))
	}
	if got := spanAttr(turns[0], "error.type"); got.GetStringValue() != "turn_abandoned" {
		t.Errorf("error.type = %q, want turn_abandoned", got.GetStringValue())
	}
}
