package main

import (
	"context"
	"log/slog"
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

	closed bool
	err    error
}

func (c *closingTalker) Close(context.Context) error {
	c.closed = true
	return c.err
}

func TestGatewayCloseReachesTheRawGatewayThroughBinding(t *testing.T) {
	gateway := &closingTalker{}

	gws, closers, _, _, err := bindGateways([]host.Deps{{Gateway: gateway}})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if _, ok := gws[0].Gateway.(contracts.GatewayCloser); ok {
		t.Fatal("the degraded gateway is expected to hide Close; the test proves nothing otherwise")
	}

	host.CloseGateways(context.Background(), closers, slog.New(slog.DiscardHandler))
	if !gateway.closed {
		t.Fatal("the raw gateway must be closed through the binding")
	}
}
