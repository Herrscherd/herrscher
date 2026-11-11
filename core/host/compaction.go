package host

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/internal/redact"
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
	errHistoryTimeout     = errors.New("the bridge did not answer a history call in time")
	errNoCompactorFactory = errors.New("is a compactor plugin but provides no Compactor factory")
	errNilCompactor       = errors.New("its Compactor factory returned no stage")
	errNoCompactorPlugin  = errors.New("no compactor plugin is registered under this name")
	errCompactorSettings  = errors.New("the compaction stage is missing required settings")
	errCompactQueueFull   = errors.New("the turn queue is full, the compaction was not queued")
)

type compactorSettingsError struct {
	missing *contracts.MissingSettingsError
}

func (e compactorSettingsError) Error() string {
	return e.render(i18n.English)
}

func (e compactorSettingsError) Unwrap() []error {
	return []error{errCompactorSettings, e.missing}
}

func (e compactorSettingsError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeSettingMissing, Args: []contracts.Arg{contracts.Text("Settings", strings.Join(e.missing.Missing, ", "))}}
}

func (e compactorSettingsError) render(lang i18n.Language) string {
	text, err := contracts.NewRenderer().Render(lang, e.Notice())
	if err != nil {
		return e.missing.Error()
	}
	return text
}

type compactionPolicy struct {
	percent        int
	limits         contextwindow.Limits
	names          []string
	plugins        map[string]contracts.Compactor
	triggers       map[string]int
	backends       map[string]bool
	historyEditors map[string]bool
	warnOnce       *sync.Once
	historyOnce    *sync.Once
	warn           func(key string, params map[string]any)
}

func newCompactionPolicy(ctx context.Context, lang languageSource, percent *int, names []string, perVendor map[string]int) (compactionPolicy, error) {
	policy := compactionPolicy{
		percent:        defaultCompactPercent,
		names:          []string{contracts.CompactStageBackend},
		plugins:        make(map[string]contracts.Compactor),
		triggers:       make(map[string]int),
		backends:       make(map[string]bool),
		historyEditors: make(map[string]bool),
		warnOnce:       &sync.Once{},
		historyOnce:    &sync.Once{},
		warn: func(key string, params map[string]any) {
			warnLive(lang, key, params)
		},
	}
	if percent != nil {
		if err := contracts.ValidateAutoCompactPercent(*percent); err != nil {
			return compactionPolicy{}, wrapHost(err, "compaction.at", nil)
		}
		policy.percent = *percent
	}
	if err := config.ValidateCompactStack(names); err != nil {
		return compactionPolicy{}, wrapHost(err, "compaction.stack", nil)
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
		capabilities := backends[index].Manifest.Capabilities
		if capabilities.Compactor {
			policy.backends[kind] = true
		}
		if capabilities.HistoryEditor {
			policy.historyEditors[kind] = true
		}
	}
	warnUnknownContextLimits(policy.warn, perVendor, kinds)
	for _, name := range policy.names {
		if name == contracts.CompactStageBackend {
			continue
		}
		if err := policy.addStage(ctx, name); err != nil {
			return compactionPolicy{}, err
		}
	}
	return policy, nil
}

func (p *compactionPolicy) addStage(ctx context.Context, name string) error {
	compactor, err := customStage(ctx, name, os.Getenv)
	if err != nil {
		return wrapHost(err, "compaction.stack_named", map[string]any{nameParam: name})
	}
	if trigger, ok := compactor.(contracts.StageTrigger); ok {
		percent := trigger.TriggerPercent()
		if err := contracts.ValidateStageTriggerPercent(percent); err != nil {
			return wrapHost(err, "compaction.stage_trigger", map[string]any{nameParam: name, "Percent": percent})
		}
		if percent != 0 {
			p.triggers[name] = percent
		}
	}
	p.plugins[name] = compactor
	return nil
}

func warnInvalidContextLimit(warn func(key string, params map[string]any), raw string) {
	if err := contextwindow.CheckOverride(raw); err != nil {
		warn("warn.context_limit_env", map[string]any{nameParam: contextwindow.EnvVar(), detailParam: err})
	}
}

