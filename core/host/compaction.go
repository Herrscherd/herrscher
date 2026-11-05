package host

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	defaultCompactPercent = 92
	percentScale          = 100
	noticeTimeout         = 10 * time.Second
	noticeQueueCapacity   = 32
	eventCompactNow       = "compact_now"
	eventCompactStarted   = "compaction_started"
	eventCompactDone      = "compaction_done"
	eventCompactFailed    = "compaction_failed"
)

type compactUnavailableError struct {
	cause  error
	reason string
}

func (e compactUnavailableError) Error() string {
	return e.reason
}

func (e compactUnavailableError) Unwrap() error {
	return e.cause
}

func (e compactUnavailableError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeCompactUnavailable, Args: []contracts.Arg{contracts.Text("Reason", e.reason)}}
}

func unsupportedCompaction(session, vendor string) error {
	return compactUnavailableError{
		cause:  errCompactUnsupported,
		reason: fmt.Sprintf("session %q runs %q: its backend does not implement Compactor", session, vendor),
	}
}

func queueFullCompaction(session string) error {
	return compactUnavailableError{
		cause:  errCompactQueueFull,
		reason: fmt.Sprintf("session %q: the turn queue is full, the compaction was not queued", session),
	}
}

var (
	errCompactUnsupported = errors.New("its backend does not implement Compactor")
	errBridgeGone         = errors.New("the bridge closed during a compaction")
	errNoBackendFactory   = errors.New("declares Capabilities.Compactor but provides no backend factory")
	errNotCompactorPort   = errors.New("declares Capabilities.Compactor but does not implement the port")
	errNoCompactorPlugin  = errors.New("no plugin declares Capabilities.Compactor under this name")
	errCompactQueueFull   = errors.New("the turn queue is full, the compaction was not queued")
)

type compactionPolicy struct {
	percent  int
	limits   contextwindow.Limits
	names    []string
	plugins  map[string]contracts.Compactor
	backends map[string]bool
	warnOnce *sync.Once
	warn     func(format string, args ...any)
}

func newCompactionPolicy(ctx context.Context, percent *int, names []string, perVendor map[string]int) (compactionPolicy, error) {
	policy := compactionPolicy{
		percent:  defaultCompactPercent,
		names:    []string{contracts.CompactStageBackend},
		plugins:  make(map[string]contracts.Compactor),
		backends: make(map[string]bool),
		warnOnce: &sync.Once{},
		warn:     warnStderr,
	}
	if percent != nil {
		if err := contracts.ValidateAutoCompactPercent(*percent); err != nil {
			return compactionPolicy{}, fmt.Errorf("compactAt: %w", err)
		}
		policy.percent = *percent
	}
	if err := config.ValidateCompactStack(names); err != nil {
		return compactionPolicy{}, fmt.Errorf("compactStack: %w", err)
	}
	if len(names) > 0 {
		policy.names = names
	}
	warnInvalidContextLimit(policy.warn, os.Getenv(contextwindow.EnvVar()))
	policy.limits = contextwindow.NewLimits(os.Getenv(contextwindow.EnvVar()), perVendor)
	backends := contracts.Default.Backends()
	kinds := make([]string, 0, len(backends))
	for index := range backends {
		kind := backends[index].Manifest.Kind
		kinds = append(kinds, kind)
		if backends[index].Manifest.Capabilities.Compactor {
			policy.backends[kind] = true
		}
	}
	warnUnknownContextLimits(policy.warn, perVendor, kinds)
	for _, name := range policy.names {
		if name == contracts.CompactStageBackend {
			continue
		}
		compactor, err := customStage(ctx, name)
		if err != nil {
			return compactionPolicy{}, fmt.Errorf("compactStack %q: %w", name, err)
		}
		policy.plugins[name] = compactor
	}
	return policy, nil
}

func warnInvalidContextLimit(warn func(format string, args ...any), raw string) {
	if err := contextwindow.CheckOverride(raw); err != nil {
		warn("herrscher serve: %s ignored, %v: the vendor default window applies\n", contextwindow.EnvVar(), err)
	}
}

