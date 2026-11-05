package host

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/checkpoint"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	pollInterval          = 50 * time.Millisecond
	maxPollBackoff        = 30 * time.Second
	defaultEnqueueTimeout = 2 * time.Second
)

type budgetGate interface {
	Check(session string) manager.BudgetCheck
}

type noBudgetGate struct{}

func (noBudgetGate) Check(string) manager.BudgetCheck { return manager.BudgetCheck{} }

type tokenGuard struct {
	headroom uint64
	capped   bool
	cut      bool
}

func (g *tokenGuard) trips(event contracts.Event) (uint64, bool) {
	if !g.capped || g.cut {
		return 0, false
	}
	spent := turnTokens(event)
	if spent == 0 || spent < g.headroom {
		return spent, false
	}
	g.cut = true
	return spent, true
}

type sessionSink struct {
	Resume     func(token string)
	Project    func(project string)
	Transcript func(state.TranscriptEntry)
	Checkpoint func(turnID, text string)
}

type sessionDriver struct {
	name          string
	incarnation   string
	agent         string
	activeTurnID  string
	turn          chan struct{}
	running       atomic.Bool
	channel       string
	saidElsewhere bool
	notices       chan noticeDelivery
	failures      chan compactFailure
	noticeLog     *slog.Logger
	gateways      []contracts.GatewaySet
	toBridge      chan<- contracts.Event
	from          <-chan contracts.Event
	queue         chan queued
	renderers     map[string]*gatewayRenderer
	grantMu       sync.Mutex
	grants        map[approval.Request]bool
	verdicts      map[approval.Request]approval.Risk

	hangup         chan struct{}
	enqueueTimeout time.Duration

	attachHosts map[string]bool

	participants string
	seenMu       sync.Mutex
	seen         map[string]bool
	pendingReply chan string

	metrics *metrics.Registry

	coordinator contracts.Coordinator

	sink sessionSink

	emitTap func(contracts.Event)

	gate budgetGate

	terms             func() modelTerms
	compaction        compactionPolicy
	percentOf         func() int
	turnReading       int
	compactSpent      bool
	compactMu         sync.Mutex
	compactWaiting    *compactRequest
	compactRegistered func()
	lang              i18n.Language
	language          func() i18n.Language
	messages          *hostMessages
}

type queued struct {
	ev     contracts.Event
	origin contracts.Conversation
}

func newSessionDriver(name string, gws []contracts.GatewaySet, toBridge chan<- contracts.Event, fromBridge <-chan contracts.Event) *sessionDriver {
	return &sessionDriver{
		messages:  newHostMessages(),
		name:      name,
		gateways:  gws,
		toBridge:  toBridge,
		from:      fromBridge,
		queue:     make(chan queued, turnQueueCapacity),
		turn:      make(chan struct{}, 1),
		renderers: make(map[string]*gatewayRenderer),
		hangup:    make(chan struct{}, 1),
		seen:      make(map[string]bool),
		notices:   make(chan noticeDelivery, noticeQueueCapacity),
		failures:  make(chan compactFailure, noticeQueueCapacity),
		noticeLog: obs.Stderr(false),
		gate:      noBudgetGate{},

		enqueueTimeout: defaultEnqueueTimeout,

		attachHosts: attachmentHosts(gws),
	}
}

func attachmentHosts(gws []contracts.GatewaySet) map[string]bool {
	var hosts map[string]bool
	for _, gateway := range gws {
		if gateway.Gateway == nil {
			continue
		}
		for _, attachmentHost := range gateway.Gateway.Manifest().AttachmentHosts {
			attachmentHost = strings.ToLower(strings.TrimSpace(attachmentHost))
			if attachmentHost == "" {
				continue
			}
			if hosts == nil {
				hosts = make(map[string]bool)
			}
			hosts[attachmentHost] = true
		}
	}
	return hosts
}

func (d *sessionDriver) journal(authorID string) {
	if d.participants == "" || authorID == "" {
		return
	}
	d.seenMu.Lock()
	first := !d.seen[authorID]
	d.seen[authorID] = true
	d.seenMu.Unlock()
	if first {
		_, appendErr := state.AppendParticipant(d.participants, authorID)
		logCleanup(appendErr)
	}
}

