package host

import (
	"context"
	"log/slog"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const gatewayCloseTimeout = 10 * time.Second

type gatewayCloseOutcome struct {
	kind string
	err  error
}

// CloseGateways closes every GatewayCloser within gatewayCloseTimeout and logs each failure.
func CloseGateways(ctx context.Context, gateways []contracts.Gateway, log *slog.Logger) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayCloseTimeout)
	defer cancel()
	outcomes := make(chan gatewayCloseOutcome, len(gateways))
	pending := make(map[string]int)
	for _, gateway := range gateways {
		closer, ok := gateway.(contracts.GatewayCloser)
		if !ok {
			continue
		}
		kind := gateway.Manifest().Kind
		pending[kind]++
		go func() {
			outcomes <- gatewayCloseOutcome{kind: kind, err: closer.Close(closeCtx)}
		}()
	}
	for remaining := sumPending(pending); remaining > 0; remaining-- {
		select {
		case outcome := <-outcomes:
			pending[outcome.kind]--
			if outcome.err != nil {
				log.Error("close gateway", "kind", outcome.kind, "err", outcome.err)
			}
		case <-closeCtx.Done():
			log.Error("gateways still closing at the deadline; abandoning them", "kinds", stuckKinds(pending))
			return
		}
	}
}

func sumPending(pending map[string]int) int {
	total := 0
	for _, n := range pending {
		total += n
	}
	return total
}

func stuckKinds(pending map[string]int) []string {
	var kinds []string
	for kind, n := range pending {
		for range n {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}
