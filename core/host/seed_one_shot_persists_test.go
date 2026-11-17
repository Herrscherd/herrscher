package host

import (
	"context"
	"io"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestTheOneShotSeedRunsWithoutASessionTable(t *testing.T) {
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{reply: "réponse sans table"}, nil
	}
	sess := state.Session{Name: "solo", ChannelID: "channel"}
	got, err := runOneShotSeedWithIDRuntime(t.Context(), sess, "tâche", newTurnID(), &consolidateSpy{}, oneShotSeedRuntime{persists: newPersistWrites(), backend: backend, warnings: io.Discard})
	if err != nil {
		t.Fatalf("seed error = %v, want the reply", err)
	}
	if got != "réponse sans table" {
		t.Fatalf("reply = %q, want %q", got, "réponse sans table")
	}
}