func (d *sessionDriver) resolveAttachments(ctx context.Context, message contracts.Message) []string {
	if len(message.Attachments) == 0 {
		return nil
	}
	return bridge.ResolveAttachments(ctx, nil, message, d.name, d.attachHosts)
}

type turnUsage struct {
	InTokens, OutTokens, CacheRead, CacheCreate, DurMs int
	CtxTokens                                          int
}

func (d *sessionDriver) recordEntry(role, text string, cost float64, usage turnUsage) {
	if d.sink.Transcript == nil || text == "" {
		return
	}
	d.sink.Transcript(state.TranscriptEntry{
		Ts:          time.Now().UTC().Format(time.RFC3339),
		Role:        role,
		Text:        text,
		Cost:        cost,
		TokensIn:    usage.InTokens,
		TokensOut:   usage.OutTokens,
		CacheRead:   usage.CacheRead,
		CacheCreate: usage.CacheCreate,
		DurMs:       usage.DurMs,
		CtxTokens:   usage.CtxTokens,
	})
}

func (d *sessionDriver) Pick(value string) bool {
	return d.enqueue(queued{ev: contracts.Event{T: "pick", Value: value}})
}

func (d *sessionDriver) enqueue(item queued) bool {
	select {
	case d.queue <- item:
		return true
	case <-time.After(d.enqueueTimeout):
		return false
	}
}

func (d *sessionDriver) remember(req approval.Request) {
	d.grantMu.Lock()
	defer d.grantMu.Unlock()
	if d.grants == nil {
		d.grants = make(map[approval.Request]bool)
	}
	d.grants[req] = true
}

func (d *sessionDriver) remembered(req approval.Request) bool {
	d.grantMu.Lock()
	defer d.grantMu.Unlock()
	return d.grants[req]
}

func (d *sessionDriver) verdict(req approval.Request) (approval.Risk, bool) {
	d.grantMu.Lock()
	defer d.grantMu.Unlock()
	risk, ok := d.verdicts[req]
	return risk, ok
}

func (d *sessionDriver) setVerdict(req approval.Request, risk approval.Risk) {
	d.grantMu.Lock()
	defer d.grantMu.Unlock()
	if d.verdicts == nil {
		d.verdicts = make(map[approval.Request]approval.Risk)
	}
	d.verdicts[req] = risk
}

func (d *sessionDriver) askApproval(ctx context.Context, prompt, approvalID string, req approval.Request, deadline time.Time) {
	opts := approvalChoices(approvalID, req)
	for _, gateway := range d.gateways {
		channel := d.renderChannel(gateway)
		if gateway.Gateway != nil {
			conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: channel}
			announceApprovalDeadline(gateway.Gateway, conv, approvalID, deadline)
			if risk, ok := d.verdict(req); ok {
				announceApprovalRisk(gateway.Gateway, conv, approvalID, risk)
			}
		}
		if mr, ok := gateway.Reader.(contracts.MenuRouter); ok && channel != "" {
			if _, err := mr.RouteMenu(ctx, channel, "", prompt, d.name, opts); err == nil {
				continue
			}
		}
		if gateway.Gateway == nil {
			continue
		}
		conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: channel}
		logCleanup(contracts.Degrade(gateway.Gateway).Menu(ctx, conv, "", prompt+" (answer with `approve allow|deny "+approvalID+"`, see `approve allow --help`)", opts))
	}
}

func announceApprovalDeadline(gateway contracts.Gateway, conv contracts.Conversation, approvalID string, deadline time.Time) {
	emitApprovalEvent(gateway, conv, contracts.Event{T: approvalEventKind, Value: approvalID, Text: deadline.UTC().Format(time.RFC3339)})
}

func announceApprovalRisk(gateway contracts.Gateway, conv contracts.Conversation, approvalID string, risk approval.Risk) {
	emitApprovalEvent(gateway, conv, contracts.Event{T: approvalRiskEventKind, Value: approvalID, Text: risk.String()})
}

func emitApprovalEvent(gateway contracts.Gateway, conv contracts.Conversation, event contracts.Event) {
	switch sink := gateway.(type) {
	case contracts.RoutedEventSink:
		sink.EmitTo(conv, event)
	case contracts.EventSink:
		sink.Emit(event)
	}
}

