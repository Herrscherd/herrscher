package host

import (
	"context"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/otelexport"
)

func (d *sessionDriver) exportTurn(ctx context.Context, outcome otelexport.Outcome, started time.Time, usage contracts.Event) {
	if !d.telemetry.Enabled() {
		return
	}
	terms := d.modelNow()
	d.telemetry.Export(ctx, otelexport.Turn{
		Session:             d.name,
		Provider:            genAIProvider(terms.vendor),
		Model:               terms.model,
		Started:             started,
		Ended:               time.Now(),
		Outcome:             outcome,
		InputTokens:         usage.TokensIn,
		OutputTokens:        usage.Tokens,
		CacheReadTokens:     usage.CacheRead,
		CacheCreationTokens: usage.CacheCreate,
		Calls:               d.calls.Calls(),
	})
}

func genAIProvider(vendor string) string {
	switch vendor {
	case vendorClaude:
		return "anthropic"
	case backendCodex:
		return "openai"
	}
	return vendor
}