func warnUnknownContextLimits(warn func(format string, args ...any), perVendor map[string]int, kinds []string) {
	for _, vendor := range slices.Sorted(maps.Keys(perVendor)) {
		if !slices.ContainsFunc(kinds, func(kind string) bool { return strings.EqualFold(kind, vendor) }) {
			warn("herrscher serve: contextLimits: no backend %q in this daemon, its window size is unused\n", vendor)
		}
	}
}

func customStage(ctx context.Context, name string) (contracts.Compactor, error) {
	plugins := contracts.Default.Plugins()
	for index := range plugins {
		plugin := &plugins[index]
		if plugin.Manifest.Kind != name || !plugin.Manifest.Capabilities.Compactor {
			continue
		}
		if plugin.Backend == nil {
			return nil, errNoBackendFactory
		}
		backend, err := plugin.Backend(ctx, contracts.PluginConfig{})
		if err != nil {
			return nil, err
		}
		compactor, ok := backend.(contracts.Compactor)
		if !ok {
			return nil, errNotCompactorPort
		}
		return compactor, nil
	}
	return nil, errNoCompactorPlugin
}

func (p compactionPolicy) percentFor(sess state.Session) int {
	if sess.CompactPercent != nil {
		return *sess.CompactPercent
	}
	return p.percent
}

func (p compactionPolicy) supports(vendor string) bool {
	return p.backends[vendor]
}

func (p compactionPolicy) warnNoPort(vendor string) {
	p.warnOnce.Do(func() {
		p.warn("herrscher serve: backend %q declares Capabilities.Compactor but its instance does not implement the port, its compaction is refused\n", vendor)
	})
}

func (d *sessionDriver) compactPercent() int {
	if d.percentOf != nil {
		return d.percentOf()
	}
	return d.compaction.percent
}

func (d *sessionDriver) modelNow() modelTerms {
	if d.terms == nil {
		return modelTerms{}
	}
	return d.terms()
}

func (d *sessionDriver) contextLimit() int {
	now := d.modelNow()
	return d.compaction.limits.LimitFor(now.vendor, now.window)
}

func (d *sessionDriver) compactSupported() bool {
	return d.compaction.supports(d.modelNow().vendor)
}

func (d *sessionDriver) compactStages() ([]contracts.CompactStage, error) {
	if !d.compactSupported() {
		return nil, unsupportedCompaction(d.name, d.modelNow().vendor)
	}
	stages := make([]contracts.CompactStage, 0, len(d.compaction.names))
	for _, name := range d.compaction.names {
		if name == contracts.CompactStageBackend {
			stages = append(stages, contracts.CompactStage{Name: name, Compactor: bridgeCompactor{driver: d}})
			continue
		}
		stages = append(stages, contracts.CompactStage{Name: name, Compactor: d.compaction.plugins[name]})
	}
	return stages, nil
}

func crossed(used, limit, percent int) bool {
	return used*percentScale >= percent*limit
}

func (d *sessionDriver) maybeAutoCompact(ctx context.Context) {
	percent := d.compactPercent()
	limit := d.contextLimit()
	if percent == 0 || d.turnReading == 0 {
		return
	}
	if !crossed(d.turnReading, limit, percent) {
		d.compactSpent = false
		return
	}
	if d.compactSpent {
		return
	}
	d.compactSpent = true
	result, err := d.runCompaction(ctx, contracts.TriggerAutomatic, "")
	if err != nil || !result.TokensMeasured {
		return
	}
	if crossed(result.TokensAfter, limit, percent) {
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: d.hostText("compaction.no_effect", map[string]any{"Percent": percent})})
	}
}

func (d *sessionDriver) runCompaction(ctx context.Context, trigger contracts.CompactTrigger, instruction string) (contracts.CompactResult, error) {
	stages, err := d.compactStages()
	if err != nil {
		d.failCompaction(ctx, trigger, err)
		return contracts.CompactResult{}, err
	}
	limit := d.contextLimit()
	measured := d.turnReading > 0
	req := contracts.CompactRequest{
		Session:       d.sessionInfo(),
		Trigger:       trigger,
		UsageMeasured: measured,
		TokensUsed:    d.turnReading,
		TokenLimit:    limit,
		Instruction:   instruction,
	}
	d.fanOut(ctx, contracts.Event{T: eventCompactStarted, Value: trigger.String(), Tokens: d.turnReading})
	result, err := contracts.RunCompactStack(ctx, stages, req, d.compactionProgress(ctx))
	if err != nil {
		d.failCompaction(ctx, trigger, err)
		return contracts.CompactResult{}, err
	}
	d.settleCompaction(result, limit)
	d.announceCompaction(ctx, trigger, result)
	return result, nil
}