func askApprovalOn(ctx context.Context, sessions *sessionTable, session, prompt, approvalID string, req approval.Request, deadline time.Time) {
	driver := sessions.lookup(session)
	if driver == nil {
		return
	}
	driver.askApproval(ctx, prompt, approvalID, req, deadline)
}

const interruptSendTimeout = 2 * time.Second

func (d *sessionDriver) Interrupt() {
	go func() {
		select {
		case d.toBridge <- contracts.Event{T: "interrupt"}:
		case <-time.After(interruptSendTimeout):
		}
	}()
}

func (d *sessionDriver) emitPaused(ctx context.Context, reason string) {
	d.fanOut(ctx, contracts.Event{T: "paused", Text: reason})
}

func (d *sessionDriver) Seed(task string) bool {
	return d.enqueue(queued{ev: contracts.Event{T: eventInput, Who: "handoff", Text: task, TurnID: newTurnID()}})
}

func (d *sessionDriver) SeedAndWait(ctx context.Context, task string) (string, bool) {
	return d.SeedAndWaitWithTurnID(ctx, task, newTurnID())
}

func (d *sessionDriver) SeedAndWaitWithTurnID(ctx context.Context, task, turnID string) (string, bool) {
	reply := make(chan string, 1)
	d.seenMu.Lock()
	d.pendingReply = reply
	d.seenMu.Unlock()
	select {
	case d.queue <- queued{ev: contracts.Event{T: eventInput, Who: authorSeed, Text: task, TurnID: turnID}}:
	case <-ctx.Done():
		d.seenMu.Lock()
		d.pendingReply = nil
		d.seenMu.Unlock()
		return "", false
	}
	select {
	case r := <-reply:
		return r, true
	case <-ctx.Done():
		d.seenMu.Lock()
		d.pendingReply = nil
		d.seenMu.Unlock()
		select {
		case r := <-reply:
			return r, true
		default:
			return "", false
		}
	}
}

type sessionTable struct {
	mu          sync.Mutex
	m           map[string]*sessionDriver
	checkpoints checkpoint.Store
	compaction  compactionPolicy
	store       *state.State
}

func (t *sessionTable) percentOfSession(name string) int {
	if t.store != nil {
		if sess, ok := t.store.FindSession(name); ok {
			return t.compaction.percentFor(sess)
		}
	}
	return t.compaction.percent
}

func newSessionTable() *sessionTable {
	return &sessionTable{m: make(map[string]*sessionDriver)}
}

func (t *sessionTable) register(name string, driver *sessionDriver) {
	t.mu.Lock()
	t.m[name] = driver
	t.mu.Unlock()
}

func (t *sessionTable) unregister(name string, driver *sessionDriver) {
	t.mu.Lock()
	if t.m[name] == driver {
		delete(t.m, name)
	}
	t.mu.Unlock()
}

func (t *sessionTable) lookup(name string) *sessionDriver {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.m[name]
}

func (t *sessionTable) claimIdle(session string) (func(), bool) {
	driver := t.lookup(session)
	if driver == nil {
		return func() {}, true
	}
	if len(driver.queue) > 0 || !driver.claimTurn() {
		return nil, false
	}
	return driver.releaseClaim, true
}

func (t *sessionTable) pick(session, value string) bool {
	driver := t.lookup(session)
	if driver == nil {
		return false
	}
	return driver.Pick(value)
}

func (t *sessionTable) Interrupt(session string) bool {
	driver := t.lookup(session)
	if driver == nil {
		return false
	}
	driver.Interrupt()
	return true
}

func (t *sessionTable) Seed(session, task string) bool {
	driver := t.lookup(session)
	if driver == nil {
		return false
	}
	return driver.Seed(task)
}

func (t *sessionTable) Submit(ctx context.Context, session string, inbound contracts.Inbound) bool {
	driver := t.lookup(session)
	if driver == nil {
		return false
	}
	return driver.submit(ctx, inbound)
}

func (d *sessionDriver) run(ctx context.Context) {
	for _, gateway := range d.gateways {
		if gateway.Reader != nil {
			go d.poll(ctx, gateway.Reader)
		}
	}
	go d.deliverNotices(ctx)
	d.pump(ctx)
}

