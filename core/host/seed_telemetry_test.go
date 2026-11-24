package host

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSeedTurnIsExportedAsASpan(t *testing.T) {
	collector, exporter := startTelemetryCollector(t)
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "fait"}, nil
	}
	sess := state.Session{Name: "solo", ChannelID: "channel"}
	runtime := oneShotSeedRuntime{
		desk:        seedDesk(t),
		persists:    newPersistWrites(),
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
		backend:     backend,
		sessions:    &sessionTable{telemetry: exporter},
	}
	if _, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "tâche", newTurnID(), nil, runtime); err != nil {
		t.Fatalf("runOneShotSeedWithIDRuntime: %v", err)
	}
	flushTelemetry(t, exporter)
	collector.named(t, "invoke_agent solo")
}
