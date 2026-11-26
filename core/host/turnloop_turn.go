package host

import (
	"context"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/otelexport"
	"github.com/Herrscherd/herrscher/core/outbox"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func (d *sessionDriver) pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case relay := <-d.relays:
			d.relayEvent(ctx, relay)
		case item := <-d.queue:
			if !d.enterTurn(ctx) {
				return
			}
			if item.ev.T == eventwire.EventCompactNow {
				d.runCompactNow(ctx, d.takeCompactInstruction())
				d.releaseTurn()
				continue
			}
			if d.abandonedByInterrupt(item) {
				d.fanOut(ctx, contracts.Event{T: eventwire.EventAbandoned, Who: item.ev.Who, Text: item.ev.Text})
				d.releaseTurn()
				continue
			}
			event, guard, ok := d.prepareTurn(ctx, item)
			if !ok {
				d.releaseTurn()
				continue
			}
			d.turnReading = 0
			d.calls = timeline.Recorder{}
			d.runTurn(ctx, event, guard)
			d.activeTurnID = ""
			d.pickTurnID = ""
			d.maybeAutoCompact(ctx)
			d.releaseTurn()
		}
	}
}

func (d *sessionDriver) abandonedByInterrupt(item queued) bool {
	return item.ev.T == eventwire.EventInput && !isOrchestratedTurn(item.ev) && item.interrupts != d.interrupts.Load()
}

func (d *sessionDriver) enterTurn(ctx context.Context) bool {
	select {
	case d.turn <- struct{}{}:
		d.running.Store(true)
		return true
	case <-ctx.Done():
		return false
	}
}

func (d *sessionDriver) claimTurn() bool {
	select {
	case d.turn <- struct{}{}:
		return true
	default:
		return false
	}
}

func (d *sessionDriver) releaseTurn() {
	d.running.Store(false)
	<-d.turn
}

func (d *sessionDriver) releaseClaim() {
	<-d.turn
}

func (d *sessionDriver) inTurn() bool {
	return d.running.Load()
}

func (d *sessionDriver) prepareTurn(ctx context.Context, item queued) (contracts.Event, tokenGuard, bool) {
	event := item.ev
	d.saidElsewhere = event.T == eventwire.EventInput && outsideBoundGateways(item.origin, d.gateways)
	if event.T != eventwire.EventInput {
		event.SessionIncarnation = ""
		event.TurnID = ""
		if event.T == eventwire.EventPick {
			d.pickTurnID = newTurnID()
			event.TurnID = d.pickTurnID
		}
		event.Agent = ""
		d.activeTurnID = ""
		return event, tokenGuard{}, true
	}
	verdict := d.gate.Check(d.name)
	if verdict.Reason != "" {
		d.emitPaused(ctx, verdict.Reason)
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("turn.budget_refused", map[string]any{"Reason": verdict.Reason})})
		return event, tokenGuard{}, false
	}
	if event.TurnID == "" {
		event.TurnID = newTurnID()
	}
	event.SessionIncarnation = d.incarnation
	event.Agent = d.agent
	d.activeTurnID = event.TurnID
	if d.sink.TurnOpened != nil {
		d.sink.TurnOpened(event.TurnID)
	}
	if d.sink.Checkpoint != nil {
		d.sink.Checkpoint(event.TurnID, event.Text)
	}
	logCleanup(d.recordEntry("user", event.Text, event.Text, 0, turnUsage{}))
	d.fanOut(ctx, contracts.Event{T: eventwire.EventHuman, Who: event.Who, Text: event.Text})
	return event, tokenGuard{headroom: verdict.Headroom, capped: verdict.Capped}, true
}

func (d *sessionDriver) runTurn(ctx context.Context, event contracts.Event, guard tokenGuard) {
	if d.telemetry.Enabled() {
		d.turnSpan = d.openTurnSpan()
	}
	select {
	case <-d.hangup:
	default:
	}
	if !d.announceLanguage(ctx) || !d.sendToBridge(ctx, event) {
		d.abandon(ctx, event)
		return
	}
	if !d.awaitTurn(ctx, guard) {
		d.abandon(ctx, event)
	}
}

func (d *sessionDriver) sendToBridge(ctx context.Context, event contracts.Event) bool {
	select {
	case d.toBridge <- event:
		return true
	case <-d.hangup:
		return false
	case <-ctx.Done():
		return false
	}
}

func (d *sessionDriver) abandon(ctx context.Context, event contracts.Event) {
	d.exportTurn(ctx, otelexport.OutcomeAbandoned, contracts.Event{})
	if event.T != eventwire.EventInput {
		return
	}
	if ctx.Err() == nil && d.recordInterruption() == nil {
		d.settleTurn()
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventAbandoned})
}

func (d *sessionDriver) settleTurn() {
	if d.sink.TurnSettled != nil {
		d.sink.TurnSettled(d.currentTurnID())
	}
}

func (d *sessionDriver) awaitTurn(ctx context.Context, guard tokenGuard) bool {
	d.metrics.TurnStarted()
	turnStart := time.Now()
	live := 0
	var files outbox.Frame
	defer func() { d.removeTurnFiles(files) }()
	for {
		select {
		case <-ctx.Done():
			d.metrics.TurnAbandoned()
			return false
		case <-d.hangup:
			d.metrics.TurnAbandoned()
			return false
		case event, ok := <-d.from:
			if !ok {
				d.metrics.TurnAbandoned()
				return false
			}
			if event.T == outbox.EventFiles {
				files = d.replaceTurnFiles(files, event)
				continue
			}
			if event.T == eventwire.EventReply && event.Done {
				d.completeTurn(ctx, event, turnStart, live, files)
				return true
			}
			live = d.relayTurnEvent(ctx, &guard, event, live)
		}
	}
}

func (d *sessionDriver) relayTurnEvent(ctx context.Context, guard *tokenGuard, event contracts.Event, live int) int {
	if historywire.IsFrame(event.T) {
		return live
	}
	if event.T == eventwire.EventBackendNotice {
		d.notifyBackendNotice(event)
		return live
	}
	d.fanOut(ctx, event)
	if n := promptSize(event); n > 0 {
		live = n
	}
	d.enforceTokenGuard(ctx, guard, event)
	return live
}
