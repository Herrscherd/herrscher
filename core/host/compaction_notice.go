package host

import (
	"context"
	"errors"
	"strconv"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

func (d *sessionDriver) failCompaction(ctx context.Context, trigger contracts.CompactTrigger, err error) {
	var text string
	if !d.queueFailureNotice(trigger, err) {
		text = compactFailureText(d.messages, d.textLanguage()(), err)
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventCompactionFailed, Value: trigger.String(), Text: text})
}

func (d *sessionDriver) queueFailureNotice(trigger contracts.CompactTrigger, err error) bool {
	typed, ok := errors.AsType[contracts.NoticeError](err)
	if !ok || !d.noticeRenders(typed.Notice()) {
		return false
	}
	return d.notifyGateways(trigger, typed.Notice())
}

func compactFailureText(messages *hostMessages, lang i18n.Language, err error) string {
	if typed, ok := errors.AsType[contracts.NoticeError](err); ok {
		if text, renderErr := contracts.NewRenderer().Render(lang, typed.Notice()); renderErr == nil {
			return text
		}
	}
	if key, known := knownCompactFailure(err); known {
		return messages.text(lang, key, nil)
	}
	return messages.text(lang, "compaction.failed", map[string]any{detailParam: redact.Output([]byte(err.Error()))})
}

func knownCompactFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, errBridgeGone):
		return "compaction.bridge_gone", true
	case errors.Is(err, errNoCompactorFactory):
		return "compaction.no_compactor_factory", true
	case errors.Is(err, errNilCompactor):
		return "compaction.nil_compactor", true
	case errors.Is(err, errNoCompactorPlugin):
		return "compaction.no_compactor_plugin", true
	case errors.Is(err, historywire.ErrNoHistoryEditor):
		return "compaction.no_history_port", true
	case errors.Is(err, errHistoryTimeout):
		return "compaction.history_timeout", true
	case errors.Is(err, historywire.ErrHistoryTooLarge):
		return "compaction.history_too_large", true
	case errors.Is(err, errTooManyEdits):
		return "compaction.too_many_edits", true
	}
	return "", false
}

func (d *sessionDriver) announceCompaction(ctx context.Context, trigger contracts.CompactTrigger, result contracts.CompactResult) {
	if result.Notice.Code == 0 || !d.noticeRenders(result.Notice) {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventCompactionDone, Value: trigger.String(), Text: result.Note, Tokens: measuredTokens(result)})
		return
	}
	d.notifyGateways(trigger, result.Notice)
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
}

type compactFailure struct {
	trigger contracts.CompactTrigger
	text    string
}

func (d *sessionDriver) notifyGateways(trigger contracts.CompactTrigger, notice contracts.Notice) bool {
	return d.broadcastNotice(notice, trigger, &sync.Once{})
}

func (d *sessionDriver) notifyBackendNotice(event contracts.Event) {
	code, err := strconv.ParseUint(event.Value, 10, 16)
	if err != nil {
		d.noticeLog.Warn("backend notice dropped, its code is not a number", keySession, d.name, "value", event.Value)
		return
	}
	d.broadcastNotice(contracts.Notice{Code: contracts.Code(code)}, 0, nil)
}

func (d *sessionDriver) broadcastNotice(notice contracts.Notice, trigger contracts.CompactTrigger, report *sync.Once) bool {
	queued := false
	d.eachGateway(func(_ int, gateway contracts.GatewaySet) {
		sink, ok := gateway.Gateway.(contracts.NoticeSink)
		if !ok {
			return
		}
		conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: d.renderChannel(gateway)}
		delivery := noticeDelivery{sink: sink, conv: conv, lang: d.textLanguage(), notice: notice, trigger: trigger, report: report}
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
		d.noticeLog.Warn("compaction notice dropped, the gateway's queue is full", keySession, d.name, "code", delivery.notice.Code, "gateway", delivery.conv.Gateway)
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
		logCleanup(err)
		if delivery.report != nil {
			delivery.report.Do(func() { d.relayNoticeFailure(parent, d.noticeFailure(delivery)) })
		}
	}
}

func (d *sessionDriver) noticeFailure(delivery noticeDelivery) compactFailure {
	text, err := contracts.NewRenderer().Render(delivery.lang(), delivery.notice)
	if err != nil {
		text = compactFailureText(d.messages, delivery.lang(), err)
	}
	return compactFailure{trigger: delivery.trigger, text: text}
}

func (d *sessionDriver) relayNoticeFailure(parent context.Context, failure compactFailure) {
	select {
	case d.failures <- failure:
	case <-parent.Done():
	}
}