func (d *sessionDriver) poll(ctx context.Context, reader contracts.ChannelReader) {
	channel := d.channel
	if channel == "" {
		channel = reader.DefaultChannel()
	}
	log := obs.Stderr(false).With("component", "poll", keySession, d.name, "channel", channel)
	var last string
	if d.channel != "" {
		cursor, ok := initPollCursor(ctx, reader, channel, log)
		if !ok {
			return
		}
		last = cursor
	}
	d.pollLoop(ctx, reader, channel, last, log)
}

func initPollCursor(ctx context.Context, reader contracts.ChannelReader, channel string, log *slog.Logger) (string, bool) {
	for delay := time.Second; ; delay = min(backoffFactor*delay, maxPollBackoff) {
		msgs, err := reader.Read(ctx, channel, 1, "")
		if err == nil {
			return lastMessageID(msgs), true
		}
		log.Warn("cursor init failed; retrying rather than replaying history", "err", err, "retry_in", delay)
		if !sleepOrDone(ctx, delay) {
			return "", false
		}
	}
}

func lastMessageID(msgs []contracts.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	return msgs[len(msgs)-1].ID
}

func sleepOrDone(ctx context.Context, delay time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

func (d *sessionDriver) pollLoop(ctx context.Context, reader contracts.ChannelReader, channel, last string, log *slog.Logger) {
	delay := pollInterval
	for ctx.Err() == nil {
		msgs, err := reader.Read(ctx, channel, pollBatchSize, last)
		if err != nil {
			delay = min(backoffFactor*delay, maxPollBackoff)
			log.Warn("poll read failed", "err", err, "retry_in", delay)
		} else {
			delay = pollInterval
			var ok bool
			if last, ok = d.forwardPolled(ctx, msgs, last); !ok {
				return
			}
		}
		if !sleepOrDone(ctx, delay) {
			return
		}
	}
}

func (d *sessionDriver) forwardPolled(ctx context.Context, msgs []contracts.Message, last string) (string, bool) {
	for _, msg := range msgs {
		if msg.AuthorBot {
			continue
		}
		last = msg.ID
		if !d.submit(ctx, contracts.Inbound{
			Author:      msg.AuthorName,
			AuthorID:    msg.AuthorID,
			Text:        msg.Content,
			Attachments: msg.Attachments,
			MessageID:   contracts.MessageID(msg.ID),
		}) {
			return last, false
		}
	}
	return last, true
}

func (d *sessionDriver) submit(ctx context.Context, inbound contracts.Inbound) bool {
	d.journal(inbound.AuthorID)
	atts := d.resolveAttachments(ctx, contracts.Message{
		ID:          string(inbound.MessageID),
		Content:     inbound.Text,
		AuthorID:    inbound.AuthorID,
		AuthorName:  inbound.Author,
		Attachments: inbound.Attachments,
	})
	select {
	case d.queue <- queued{
		ev:     contracts.Event{T: eventInput, Who: inbound.Author, Text: inbound.Text, Attachments: atts},
		origin: inbound.Conversation,
	}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (d *sessionDriver) pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case failure := <-d.failures:
			d.fanOut(ctx, contracts.Event{T: eventCompactFailed, Value: failure.trigger.String(), Text: failure.text})
		case item := <-d.queue:
			if !d.enterTurn(ctx) {
				return
			}
			if item.ev.T == eventCompactNow {
				d.runCompactNow(ctx, d.takeCompactInstruction())
				d.releaseTurn()
				continue
			}
			event, guard, ok := d.prepareTurn(ctx, item)
			if !ok {
				d.releaseTurn()
				continue
			}
			d.turnReading = 0
			d.runTurn(ctx, event, guard)
			d.activeTurnID = ""
			d.maybeAutoCompact(ctx)
			d.releaseTurn()
		}
	}
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
	d.saidElsewhere = event.T == eventInput && outsideBoundGateways(item.origin, d.gateways)
	if event.T != eventInput {
		event.SessionIncarnation = ""
		event.TurnID = ""
		event.Agent = ""
		d.activeTurnID = ""
		return event, tokenGuard{}, true
	}
	verdict := d.gate.Check(d.name)
	if verdict.Reason != "" {
		d.emitPaused(ctx, verdict.Reason)
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: "tour refusé, plafond budget atteint (" + verdict.Reason + ") ; augmentez le plafond puis renvoyez le message"})
		return event, tokenGuard{}, false
	}
	if event.TurnID == "" {
		event.TurnID = newTurnID()
	}
	event.SessionIncarnation = d.incarnation
	event.Agent = d.agent
	d.activeTurnID = event.TurnID
	if d.sink.Checkpoint != nil {
		d.sink.Checkpoint(event.TurnID, event.Text)
	}
	d.recordEntry("user", event.Text, 0, turnUsage{})
	d.fanOut(ctx, contracts.Event{T: "human", Who: event.Who, Text: event.Text})
	return event, tokenGuard{headroom: verdict.Headroom, capped: verdict.Capped}, true
}