func warnUnknownContextLimits(warn func(key string, params map[string]any), perVendor map[string]int, kinds []string) {
	for _, vendor := range slices.Sorted(maps.Keys(perVendor)) {
		if !slices.ContainsFunc(kinds, func(kind string) bool { return strings.EqualFold(kind, vendor) }) {
			warn("warn.context_limit_unknown_vendor", map[string]any{nameParam: vendor})
		}
	}
}

func customStage(ctx context.Context, name string, getenv func(string) string) (contracts.Compactor, error) {
	plugins := contracts.Default.Compactors()
	for index := range plugins {
		plugin := &plugins[index]
		if plugin.Manifest.Kind != name {
			continue
		}
		if plugin.Compactor == nil {
			return nil, errNoCompactorFactory
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, getenv)
		if missing, ok := errors.AsType[*contracts.MissingSettingsError](err); ok {
			return nil, compactorSettingsError{missing: missing}
		}
		if err != nil {
			return nil, err
		}
		compactor, err := plugin.Compactor(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if compactor == nil {
			return nil, errNilCompactor
		}
		return compactor, nil
	}
	return nil, errNoCompactorPlugin
}

func (p *compactionPolicy) percentFor(sess state.Session) int {
	if sess.CompactPercent != nil {
		return *sess.CompactPercent
	}
	return p.percent
}

func (p *compactionPolicy) supports(vendor string) bool {
	return p.backends[vendor]
}

func (p *compactionPolicy) editsHistory(vendor string) bool {
	return p.historyEditors[vendor]
}

func (p *compactionPolicy) warnNoHistoryPort(vendor string) {
	p.historyOnce.Do(func() {
		p.warn("warn.history_port_missing", map[string]any{nameParam: vendor})
	})
}

func (p *compactionPolicy) warnNoPort(vendor string) {
	p.warnOnce.Do(func() {
		p.warn("warn.compactor_port_missing", map[string]any{nameParam: vendor})
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

func (d *sessionDriver) compactStages(names []string, notes stageNotes) ([]contracts.CompactStage, error) {
	if !d.compactSupported() {
		return nil, unsupportedCompaction(d.name, d.modelNow().vendor)
	}
	stages := make([]contracts.CompactStage, 0, len(names))
	for _, name := range names {
		var compactor contracts.Compactor = bridgeCompactor{driver: d}
		if name != contracts.CompactStageBackend {
			compactor = historyStage{compactor: d.compaction.plugins[name], history: d.sessionHistory()}
		}
		stages = append(stages, contracts.CompactStage{Name: name, Compactor: recordedStage{name: name, compactor: compactor, notes: notes}})
	}
	return stages, nil
}

func (d *sessionDriver) sessionHistory() contracts.HistoryEditor {
	if !d.compaction.editsHistory(d.modelNow().vendor) {
		return nil
	}
	return bridgeHistory{driver: d}
}

type historyStage struct {
	compactor contracts.Compactor
	history   contracts.HistoryEditor
}

func (s historyStage) Compact(ctx context.Context, req contracts.CompactRequest, onEvent func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	if s.compactor == nil {
		return contracts.CompactResult{}, contracts.ErrNilCompactor
	}
	req.History = s.history
	return s.compactor.Compact(ctx, req, onEvent)
}

type recordedStage struct {
	name      string
	compactor contracts.Compactor
	notes     stageNotes
}

func (s recordedStage) Compact(ctx context.Context, req contracts.CompactRequest, onEvent func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	started := time.Now()
	result, err := s.compactor.Compact(ctx, req, onEvent)
	if errors.Is(err, contracts.ErrNilCompactor) {
		return result, err
	}
	s.notes.fail(s.name, err)
	if result.Stats != nil {
		stats := *result.Stats
		stats.Stage = s.name
		result.Stats = &stats
		return result, err
	}
	if err == nil {
		return result, nil
	}
	result.Stats = &contracts.CompactStats{Stage: s.name, Outcome: contracts.CompactFailed, Duration: time.Since(started)}
	return result, err
}

func crossed(used, limit, percent int) bool {
	return used*percentScale >= percent*limit
}

type compactionPass struct {
	names []string
	full  bool
}

func (p compactionPass) due() bool {
	return p.full || len(p.names) > 0
}

func (p *compactionPolicy) stageThreshold(name string, host int) int {
	own := p.triggers[name]
	if host == 0 || own == 0 || own >= host {
		return host
	}
	return own
}

func (d *sessionDriver) automaticRun(used, limit int) compactionPass {
	host := d.compactPercent()
	if host == 0 {
		return compactionPass{}
	}
	if !crossed(used, limit, host) {
		d.hostSpent = false
	} else if !d.hostSpent {
		return compactionPass{names: d.compaction.names, full: true}
	}
	var due []string
	for _, name := range d.compaction.names {
		threshold := d.compaction.stageThreshold(name, host)
		if threshold == host {
			continue
		}
		if !crossed(used, limit, threshold) {
			d.stageSpent[name] = false
			continue
		}
		if !d.stageSpent[name] {
			due = append(due, name)
		}
	}
	return compactionPass{names: due}
}

func (d *sessionDriver) markSpent(run compactionPass) {
	if run.full {
		d.hostSpent = true
	}
	for _, name := range run.names {
		d.stageSpent[name] = true
	}
}

func (d *sessionDriver) maybeAutoCompact(ctx context.Context) {
	if d.turnReading == 0 {
		return
	}
	limit := d.contextLimit()
	run := d.automaticRun(d.turnReading, limit)
	if !run.due() {
		return
	}
	settled := d.runAutomatic(ctx, run)
	if !settled || run.full || d.hostSpent || !crossed(d.turnReading, limit, d.compactPercent()) {
		return
	}
	rest := stagesAfter(d.compaction.names, run.names)
	if len(rest) == 0 {
		return
	}
	d.runAutomatic(ctx, compactionPass{names: rest, full: true})
}

func stagesAfter(stack, ran []string) []string {
	rest := make([]string, 0, len(stack))
	for _, name := range stack {
		if !slices.Contains(ran, name) {
			rest = append(rest, name)
		}
	}
	return rest
}

func (d *sessionDriver) runAutomatic(ctx context.Context, run compactionPass) bool {
	d.markSpent(run)
	result, err := d.runCompaction(ctx, contracts.TriggerAutomatic, "", run)
	if err != nil || !result.TokensMeasured {
		return false
	}
	if percent := d.compactPercent(); run.full && crossed(result.TokensAfter, d.contextLimit(), percent) {
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: d.hostText("compaction.no_effect", map[string]any{"Percent": percent})})
	}
	return true
}

func (d *sessionDriver) runCompaction(ctx context.Context, trigger contracts.CompactTrigger, instruction string, run compactionPass) (contracts.CompactResult, error) {
	notes := make(stageNotes)
	stages, err := d.compactStages(run.names, notes)
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
	d.recordCompaction(result.StageStats, notes)
	if err != nil {
		d.failCompaction(ctx, trigger, err)
		return contracts.CompactResult{}, err
	}
	d.settleCompaction(result, limit, run)
	d.announceCompaction(ctx, trigger, result)
	return result, nil
}

func (d *sessionDriver) failCompaction(ctx context.Context, trigger contracts.CompactTrigger, err error) {
	var text string
	if !d.queueFailureNotice(trigger, err) {
		text = compactFailureText(d.messages, d.textLanguage()(), err)
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
	report := &sync.Once{}
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
		delivery.report.Do(func() { d.relayNoticeFailure(parent, d.noticeFailure(delivery)) })
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

func (d *sessionDriver) settleCompaction(result contracts.CompactResult, limit int, run compactionPass) {
	if !result.TokensMeasured {
		return
	}
	d.turnReading = result.TokensAfter
	host := d.compactPercent()
	if run.full {
		d.hostSpent = crossed(result.TokensAfter, limit, host)
	}
	for _, name := range run.names {
		d.stageSpent[name] = crossed(result.TokensAfter, limit, d.compaction.stageThreshold(name, host))
	}
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
	return contracts.SessionInfo{Name: d.name, Incarnation: d.incarnation, Vendor: d.modelNow().vendor, AutoCompactPercent: d.compactPercent(), ContextWindow: d.contextLimit(), Language: d.currentLanguage()}
}

func (d *sessionDriver) runCompactNow(ctx context.Context, instruction string) {
	if _, err := d.runCompaction(ctx, contracts.TriggerManual, instruction, compactionPass{names: d.compaction.names, full: true}); err != nil {
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
		warnTo(d.warnings, d.textLanguage()(), "warn.session_compaction_notice", map[string]any{nameParam: d.name, detailParam: err})
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

const historyCallTimeout = 2 * time.Minute

type bridgeHistory struct {
	driver *sessionDriver
}

func (h bridgeHistory) ReadHistory(ctx context.Context) (contracts.History, error) {
	var collector historywire.Collector
	err := h.driver.historyThroughBridge(ctx, historywire.Read(h.driver.nextHistoryCall()), func(frame contracts.Event) (bool, error) {
		if frame.T != historywire.EventHistory {
			return false, nil
		}
		return collector.Add(frame)
	})
	if err != nil {
		return contracts.History{}, err
	}
	return collector.History()
}

func (h bridgeHistory) PruneHistory(ctx context.Context, edits []contracts.HistoryEdit) (contracts.PruneOutcome, error) {
	if err := contracts.ValidateHistoryEdits(edits); err != nil {
		return contracts.PruneOutcome{}, err
	}
	var total contracts.PruneOutcome
	for start := 0; start < len(edits) || start == 0; start += historywire.PruneBatch {
		outcome, err := h.pruneBatch(ctx, edits[start:min(start+historywire.PruneBatch, len(edits))])
		if err != nil {
			return total, err
		}
		if start == 0 {
			total.CharsBefore = outcome.CharsBefore
		}
		total.Edited += outcome.Edited
		total.CharsAfter = outcome.CharsAfter
	}
	return total, nil
}

func (h bridgeHistory) pruneBatch(ctx context.Context, edits []contracts.HistoryEdit) (contracts.PruneOutcome, error) {
	request, err := historywire.Prune(h.driver.nextHistoryCall(), edits)
	if err != nil {
		return contracts.PruneOutcome{}, err
	}
	var outcome contracts.PruneOutcome
	err = h.driver.historyThroughBridge(ctx, request, func(frame contracts.Event) (bool, error) {
		if frame.T != historywire.EventPruned {
			return false, nil
		}
		var decodeErr error
		outcome, decodeErr = historywire.DecodePruned(frame)
		return true, decodeErr
	})
	return outcome, err
}

func (d *sessionDriver) nextHistoryCall() string {
	return strconv.FormatUint(d.historyCalls.Add(1), 36)
}

func (d *sessionDriver) historyThroughBridge(ctx context.Context, request contracts.Event, accept func(contracts.Event) (bool, error)) error {
	ctx, cancel := context.WithTimeoutCause(ctx, historyCallTimeout, errHistoryTimeout)
	defer cancel()
	select {
	case d.toBridge <- request:
	case <-d.hangup:
		return errBridgeGone
	case <-ctx.Done():
		return historyCallEnded(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			d.Interrupt()
			return historyCallEnded(ctx)
		case <-d.hangup:
			return errBridgeGone
		case frame, ok := <-d.from:
			if !ok {
				return errBridgeGone
			}
			if done, err := d.historyAnswer(request, frame, accept); err != nil || done {
				return err
			}
		}
	}
}

func historyCallEnded(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), errHistoryTimeout) {
		return fmt.Errorf("%w: %w", errHistoryTimeout, ctx.Err())
	}
	return ctx.Err()
}

func (d *sessionDriver) historyAnswer(request, frame contracts.Event, accept func(contracts.Event) (bool, error)) (bool, error) {
	if !historywire.IsFrame(frame.T) || frame.Who != request.Who {
		return false, nil
	}
	if frame.T != historywire.EventError {
		return accept(frame)
	}
	if historywire.CodeOf(frame) == historywire.FailureNoEditor {
		d.compaction.warnNoHistoryPort(d.modelNow().vendor)
	}
	return true, historywire.DecodeFailure(frame)
}
