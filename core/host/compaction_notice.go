package host

import (
	"context"
	"errors"
	"strconv"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

func (d *sessionDriver) failCompaction(ctx context.Context, trigger contracts.CompactTrigger, err error) {
	event := d.compactionFailure(trigger, err)
	d.tapEvent(d.stamp(event))
	if d.saidElsewhere {
		return
	}
	notice, queueable := d.renderableNotice(err)
	d.eachGateway(func(index int, gateway contracts.GatewaySet) {
		if queueable && d.queueNotice(index, gateway, notice, d.failedFallback(trigger, notice)) {
			d.emitToGateway(ctx, index, gateway, d.stamp(d.quietFailure(trigger)))
			return
		}
		d.emitToGateway(ctx, index, gateway, d.stamp(event))
	})
}

func (d *sessionDriver) renderableNotice(err error) (contracts.Notice, bool) {
	typed, ok := errors.AsType[contracts.NoticeError](err)
	if !ok || !d.noticeRenders(typed.Notice()) {
		return contracts.Notice{}, false
	}
	return typed.Notice(), true
}

func (d *sessionDriver) compactionFailure(trigger contracts.CompactTrigger, err error) contracts.Event {
	payload := eventwire.CompactionFailure{Trigger: trigger.String()}
	reason, known := knownCompactFailure(err)
	notice, isNotice := d.renderableNotice(err)
	switch {
	case isNotice:
		payload.Code = uint16(notice.Code)
	case known:
		payload.Reason = reason
	default:
		payload.Reason = eventwire.CompactReasonUnknown
		d.noticeLog.Warn("compaction failed", keySession, d.name, "trigger", trigger.String(), "err", redact.Output([]byte(err.Error())))
	}
	return d.encodedCompactionFailure(payload)
}

func (d *sessionDriver) quietFailure(trigger contracts.CompactTrigger) contracts.Event {
	return d.encodedCompactionFailure(eventwire.CompactionFailure{Trigger: trigger.String()})
}

func (d *sessionDriver) failedFallback(trigger contracts.CompactTrigger, notice contracts.Notice) func() contracts.Event {
	return func() contracts.Event {
		return d.encodedCompactionFailure(eventwire.CompactionFailure{Trigger: trigger.String(), Code: uint16(notice.Code)})
	}
}

func (d *sessionDriver) compactedFallback(trigger contracts.CompactTrigger, notice contracts.Notice, tokens int) func() contracts.Event {
	return func() contracts.Event {
		return d.compactionDone(trigger, notice.Code, "", tokens)
	}
}

func (d *sessionDriver) encodedCompactionFailure(payload eventwire.CompactionFailure) contracts.Event {
	value, err := payload.Encode()
	if err != nil {
		d.noticeLog.Warn("compaction failure not encoded", keySession, d.name, "err", err)
	}
	return contracts.Event{T: eventwire.EventCompactionFailed, Value: value}
}

func (d *sessionDriver) compactionDone(trigger contracts.CompactTrigger, code contracts.Code, text string, tokens int) contracts.Event {
	value, err := eventwire.CompactionDone{Trigger: trigger.String(), Code: uint16(code)}.Encode()
	if err != nil {
		d.noticeLog.Warn("compaction done not encoded", keySession, d.name, "err", err)
	}
	return contracts.Event{T: eventwire.EventCompactionDone, Value: value, Text: text, Tokens: tokens}
}

func knownCompactFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, errBridgeGone):
		return eventwire.CompactReasonBridgeGone, true
	case errors.Is(err, errNoCompactorFactory):
		return eventwire.CompactReasonNoCompactorFactory, true
	case errors.Is(err, errNilCompactor):
		return eventwire.CompactReasonNilCompactor, true
	case errors.Is(err, errNoCompactorPlugin):
		return eventwire.CompactReasonNoCompactorPlugin, true
	case errors.Is(err, historywire.ErrNoHistoryEditor):
		return eventwire.CompactReasonNoHistoryPort, true
	case errors.Is(err, errHistoryTimeout):
		return eventwire.CompactReasonHistoryTimeout, true
	case errors.Is(err, historywire.ErrHistoryTooLarge):
		return eventwire.CompactReasonHistoryTooLarge, true
	case errors.Is(err, errTooManyEdits):
		return eventwire.CompactReasonTooManyEdits, true
	}
	return "", false
}

