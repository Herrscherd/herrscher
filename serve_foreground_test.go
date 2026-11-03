package main

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/host"
)

type returningForeground struct{}

func (returningForeground) RunForeground(context.Context, context.CancelFunc) error { return nil }

func TestSuperviseForegroundJoinsTheHubBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})

	err := superviseForeground(ctx, cancel, returningForeground{}, func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("the foreground result must be returned, got %v", err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("the hub must have stopped before the foreground returned")
	}
}

type closingTalker struct {
	talker

	err error
}

func (c *closingTalker) Close(context.Context) error {
	return c.err
}

func TestBindingKeepsTheRawGatewayAsItsCloser(t *testing.T) {
	gateway := &closingTalker{}

	gws, closers, _, _, err := bindGateways([]host.Deps{{Gateway: gateway}})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if _, ok := gws[0].Gateway.(contracts.GatewayCloser); ok {
		t.Fatal("the degraded gateway is expected to hide Close; the test proves nothing otherwise")
	}
	if len(closers) != 1 || closers[0] != contracts.Gateway(gateway) {
		t.Fatalf("the closers must hold the raw gateway, got %v", closers)
	}
}
