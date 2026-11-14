package host

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type failingSeedBackend struct{}

func (failingSeedBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "", errors.New("refused by the vendor")
}
func (failingSeedBackend) Close() error { return nil }

func TestTheOneShotBridgeCancelsTheSeedOnlyWhenNoReplyCame(t *testing.T) {
	for _, tc := range []struct {
		name       string
		backend    error
		turn       contracts.Backend
		wantCancel bool
	}{
		{name: "a failed turn still hands its reply back", turn: failingSeedBackend{}, wantCancel: false},
		{name: "a backend that cannot start hands no reply", backend: errors.New("no model"), wantCancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cancelled := false
			toBridge := make(chan contracts.Event, 1)
			fromBridge := make(chan contracts.Event, bridgeEventBuffer)
			bridgeErr := make(chan error, 1)
			toBridge <- contracts.Event{T: "input", Text: "tâche"}
			runtime := oneShotSeedRuntime{backend: func(context.Context, state.Session) (contracts.Backend, error) {
				return tc.turn, tc.backend
			}}
			runOneShotBridge(context.Background(), func() { cancelled = true }, state.Session{Name: "solo", ChannelID: "channel"}, nil, runtime, toBridge, fromBridge, bridgeErr)
			if cancelled != tc.wantCancel {
				t.Fatalf("cancelled = %t, want %t", cancelled, tc.wantCancel)
			}
			if err := <-bridgeErr; err == nil {
				t.Fatal("bridge error = nil, want the failure")
			}
		})
	}
}

type refusedWithTrailerBackend struct{}

func (refusedWithTrailerBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "travail préparé\n⟢ delegate: roblox-scripter " + trailerSeparator + " inspection en lecture seule", errors.New("refused by the vendor")
}
func (refusedWithTrailerBackend) Close() error { return nil }

func TestAFailedOneShotTurnNeitherCoordinatesNorConsolidates(t *testing.T) {
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return refusedWithTrailerBackend{}, nil
	}
	sess := state.Session{Name: "orchestrator", ChannelID: "channel"}
	orch := &consolidateSpy{}
	coord := &recordingCoord{}
	_, err := runOneShotSeedWithIDRuntime(context.Background(), sess, "délègue", newTurnID(), orch, oneShotSeedRuntime{backend: backend, coordinator: coord})
	if _, failed := errors.AsType[*bridge.TurnFailedError](err); !failed {
		t.Fatalf("seed error = %v, want a failed turn", err)
	}
	if orch.called {
		t.Fatal("a failed turn returned no reply, so consolidating would only call the provider that just failed")
	}
	if len(coord.delegates) != 0 {
		t.Fatalf("delegate calls = %d, want none after a failed turn", len(coord.delegates))
	}
}

func TestAFailedSeedTurnReadsInTheOperatorLanguage(t *testing.T) {
	backend := func(context.Context, state.Session) (contracts.Backend, error) {
		return failingSeedBackend{}, nil
	}
	_, err := runOneShotSeedWithIDRuntime(context.Background(), state.Session{Name: "solo", ChannelID: "channel"}, "tâche", newTurnID(), nil, oneShotSeedRuntime{backend: backend, warnings: io.Discard})
	for _, tc := range []struct {
		lang i18n.Language
		want string
	}{
		{i18n.English, "unexpected error: refused by the vendor"},
		{i18n.French, "erreur inattendue : refused by the vendor"},
	} {
		if got := cli.Localize(err, tc.lang).Error(); !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want it to contain %q", tc.lang, got, tc.want)
		}
	}
}
