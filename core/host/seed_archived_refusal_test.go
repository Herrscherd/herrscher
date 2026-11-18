package host

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestOneShotSeedRefusesAnArchivedSession(t *testing.T) {
	store := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := store.AddSession(state.Session{Name: "doomed", ChannelID: "c-doomed", Incarnation: "doomed"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetArchived("doomed", true); err != nil {
		t.Fatal(err)
	}
	runtime := oneShotSeedRuntime{
		persists:    newPersistWrites(),
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
		timeout:     time.Millisecond,
		backend: func(context.Context, state.Session) (contracts.Backend, error) {
			t.Error("a seed on an archived session reached the backend")
			return nil, errSeedArchivedSession
		},
	}
	_, err := runOneShotSeedIDWithRuntime(t.Context(), store, "doomed", "write the digest", "turn-1", runtime)
	if !errors.Is(err, errSeedArchivedSession) {
		t.Fatalf("a seed on an archived session returned %v, want the archived refusal", err)
	}
}
