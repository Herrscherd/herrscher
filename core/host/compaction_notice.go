package host

import (
	"context"
	"errors"
	"strconv"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

func (d *sessionDriver) failCompaction(ctx context.Context, trigger contracts.CompactTrigger, err error) {
	d.queueFailureNotice(trigger, err)
	d.fanOut(ctx, d.compactionFailure(trigger, err))
}

func (d *sessionDriver) queueFailureNotice(trigger contracts.CompactTrigger, err error) {
	typed, ok := errors.AsType[contracts.NoticeError](err)
	if !ok || !d.noticeRenders(typed.Notice()) {
		return
	}
	d.notifyGateways(trigger, typed.Notice(), true)
}

type relayedFailure struct {
	event  contracts.Event
	tapped bool
}

func (d *sessionDriver) compactionFailure(trigger contracts.CompactTrigger, err error) contracts.Event {
	payload := eventwire.CompactionFailure{Trigger: trigger.String()}
	typed, isNotice := errors.AsType[contracts.NoticeError](err)
	reason, known := knownCompactFailure(err)
	switch {
	case isNotice && d.noticeRenders(typed.Notice()):
		payload.Code = uint16(typed.Notice().Code)
	case known:
		payload.Reason = reason
	default:
		payload.Reason = eventwire.CompactReasonUnknown
		d.noticeLog.Warn("compaction failed", keySession, d.name, "trigger", trigger.String(), "err", redact.Output([]byte(err.Error())))
	}
	return d.encodedCompactionFailure(payload, "")
}

func (d *sessionDriver) encodedCompactionFailure(payload eventwire.CompactionFailure, text string) contracts.Event {
	value, err := payload.Encode()
	if err != nil {
		d.noticeLog.Warn("compaction failure not encoded", keySession, d.name, "err", err)
	}
	return contracts.Event{T: eventwire.EventCompactionFailed, Value: value, Text: text}
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
	if result.Notice.Code == 0 || !d.noticeRenders(result.Notice) {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventCompactionDone, Value: trigger.String(), Text: result.Note, Tokens: measuredTokens(result)})
		return
	}
	d.notifyGateways(trigger, result.Notice, false)
	d.fanOut(ctx, contracts.Event{T: eventwire.EventCompactionDone, Value: trigger.String(), Tokens: measuredTokens(result)})
}

func (d *sessionDriver) noticeRenders(notice contracts.Notice) bool {
	_, err := contracts.NewRenderer().Render(d.textLanguage()(), notice)
	return err == nil
}

type noticeDelivery struct {
	sink    contracts.NoticeSink
	conv    contracts.Conversation
	lang    languageSource
	notice  contracts.Notice
	trigger contracts.CompactTrigger
	report  *sync.Once
	tapped  bool
}

func (d *sessionDriver) notifyGateways(trigger contracts.CompactTrigger, notice contracts.Notice, tapped bool) bool {
	return d.broadcastNotice(notice, trigger, &sync.Once{}, tapped)
}

func (d *sessionDriver) notifyBackendNotice(event contracts.Event) {
	code, err := strconv.ParseUint(event.Value, 10, 16)
	if err != nil {
		d.noticeLog.Warn("backend notice dropped, its code is not a number", keySession, d.name, "value", event.Value)
		return
	}
	d.broadcastNotice(contracts.Notice{Code: contracts.Code(code)}, 0, nil, false)
	d.tapEvent(d.stamp(event))
}

func (d *sessionDriver) broadcastNotice(notice contracts.Notice, trigger contracts.CompactTrigger, report *sync.Once, tapped bool) bool {
	queued := false
	d.eachGateway(func(_ int, gateway contracts.GatewaySet) {
		sink, ok := gateway.Gateway.(contracts.NoticeSink)
		if !ok {
			return
		}
		conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: d.renderChannel(gateway)}
		delivery := noticeDelivery{sink: sink, conv: conv, lang: d.textLanguage(), notice: notice, trigger: trigger, report: report, tapped: tapped}
		if d.enqueueNotice(delivery) {
			queued = true
		}
	})
	return queued
}

func (d *sessionDriver) enqueueNotice(delivery noticeDelivery) bool {
	select {
	case d.notices <- delivery:
		return true
	default:
		d.noticeLog.Warn("notice dropped, the gateway's queue is full", keySession, d.name, "code", delivery.notice.Code, "gateway", delivery.conv.Gateway)
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
	if _, err := delivery.sink.Notify(ctx, delivery.conv, delivery.lang(), delivery.notice); err != nil && !errors.Is(err, contracts.ErrNoticeFallbackPosted) {
		if delivery.report == nil {
			d.noticeLog.Warn("notice not delivered", keySession, d.name, "code", delivery.notice.Code, "gateway", delivery.conv.Gateway, "err", err)
			return
		}
		logCleanup(err)
		delivery.report.Do(func() { d.relayNoticeFailure(parent, d.noticeFailure(delivery)) })
	}
}

func (d *sessionDriver) noticeFailure(delivery noticeDelivery) relayedFailure {
	payload := eventwire.CompactionFailure{Trigger: delivery.trigger.String(), Code: uint16(delivery.notice.Code)}
	text, err := contracts.NewRenderer().Render(delivery.lang(), delivery.notice)
	if err != nil {
		text = ""
	}
	return relayedFailure{event: d.encodedCompactionFailure(payload, text), tapped: delivery.tapped}
}

func (d *sessionDriver) relayNoticeFailure(parent context.Context, failure relayedFailure) {
	select {
	case d.failures <- failure:
	case <-parent.Done():
	}
}

func (d *sessionDriver) relayFailure(ctx context.Context, failure relayedFailure) {
	if !failure.tapped {
		d.tapEvent(d.stamp(failure.event))
	}
	if d.saidElsewhere {
		return
	}
	d.eachGateway(func(index int, gateway contracts.GatewaySet) {
		d.emitToGateway(ctx, index, gateway, d.stamp(failure.event))
	})
}
