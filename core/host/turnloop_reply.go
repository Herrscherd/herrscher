package host

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/outbox"
)

func (d *sessionDriver) completeTurn(ctx context.Context, event contracts.Event, turnStart time.Time, live int, files outbox.Frame) {
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
	})
	d.deliverPendingReply(event.Text)
	d.metrics.TurnCompleted()
	if verdict := d.gate.Check(d.name); verdict.Reason != "" {
		d.emitPaused(ctx, verdict.Reason)
	}
}

func (d *sessionDriver) deliverPendingReply(text string) {
	d.seenMu.Lock()
	defer d.seenMu.Unlock()
	if d.pendingReply != nil {
		d.pendingReply <- text
		d.pendingReply = nil
	}
}

func (d *sessionDriver) enforceTokenGuard(ctx context.Context, guard *tokenGuard, e contracts.Event) {
	spent, over := guard.trips(e)
	if !over {
		return
	}
	d.Interrupt()
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("turn.token_ceiling", map[string]any{"Spent": strconv.FormatUint(spent, 10), "Margin": strconv.FormatUint(guard.headroom, 10)})})
}

func promptSize(e contracts.Event) int {
	return e.TokensIn + e.CacheRead + e.CacheCreate
}

func contextReading(live int, done contracts.Event) int {
	if live > 0 {
		return live
	}
	return promptSize(done)
}

func turnTokens(event contracts.Event) uint64 {
	total := 0
	if event.Tokens > 0 {
		total += event.Tokens
	}
	if event.TokensIn > 0 {
		total += event.TokensIn
	}
	return uint64(total)
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
	if d.emitTap != nil {
		d.emitTap(event)
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