func (d *sessionDriver) runTurn(ctx context.Context, event contracts.Event, guard tokenGuard) {
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

func (d *sessionDriver) currentLanguage() i18n.Language {
	if d.language == nil {
		return 0
	}
	return d.language()
}

func (d *sessionDriver) announceLanguage(ctx context.Context) bool {
	lang := d.currentLanguage()
	if lang == 0 {
		return true
	}
	return d.sendToBridge(ctx, contracts.Event{T: eventLanguage, Value: lang.String()})
}

func (d *sessionDriver) abandon(ctx context.Context, ev contracts.Event) {
	if ev.T != eventInput {
		return
	}
	d.fanOut(ctx, contracts.Event{T: "abandoned"})
}

func (d *sessionDriver) awaitTurn(ctx context.Context, guard tokenGuard) bool {
	d.metrics.TurnStarted()
	turnStart := time.Now()
	live := 0
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
			if event.T == eventReply && event.Done {
				d.completeTurn(ctx, event, turnStart, live)
				return true
			}
			d.fanOut(ctx, event)
			if n := promptSize(event); n > 0 {
				live = n
			}
			d.enforceTokenGuard(ctx, &guard, event)
		}
	}
}

func (d *sessionDriver) completeTurn(ctx context.Context, event contracts.Event, turnStart time.Time, live int) {
	d.turnReading = contextReading(live, event)
	event.Coordination = d.maybeCoordinate(ctx, event.Text)
	event.Cost = estimateTurnCost(event.Cost, priceableTokens{
		uncachedInput: event.TokensIn,
		output:        event.Tokens,
		cacheRead:     event.CacheRead,
		cacheWrite:    event.CacheCreate,
	}, d.modelNow().rates)
	d.fanOut(ctx, event)
	if d.sink.Resume != nil && event.Resume != "" {
		d.sink.Resume(event.Resume)
	}
	if d.sink.Project != nil && event.Project != "" {
		d.sink.Project(event.Project)
	}
	d.recordEntry("assistant", event.Text, event.Cost, turnUsage{
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
	d.fanOut(ctx, contracts.Event{T: eventStatus, Text: "tour interrompu, plafond de tokens atteint (" +
		strconv.FormatUint(spent, 10) + " tokens sur ce tour, marge " + strconv.FormatUint(guard.headroom, 10) + ")"})
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

func (d *sessionDriver) maybeCoordinate(ctx context.Context, reply string) *contracts.CoordinationEvent {
	if d.coordinator == nil {
		return nil
	}
	if summary, ok := parseDone(reply); ok {
		return d.reportDone(ctx, summary)
	}
	if toAgent, task, ok := parseDelegate(reply); ok {
		return d.delegateTask(ctx, toAgent, task)
	}
	if toAgent, tasks, ok := parseFanOut(reply); ok {
		d.launchCohort(ctx, toAgent, tasks)
		return nil
	}
	if task, ok := parseRoute(reply); ok {
		d.routeTask(ctx, task)
		return nil
	}
	if n, ok := parseSeal(reply); ok {
		d.sealCohort(ctx, n)
		return nil
	}
	if worker, ok := parseMerge(reply); ok {
		d.mergeWorker(ctx, worker)
		return nil
	}
	if toAgent, task, ok := parseHandoff(reply); ok {
		d.handoffTask(ctx, toAgent, task)
	}
	return nil
}

func (d *sessionDriver) reportDone(ctx context.Context, summary string) *contracts.CoordinationEvent {
	parent, err := d.coordinator.Report(ctx, contracts.ReportRequest{
		FromSession: d.name, Summary: summary,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: "report refusé"})
		return &contracts.CoordinationEvent{
			Kind: "report_failed", SourceSession: d.name,
			Summary: sanitizeCoordinationError(err),
		}
	}
	d.fanOut(ctx, contracts.Event{T: eventStatus, Text: "rapport livré à " + parent})
	return &contracts.CoordinationEvent{
		Kind: "reported", SourceSession: d.name,
		ParentSession: parent, Summary: summary,
	}
}

func (d *sessionDriver) delegateTask(ctx context.Context, toAgent, task string) *contracts.CoordinationEvent {
	target, err := d.coordinator.Delegate(ctx, contracts.DelegateRequest{
		FromSession: d.name, ToAgent: toAgent, Task: task,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: "delegate refusé"})
		return &contracts.CoordinationEvent{
			Kind: "delegate_failed", SourceSession: d.name,
			Agent: toAgent, Summary: sanitizeCoordinationError(err),
		}
	}
	return &contracts.CoordinationEvent{
		Kind: "delegated", SourceSession: d.name,
		TargetSession: target, Agent: toAgent,
	}
}

func (d *sessionDriver) launchCohort(ctx context.Context, toAgent string, tasks []string) {
	spawned, err := d.coordinator.FanOut(ctx, contracts.FanOutRequest{
		FromSession: d.name, ToAgent: toAgent, Tasks: tasks,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{
			T:    eventStatus,
			Text: d.hostText("coordination.fanout_partial", map[string]any{"Launched": len(spawned), detailParam: err.Error()}),
		})
		return
	}
	d.fanOut(ctx, contracts.Event{
		T:    eventStatus,
		Text: d.hostText("coordination.launched", map[string]any{"Count": len(spawned), "Workers": strings.Join(spawned, ", ")}),
	})
}

func (d *sessionDriver) routeTask(ctx context.Context, task string) {
	toAgent, session, err := d.coordinator.Route(ctx, contracts.RouteRequest{
		FromSession: d.name, Task: task,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: d.hostText("coordination.route_refused", map[string]any{detailParam: err.Error()})})
		return
	}
	d.fanOut(ctx, contracts.Event{T: eventStatus, Text: d.hostText("coordination.routed", map[string]any{"Agent": toAgent, "Session": session})})
}