func (d *sessionDriver) failCompaction(ctx context.Context, trigger contracts.CompactTrigger, err error) {
	var text string
	if !d.queueFailureNotice(trigger, err) {
		text = compactFailureText(d.messages, d.lang, err)
	}
	d.fanOut(ctx, contracts.Event{T: eventCompactFailed, Value: trigger.String(), Text: text})
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
	return messages.text(lang, "compaction.failed", map[string]any{detailParam: err.Error()})
}

func knownCompactFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, errBridgeGone):
		return "compaction.bridge_gone", true
	case errors.Is(err, errNoBackendFactory):
		return "compaction.no_backend_factory", true
	case errors.Is(err, errNotCompactorPort):
		return "compaction.not_compactor_port", true
	case errors.Is(err, errNoCompactorPlugin):
		return "compaction.no_compactor_plugin", true
	}
	return "", false
}

func (d *sessionDriver) announceCompaction(ctx context.Context, trigger contracts.CompactTrigger, result contracts.CompactResult) {
	if result.Notice.Code == 0 || !d.noticeRenders(result.Notice) {
		d.fanOut(ctx, contracts.Event{T: eventCompactDone, Value: trigger.String(), Text: result.Note, Tokens: measuredTokens(result)})
		return
	}
	d.notifyGateways(trigger, result.Notice)
	d.fanOut(ctx, contracts.Event{T: eventCompactDone, Value: trigger.String(), Tokens: measuredTokens(result)})
}

func (d *sessionDriver) noticeRenders(notice contracts.Notice) bool {
	_, err := contracts.NewRenderer().Render(d.lang, notice)
	return err == nil
}

type noticeDelivery struct {
	sink    contracts.NoticeSink
	conv    contracts.Conversation
	lang    i18n.Language
	notice  contracts.Notice
	trigger contracts.CompactTrigger
	report  *sync.Once
}

type compactFailure struct {
	trigger contracts.CompactTrigger
	text    string
}