func (d *sessionDriver) announceCompaction(ctx context.Context, trigger contracts.CompactTrigger, result contracts.CompactResult) {
	tokens := measuredTokens(result)
	if result.Notice.Code == 0 || !d.noticeRenders(result.Notice) {
		d.fanOut(ctx, d.compactionDone(trigger, 0, result.Note, tokens))
		return
	}
	d.notifyGateways(result.Notice, d.compactedFallback(trigger, result.Notice, tokens))
	d.fanOut(ctx, d.compactionDone(trigger, 0, "", tokens))
}

func (d *sessionDriver) noticeRenders(notice contracts.Notice) bool {
	_, err := contracts.NewRenderer().Render(d.textLanguage()(), notice)
	return err == nil
}

type noticeDelivery struct {
	sink     contracts.NoticeSink
	conv     contracts.Conversation
	lang     languageSource
	notice   contracts.Notice
	index    int
	gateway  contracts.GatewaySet
	fallback func() contracts.Event
}

type relayedEvent struct {
	index   int
	gateway contracts.GatewaySet
	event   contracts.Event
}

func (d *sessionDriver) notifyGateways(notice contracts.Notice, fallback func() contracts.Event) {
	d.eachGateway(func(index int, gateway contracts.GatewaySet) {
		d.queueNotice(index, gateway, notice, fallback)
	})
}

func (d *sessionDriver) notifyBackendNotice(event contracts.Event) {
	code, err := strconv.ParseUint(event.Value, 10, 16)
	if err != nil {
		d.noticeLog.Warn("backend notice dropped, its code is not a number", keySession, d.name, "value", event.Value)
		return
	}
	d.notifyGateways(contracts.Notice{Code: contracts.Code(code)}, nil)
	d.tapEvent(d.stamp(event))
}

func (d *sessionDriver) queueNotice(index int, gateway contracts.GatewaySet, notice contracts.Notice, fallback func() contracts.Event) bool {
	sink, ok := gateway.Gateway.(contracts.NoticeSink)
	if !ok {
		return false
	}
	conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: d.renderChannel(gateway)}
	delivery := noticeDelivery{sink: sink, conv: conv, lang: d.textLanguage(), notice: notice, index: index, gateway: gateway, fallback: fallback}
	select {
	case d.notices <- delivery:
		return true
	default:
		d.noticeLog.Warn("notice dropped, the gateway's queue is full", keySession, d.name, "code", notice.Code, "gateway", conv.Gateway)
		return false
	}
}

func (d *sessionDriver) deliverNotices(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case delivery := <-d.notices:
			d.deliverNotice(ctx, delivery)
		}
	}
}

func (d *sessionDriver) deliverNotice(parent context.Context, delivery noticeDelivery) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), noticeTimeout)
	defer cancel()
	_, err := delivery.sink.Notify(ctx, delivery.conv, delivery.lang(), delivery.notice)
	if err == nil || errors.Is(err, contracts.ErrNoticeFallbackPosted) {
		return
	}
	if delivery.fallback == nil {
		d.noticeLog.Warn("notice not delivered", keySession, d.name, "code", delivery.notice.Code, "gateway", delivery.conv.Gateway, "err", err)
		return
	}
	logCleanup(err)
	d.relayNotice(parent, relayedEvent{index: delivery.index, gateway: delivery.gateway, event: delivery.fallback()})
}

func (d *sessionDriver) relayNotice(parent context.Context, relay relayedEvent) {
	select {
	case d.relays <- relay:
	case <-parent.Done():
	}
}

func (d *sessionDriver) relayEvent(ctx context.Context, relay relayedEvent) {
	if d.saidElsewhere {
		return
	}
	d.emitToGateway(ctx, relay.index, relay.gateway, d.stamp(relay.event))
}
