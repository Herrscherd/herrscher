package host

import (
	"context"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/otelexport"
)

type turnSpan struct {
	started  time.Time
	provider string
	model    string
}

func (d *sessionDriver) openTurnSpan() turnSpan {
	terms := d.modelNow()
	return turnSpan{started: time.Now(), provider: genAIProvider(terms.vendor), model: terms.model}
}

func (d *sessionDriver) exportTurn(ctx context.Context, outcome otelexport.Outcome, usage contracts.Event) {
	span := d.turnSpan
	d.turnSpan = turnSpan{}
	if !d.telemetry.Enabled() {
		return
	}
	err := d.telemetry.Export(ctx, otelexport.Turn{
		Session:             d.name,
		Provider:            span.provider,
		Model:               span.model,
		Started:             span.started,
		Ended:               time.Now(),
		Outcome:             outcome,
		InputTokens:         usage.TokensIn,
		OutputTokens:        usage.Tokens,
		CacheReadTokens:     usage.CacheRead,
		CacheCreationTokens: usage.CacheCreate,
		Calls:               d.calls.Calls(),
	})
	if err != nil {
		d.noticeLog.Warn("turn not exported to telemetry", keySession, d.name, "err", err)
	}
}

func genAIProvider(vendor string) string {
	switch vendor {
	case vendorClaude:
		return semconv.GenAIProviderNameAnthropic.Value.AsString()
	case backendCodex:
		return semconv.GenAIProviderNameOpenAI.Value.AsString()
	}
	return vendor
}
