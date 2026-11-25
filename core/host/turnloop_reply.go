package host

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/otelexport"
	"github.com/Herrscherd/herrscher/core/outbox"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func (d *sessionDriver) completeTurn(ctx context.Context, event contracts.Event, turnStart time.Time, live int, files outbox.Frame) {
	d.exportTurn(ctx, exportOutcome(event.Outcome), event)
	d.turnReading = contextReading(live, event)
	event.Coordination = d.maybeCoordinate(ctx, event.Text)
	event.Cost = estimateTurnCost(event.Cost, priceableTokens{
		uncachedInput: event.TokensIn,
		output:        event.Tokens,
		cacheRead:     event.CacheRead,
		cacheWrite:    event.CacheCreate,
	}, d.modelNow().rates)
	shown := d.deliverReply(ctx, event, files)
	d.settleTurn()
	if d.sink.Resume != nil && event.Resume != "" {
		d.sink.Resume(event.Resume)
	}
	if d.sink.Project != nil && event.Project != "" {
		d.sink.Project(event.Project)
	}
	d.recordUsage(event)
	d.recordEntry("assistant", event.Text, shown.Text, event.Cost, turnUsage{
		InTokens:    event.TokensIn,
		OutTokens:   event.Tokens,
		CacheRead:   event.CacheRead,
		CacheCreate: event.CacheCreate,
		DurMs:       int(time.Since(turnStart).Milliseconds()),
		CtxTokens:   contextReading(live, event),
		Tools:       d.calls.Calls(),
		Injected:    event.Injected,
	})
	d.deliverPendingReply(event.Text)
	d.metrics.TurnCompleted()
	if verdict := d.gate.Check(d.name); verdict.Reason != "" {
		d.emitPaused(ctx, verdict.Reason)
	}
}

func exportOutcome(outcome contracts.TurnOutcome) otelexport.Outcome {
	switch outcome {
	case contracts.TurnFailed:
		return otelexport.OutcomeFailed
	case contracts.TurnInterrupted:
		return otelexport.OutcomeInterrupted
	}
	return otelexport.OutcomeCompleted
}

func (d *sessionDriver) deliverPendingReply(text string) {
	d.seenMu.Lock()
	defer d.seenMu.Unlock()
	if d.pendingReply != nil {
		d.pendingReply <- text
		d.pendingReply = nil
	}
}

func (d *sessionDriver) fanOut(ctx context.Context, event contracts.Event) {
	d.fanOutReply(ctx, event, outbox.Frame{})
}

func (d *sessionDriver) fanOutReply(ctx context.Context, event contracts.Event, files outbox.Frame) contracts.Event {
	if d.saidElsewhere {
		shown := d.withElsewhereNotice(event, files)
		d.tapEvent(d.stamp(shown))
		return shown
	}
	shown := d.withUnsentFiles(event, files, d.carriesFiles())
	d.tapEvent(d.stamp(shown))
	d.eachGateway(func(index int, gateway contracts.GatewaySet) {
		sender, canSend := d.fileSenders[gateway.Gateway.Manifest().Kind]
		d.emitToGateway(ctx, index, gateway, d.stamp(d.withUnsentFiles(event, files, canSend)))
		if canSend {
			d.sendReplyFiles(ctx, index, gateway, sender, files.Files)
		}
	})
	return shown
}

func (d *sessionDriver) tapEvent(event contracts.Event) {
	d.observeCalls(event)
	if d.emitTap != nil {
		d.emitTap(event)
	}
}

func (d *sessionDriver) observeCalls(event contracts.Event) {
	if _, _, err := d.calls.Apply(event); errors.Is(err, timeline.ErrFull) {
		slog.Warn("turn tool calls past the bound are not kept", "bound", timeline.MaxCalls)
	}
}

func (d *sessionDriver) carriesFiles() bool {
	for _, gateway := range d.gateways {
		if _, ok := d.fileSenders[gateway.Gateway.Manifest().Kind]; ok {
			return true
		}
	}
	return false
}

func (d *sessionDriver) deliverReply(ctx context.Context, event contracts.Event, files outbox.Frame) contracts.Event {
	return d.fanOutReply(ctx, event, d.revet(files))
}

func (d *sessionDriver) revet(files outbox.Frame) outbox.Frame {
	if len(files.Files) == 0 {
		return files
	}
	accepted, refusals := outbox.Vet(files.Dir, files.Files, d.outboundMaxBytes)
	for _, refusal := range refusals {
		slog.Warn("outbound file refused at delivery, not sent", "file", refusal.Name, "err", refusal.Cause)
		files.Refused = append(files.Refused, refusal.Name)
	}
	files.Files = accepted
	return files
}

func (d *sessionDriver) withUnsentFiles(event contracts.Event, files outbox.Frame, canSend bool) contracts.Event {
	var notices []string
	if len(files.Refused) > 0 {
		notices = append(notices, d.hostText("outbound.refused", map[string]any{namesParam: d.noticeNames(files.Refused)}))
	}
	if !canSend && len(files.Files) > 0 {
		notices = append(notices, d.hostText("outbound.unsent", map[string]any{namesParam: d.noticeNames(fileNames(files.Files))}))
	}
	if len(notices) == 0 {
		return event
	}
	event.Text = strings.TrimSpace(strings.Join(append([]string{event.Text}, notices...), "\n\n"))
	return event
}

func (d *sessionDriver) withElsewhereNotice(event contracts.Event, files outbox.Frame) contracts.Event {
	names := frameNames(files)
	if len(names) == 0 {
		return event
	}
	notice := d.hostText("outbound.elsewhere", map[string]any{namesParam: d.noticeNames(names)})
	event.Text = strings.TrimSpace(event.Text + "\n\n" + notice)
	return event
}