func (d *sessionDriver) notifyGateways(trigger contracts.CompactTrigger, notice contracts.Notice) bool {
	report := &sync.Once{}
	queued := false
	d.eachGateway(func(_ int, gateway contracts.GatewaySet) {
		sink, ok := gateway.Gateway.(contracts.NoticeSink)
		if !ok {
			return
		}
		conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: d.renderChannel(gateway)}
		delivery := noticeDelivery{sink: sink, conv: conv, lang: d.lang, notice: notice, trigger: trigger, report: report}
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
	if _, err := delivery.sink.Notify(ctx, delivery.conv, delivery.lang, delivery.notice); err != nil && !errors.Is(err, contracts.ErrNoticeFallbackPosted) {
		logCleanup(err)
		delivery.report.Do(func() { d.relayNoticeFailure(parent, d.noticeFailure(delivery)) })
	}
}

func (d *sessionDriver) noticeFailure(delivery noticeDelivery) compactFailure {
	text, err := contracts.NewRenderer().Render(delivery.lang, delivery.notice)
	if err != nil {
		text = compactFailureText(d.messages, delivery.lang, err)
	}
	return compactFailure{trigger: delivery.trigger, text: text}
}

func (d *sessionDriver) relayNoticeFailure(parent context.Context, failure compactFailure) {
	select {
	case d.failures <- failure:
	case <-parent.Done():
	}
}

func (d *sessionDriver) settleCompaction(result contracts.CompactResult, limit int) {
	if !result.TokensMeasured {
		return
	}
	d.turnReading = result.TokensAfter
	d.compactSpent = crossed(result.TokensAfter, limit, d.compactPercent())
}

func measuredTokens(result contracts.CompactResult) int {
	if !result.TokensMeasured {
		return 0
	}
	return result.TokensAfter
}

func (d *sessionDriver) compactionProgress(ctx context.Context) func(contracts.BackendEvent) {
	return func(event contracts.BackendEvent) {
		if event.Detail != "" {
			d.fanOut(ctx, contracts.Event{T: eventStatus, Text: event.Detail})
		}
	}
}

func (d *sessionDriver) sessionInfo() contracts.SessionInfo {
	return contracts.SessionInfo{Name: d.name, Incarnation: d.incarnation, Vendor: d.modelNow().vendor, AutoCompactPercent: d.compactPercent(), ContextWindow: d.contextLimit()}
}

func (d *sessionDriver) runCompactNow(ctx context.Context, instruction string) {
	if _, err := d.runCompaction(ctx, contracts.TriggerManual, instruction); err != nil {
		return
	}
}

type compactRequest struct {
	instruction string
	done        chan struct{}
	queued      bool
}

func (d *sessionDriver) requestCompaction(instruction string, origin contracts.Conversation) (bool, bool) {
	d.compactMu.Lock()
	if waiting := d.compactWaiting; waiting != nil {
		if instruction != "" {
			waiting.instruction = instruction
		}
		d.compactMu.Unlock()
		<-waiting.done
		return true, waiting.queued
	}
	request := &compactRequest{instruction: instruction, done: make(chan struct{})}
	d.compactWaiting = request
	d.compactMu.Unlock()
	if d.compactRegistered != nil {
		d.compactRegistered()
	}
	request.queued = d.enqueue(queued{ev: contracts.Event{T: eventCompactNow}, origin: origin})
	if !request.queued {
		d.clearCompactWaiting(request)
	}
	close(request.done)
	return false, request.queued
}

func (d *sessionDriver) clearCompactWaiting(request *compactRequest) {
	d.compactMu.Lock()
	defer d.compactMu.Unlock()
	if d.compactWaiting == request {
		d.compactWaiting = nil
	}
}

func (d *sessionDriver) takeCompactInstruction() string {
	d.compactMu.Lock()
	defer d.compactMu.Unlock()
	waiting := d.compactWaiting
	if waiting == nil {
		return ""
	}
	d.compactWaiting = nil
	return waiting.instruction
}

type bridgeCompactor struct {
	driver *sessionDriver
}

func (c bridgeCompactor) Compact(ctx context.Context, req contracts.CompactRequest, onEvent func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	return c.driver.compactThroughBridge(ctx, req, onEvent)
}

func (d *sessionDriver) decodeCompactResult(frame contracts.Event) (contracts.CompactResult, error) {
	res, err := compactwire.DecodeResult(frame)
	if errors.Is(err, compactwire.ErrNoticeMalformed) {
		warnStderr("herrscher serve: session %q: compaction notice dropped, its note and figures are kept: %v\n", d.name, err)
		return res, nil
	}
	return res, err
}

func (d *sessionDriver) compactThroughBridge(ctx context.Context, req contracts.CompactRequest, onEvent func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	frame, err := compactwire.Request(req)
	if err != nil {
		return contracts.CompactResult{}, err
	}
	select {
	case d.toBridge <- frame:
	case <-d.hangup:
		return contracts.CompactResult{}, errBridgeGone
	case <-ctx.Done():
		return contracts.CompactResult{}, ctx.Err()
	}
	for {
		select {
		case <-ctx.Done():
			d.Interrupt()
			return contracts.CompactResult{}, ctx.Err()
		case <-d.hangup:
			return contracts.CompactResult{}, errBridgeGone
		case frame, ok := <-d.from:
			if !ok {
				return contracts.CompactResult{}, errBridgeGone
			}
			switch frame.T {
			case compactwire.EventResult:
				return d.decodeCompactResult(frame)
			case compactwire.EventError:
				if frame.Value == compactwire.NoCompactor {
					d.compaction.warnNoPort(d.modelNow().vendor)
				}
				return contracts.CompactResult{}, compactwire.DecodeFailure(frame)
			case compactwire.EventProgress:
				onEvent(contracts.BackendEvent{Kind: "progress", Detail: frame.Text})
			}
		}
	}
}
