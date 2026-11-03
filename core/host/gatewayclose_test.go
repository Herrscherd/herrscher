package host

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type closingGateway struct {
	contracts.Gateway

	err         error
	closed      bool
	hasDeadline bool
	canceled    bool
}

func (g *closingGateway) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: "closing"}
}

func (g *closingGateway) Close(ctx context.Context) error {
	g.closed = true
	_, g.hasDeadline = ctx.Deadline()
	g.canceled = ctx.Err() != nil
	return g.err
}

func TestCloseGatewaysClosesEveryCloserWithALiveDeadline(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	first := &closingGateway{}
	failing := &closingGateway{err: errors.New("stuck")}
	plain := struct{ contracts.Gateway }{}

	CloseGateways(parent, []contracts.Gateway{first, plain, failing}, slog.New(slog.DiscardHandler))

	for name, gateway := range map[string]*closingGateway{"first": first, "failing": failing} {
		if !gateway.closed {
			t.Fatalf("%s gateway was not closed", name)
		}
		if !gateway.hasDeadline {
			t.Fatalf("%s gateway closed without a deadline", name)
		}
		if gateway.canceled {
			t.Fatalf("%s gateway closed with the cancelled run context", name)
		}
	}
}
