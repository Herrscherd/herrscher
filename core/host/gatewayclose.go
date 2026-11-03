package host

import (
	"context"
	"log/slog"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const gatewayCloseTimeout = 10 * time.Second

func closeGateways(ctx context.Context, gateways []contracts.Gateway, log *slog.Logger) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayCloseTimeout)
	defer cancel()
	var closing sync.WaitGroup
	for _, gateway := range gateways {
		closer, ok := gateway.(contracts.GatewayCloser)
		if !ok {
			continue
		}
		closing.Go(func() {
			if err := closer.Close(closeCtx); err != nil {
				log.Error("close gateway", "err", err)
			}
		})
	}
	closing.Wait()
}

func gatewaysOf(deps []Deps) []contracts.Gateway {
	gateways := make([]contracts.Gateway, 0, len(deps))
	for _, dep := range deps {
		gateways = append(gateways, dep.Gateway)
	}
	return gateways
}