func (d *sessionDriver) sealCohort(ctx context.Context, node int) {
	if _, err := d.coordinator.Seal(ctx, contracts.SealRequest{
		FromSession: d.name, Expected: node,
	}); err != nil {
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: d.hostText("coordination.seal_refused", map[string]any{detailParam: err.Error()})})
		return
	}
	d.fanOut(ctx, contracts.Event{T: eventStatus, Text: d.hostText("coordination.sealed", map[string]any{"Node": node})})
}

func (d *sessionDriver) mergeWorker(ctx context.Context, worker string) {
	lead, err := d.coordinator.Merge(ctx, contracts.MergeRequest{
		FromSession: d.name, Worker: worker,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: d.hostText("coordination.merge_refused", map[string]any{detailParam: err.Error()})})
		return
	}
	d.fanOut(ctx, contracts.Event{T: eventStatus, Text: d.hostText("coordination.merged", map[string]any{"Lead": lead})})
}

func (d *sessionDriver) handoffTask(ctx context.Context, toAgent, task string) {
	if _, err := d.coordinator.Handoff(ctx, contracts.HandoffRequest{
		FromSession: d.name, ToAgent: toAgent, Task: task,
	}); err != nil {
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: d.hostText("coordination.handoff_refused", map[string]any{detailParam: err.Error()})})
	}
}

func sanitizeCoordinationError(err error) string {
	const maxLen = 160
	message := strings.Join(strings.Fields(err.Error()), " ")
	if len(message) <= maxLen {
		return message
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut]
}

func (d *sessionDriver) fanOut(ctx context.Context, event contracts.Event) {
	if d.activeTurnID != "" {
		event.SessionIncarnation = d.incarnation
		event.TurnID = d.activeTurnID
		event.Agent = d.agent
	} else {
		event.SessionIncarnation = ""
		event.TurnID = ""
		event.Agent = ""
	}
	if d.emitTap != nil {
		d.emitTap(event)
	}
	if d.saidElsewhere {
		return
	}
	d.eachGateway(func(index int, gateway contracts.GatewaySet) {
		d.emitToGateway(ctx, index, gateway, event)
	})
}

func (d *sessionDriver) eachGateway(visit func(index int, gateway contracts.GatewaySet)) {
	for index, gateway := range d.gateways {
		visit(index, gateway)
	}
}

func (d *sessionDriver) emitToGateway(ctx context.Context, index int, gateway contracts.GatewaySet, event contracts.Event) {
	if rs, ok := gateway.Gateway.(contracts.RoutedEventSink); ok {
		rs.EmitTo(contracts.Conversation{
			Gateway: gateway.Gateway.Manifest().Kind,
			ID:      d.renderChannel(gateway),
		}, event)
		return
	}
	if sink, ok := gateway.Gateway.(contracts.EventSink); ok {
		sink.Emit(event)
		return
	}
	key := strconv.Itoa(index) + ":" + gateway.Gateway.Manifest().Kind
	renderer := d.renderers[key]
	if renderer == nil {
		renderer = newGatewayRenderer(gateway.Gateway, d.renderChannel(gateway))
		d.renderers[key] = renderer
	}
	renderer.handle(ctx, event)
}

func outsideBoundGateways(origin contracts.Conversation, gws []contracts.GatewaySet) bool {
	if origin.Gateway == "" {
		return false
	}
	for _, g := range gws {
		if g.Gateway != nil && g.Gateway.Manifest().Kind == origin.Gateway {
			return false
		}
	}
	return true
}

func gatewayChannel(gateway contracts.GatewaySet) string {
	if gateway.Reader != nil {
		return gateway.Reader.DefaultChannel()
	}
	return ""
}

func (d *sessionDriver) renderChannel(gateway contracts.GatewaySet) string {
	if d.channel != "" {
		return d.channel
	}
	return gatewayChannel(gateway)
}

type sessionIdentity struct {
	incarnation string
	agent       string
	terms       func() modelTerms
	lang        i18n.Language
	language    func() i18n.Language
}

// RunSession drives a session without a declared identity until ctx is done.
func RunSession(ctx context.Context, sessions *sessionTable, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, sink sessionSink, gate budgetGate) {
	runSessionIdentified(ctx, sessions, name, channel, gws, acc, participants, m, coord, sink, nil, sessionIdentity{}, gate)
}

func runSessionIdentified(ctx context.Context, sessions *sessionTable, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, registry *metrics.Registry, coord contracts.Coordinator, sink sessionSink, emit func(contracts.Event), identity sessionIdentity, gate budgetGate) {
	defer closeRead(acc)
	toBridge := make(chan contracts.Event)
	fromBridge := make(chan contracts.Event)
	driver := newSessionDriver(name, gws, toBridge, fromBridge)
	driver.incarnation = identity.incarnation
	driver.agent = identity.agent
	driver.channel = channel
	driver.participants = participants
	driver.metrics = registry
	driver.coordinator = coord
	driver.sink = sink
	driver.emitTap = emit
	driver.terms = identity.terms
	driver.lang = identity.lang
	driver.language = identity.language
	driver.compaction = sessions.compaction
	driver.percentOf = func() int { return sessions.percentOfSession(name) }
	if gate != nil {
		driver.gate = gate
	}
	sessions.register(name, driver)
	defer sessions.unregister(name, driver)
	go driver.run(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case conn, ok := <-acc.Conns():
			if !ok {
				return
			}
			serveConn(ctx, conn, toBridge, fromBridge, driver.hangup)
		}
	}
}

func serveConn(ctx context.Context, conn *control.Conn, toBridge <-chan contracts.Event, fromBridge chan<- contracts.Event, hangup chan<- struct{}) {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer closeRead(conn)
	defer func() {
		select {
		case hangup <- struct{}{}:
		default:
		}
	}()

	go func() {
		logCleanup(conn.Scan(func(event contracts.Event) error {
			select {
			case fromBridge <- event:
				return nil
			case <-cctx.Done():
				return cctx.Err()
			}
		}))
		cancel()
	}()

	for {
		select {
		case <-cctx.Done():
			return
		case event := <-toBridge:
			if cctx.Err() != nil {
				return
			}
			if err := conn.Write(event); err != nil {
				return
			}
		}
	}
}
