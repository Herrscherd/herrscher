package host

import (
	"context"
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/outbox"
	"github.com/Herrscherd/herrscher/core/usage"
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
	Resume      func(token string)
	Project     func(project string)
	Transcript  func(state.TranscriptEntry)
	Checkpoint  func(turnID, text string)
	Usage       func([]usage.Record)
	TurnOpened  func(turnID string)
	TurnSettled func()
}

type sessionDriver struct {
	name          string
	incarnation   string
	agent         string
	activeTurnID  string
	pickTurnID    string
	turn          chan struct{}
	running       atomic.Bool
	historyCalls  atomic.Uint64
	channel       string
	outboxRoot    string
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
	persists     *persistWrites
	seenMu       sync.Mutex
	seen         map[string]bool
	pendingReply chan string

	metrics *metrics.Registry

	coordinator contracts.Coordinator

	sink sessionSink

	emitTap func(contracts.Event)

	gate budgetGate

	outboundMaxBytes int64
	fileSenders      map[string]contracts.FileSender

	terms             func() modelTerms
	compaction        compactionPolicy
	percentOf         func() int
	turnReading       int
	hostSpent         bool
	stageSpent        map[string]bool
	compactMu         sync.Mutex
	compactWaiting    *compactRequest
	compactRegistered func()
	language          func() i18n.Language
	operator          languageSource
	warnings          io.Writer
	messages          *hostMessages
	cliMessages       *clitext.Messages
}

type queued struct {
	ev     contracts.Event
	origin contracts.Conversation
}

func newSessionDriver(messages *hostMessages, cliMessages *clitext.Messages, name string, gws []contracts.GatewaySet, toBridge chan<- contracts.Event, fromBridge <-chan contracts.Event, warnings io.Writer) *sessionDriver {
	return &sessionDriver{
		messages:    messages,
		cliMessages: cliMessages,
		warnings:    warnings,
		name:        name,
		gateways:    gws,
		toBridge:    toBridge,
		from:        fromBridge,
		queue:       make(chan queued, turnQueueCapacity),
		turn:        make(chan struct{}, 1),
		renderers:   make(map[string]*gatewayRenderer),
		hangup:      make(chan struct{}, 1),
		seen:        make(map[string]bool),
		stageSpent:  make(map[string]bool),
		notices:     make(chan noticeDelivery, noticeQueueCapacity),
		failures:    make(chan compactFailure, noticeQueueCapacity),
		noticeLog:   obs.Stderr(false),
		gate:        noBudgetGate{},
		persists:    newPersistWrites(),

		enqueueTimeout: defaultEnqueueTimeout,

		attachHosts: attachmentHosts(gws),
	}
}

func (d *sessionDriver) Pick(value string) bool {
	return d.enqueue(queued{ev: contracts.Event{T: eventwire.EventPick, Value: value}})
}

func (d *sessionDriver) enqueue(item queued) bool {
	select {
	case d.queue <- item:
		return true
	case <-time.After(d.enqueueTimeout):
		return false
	}
}

const interruptSendTimeout = 2 * time.Second

func (d *sessionDriver) Interrupt() {
	go func() {
		select {
		case d.toBridge <- contracts.Event{T: eventwire.EventInterrupt}:
		case <-time.After(interruptSendTimeout):
		}
	}()
}

func (d *sessionDriver) emitPaused(ctx context.Context, reason string) {
	d.fanOut(ctx, contracts.Event{T: eventwire.EventPaused, Text: reason})
}

func (d *sessionDriver) Seed(task string) bool {
	return d.enqueue(queued{ev: contracts.Event{T: eventwire.EventInput, Who: "handoff", Text: task, TurnID: newTurnID()}})
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
	case d.queue <- queued{ev: contracts.Event{T: eventwire.EventInput, Who: authorSeed, Text: task, TurnID: turnID}}:
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
		ev:     contracts.Event{T: eventwire.EventInput, Who: inbound.Author, Text: inbound.Text, Attachments: atts},
		origin: inbound.Conversation,
	}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (d *sessionDriver) decodeTurnFiles(event contracts.Event) outbox.Frame {
	frame, err := outbox.Decode(event.Value)
	if err != nil {
		slog.Warn("outbound files dropped", "err", err)
		return outbox.Frame{}
	}
	if frame.Dir != "" && !outbox.Owns(d.outboxRoot, frame.Dir) {
		slog.Warn("outbound files dropped, turn directory outside the outbox", "dir", frame.Dir)
		return outbox.Frame{}
	}
	return frame
}

func (d *sessionDriver) replaceTurnFiles(previous outbox.Frame, event contracts.Event) outbox.Frame {
	next := d.decodeTurnFiles(event)
	switch {
	case next.Dir == "":
		return previous
	case previous.Dir == "":
		return next
	case previous.Dir == next.Dir:
		next.Files = slices.Concat(previous.Files, next.Files)
		next.Refused = slices.Concat(previous.Refused, next.Refused)
		return next
	default:
		slog.Warn("earlier outbound files of the turn dropped", "dir", previous.Dir)
		d.removeTurnFiles(previous)
		return next
	}
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
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.report_refused", nil)})
		return &contracts.CoordinationEvent{
			Kind: "report_failed", SourceSession: d.name,
			Summary: sanitizeCoordinationError(localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err)),
		}
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.report_delivered", map[string]any{"Parent": parent})})
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
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.delegate_refused", nil)})
		return &contracts.CoordinationEvent{
			Kind: "delegate_failed", SourceSession: d.name,
			Agent: toAgent, Summary: sanitizeCoordinationError(localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err)),
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
			T:    eventwire.EventStatus,
			Text: d.hostText("coordination.fanout_partial", map[string]any{"Launched": len(spawned), detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()}),
		})
		return
	}
	d.fanOut(ctx, contracts.Event{
		T:    eventwire.EventStatus,
		Text: d.hostText("coordination.launched", map[string]any{"Count": len(spawned), "Workers": strings.Join(spawned, ", ")}),
	})
}

func (d *sessionDriver) routeTask(ctx context.Context, task string) {
	toAgent, session, err := d.coordinator.Route(ctx, contracts.RouteRequest{
		FromSession: d.name, Task: task,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.route_refused", map[string]any{detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()})})
		return
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.routed", map[string]any{"Agent": toAgent, "Session": session})})
}

func (d *sessionDriver) sealCohort(ctx context.Context, node int) {
	if _, err := d.coordinator.Seal(ctx, contracts.SealRequest{
		FromSession: d.name, Expected: node,
	}); err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.seal_refused", map[string]any{detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()})})
		return
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.sealed", map[string]any{"Node": node})})
}

func (d *sessionDriver) mergeWorker(ctx context.Context, worker string) {
	lead, err := d.coordinator.Merge(ctx, contracts.MergeRequest{
		FromSession: d.name, Worker: worker,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.merge_refused", map[string]any{detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()})})
		return
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.merged", map[string]any{"Lead": lead})})
}

func (d *sessionDriver) handoffTask(ctx context.Context, toAgent, task string) {
	if _, err := d.coordinator.Handoff(ctx, contracts.HandoffRequest{
		FromSession: d.name, ToAgent: toAgent, Task: task,
	}); err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.handoff_refused", map[string]any{detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()})})
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

func frameNames(files outbox.Frame) []string {
	return append(fileNames(files.Files), files.Refused...)
}

func fileSendersOf(gateways []contracts.Gateway) map[string]contracts.FileSender {
	senders := make(map[string]contracts.FileSender, len(gateways))
	for _, gateway := range gateways {
		if sender, ok := gateway.(contracts.FileSender); ok {
			senders[gateway.Manifest().Kind] = sender
		}
	}
	return senders
}

func (d *sessionDriver) noticeNames(names []string) string {
	labelled := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			name = d.hostText("outbound.unnamed", nil)
		}
		labelled = append(labelled, name)
	}
	return strings.Join(labelled, ", ")
}

func fileNames(files []contracts.OutboundFile) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
}

const (
	fileSendTimeout   = 2 * time.Minute
	namesParam        = "Names"
	filesPerSendBatch = 10
	bytesPerSendBatch = 9 << 20
	unmeasuredBytes   = bytesPerSendBatch + 1
)

func (d *sessionDriver) sendReplyFiles(ctx context.Context, index int, gateway contracts.GatewaySet, sender contracts.FileSender, files []contracts.OutboundFile) {
	batches := sendBatches(files, outboundSize)
	for position, batch := range batches {
		if ctx.Err() != nil {
			d.noticeUnsentFiles(ctx, index, gateway, slices.Concat(batches[position:]...))
			return
		}
		d.sendFileBatch(ctx, index, gateway, sender, batch)
	}
}

func (d *sessionDriver) noticeUnsentFiles(ctx context.Context, index int, gateway contracts.GatewaySet, files []contracts.OutboundFile) {
	notice := d.hostText("outbound.failed", map[string]any{namesParam: d.noticeNames(fileNames(files))})
	d.emitToGateway(ctx, index, gateway, d.stamp(contracts.Event{T: eventwire.EventStatus, Text: notice}))
}

func (d *sessionDriver) sendFileBatch(ctx context.Context, index int, gateway contracts.GatewaySet, sender contracts.FileSender, batch []contracts.OutboundFile) {
	conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: d.renderChannel(gateway)}
	sendCtx, cancel := context.WithTimeout(ctx, fileSendTimeout)
	defer cancel()
	if _, err := sender.SendFiles(sendCtx, conv, "", "", batch); err != nil {
		slog.Warn("reply files not sent", "gateway", conv.Gateway, "err", err)
		d.noticeUnsentFiles(ctx, index, gateway, batch)
	}
}

func sendBatches(files []contracts.OutboundFile, sizeOf func(contracts.OutboundFile) int64) [][]contracts.OutboundFile {
	var batches [][]contracts.OutboundFile
	var batch []contracts.OutboundFile
	var batchBytes int64
	for _, file := range files {
		size := sizeOf(file)
		if len(batch) > 0 && (len(batch) == filesPerSendBatch || batchBytes+size > bytesPerSendBatch) {
			batches = append(batches, batch)
			batch, batchBytes = nil, 0
		}
		batch = append(batch, file)
		batchBytes += size
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

func outboundSize(file contracts.OutboundFile) int64 {
	info, err := os.Stat(file.Path)
	if err != nil {
		slog.Warn("outbound file not measured", "file", file.Name, "err", err)
		return unmeasuredBytes
	}
	return info.Size()
}

func (d *sessionDriver) removeTurnFiles(files outbox.Frame) {
	if files.Dir == "" {
		return
	}
	if err := outbox.Remove(d.outboxRoot, files.Dir); err != nil {
		slog.Warn("turn outbox not removed", "err", err)
	}
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
	language    func() i18n.Language
	operator    languageSource
}

// RunSession drives a session without a declared identity until ctx is done.
func RunSession(ctx context.Context, sessions *sessionTable, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, sink sessionSink, gate budgetGate, warnings io.Writer) {
	runSessionIdentified(ctx, sessions, name, channel, gws, acc, participants, m, coord, sink, nil, sessionIdentity{}, gate, warnings)
}

func runSessionIdentified(ctx context.Context, sessions *sessionTable, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, registry *metrics.Registry, coord contracts.Coordinator, sink sessionSink, emit func(contracts.Event), identity sessionIdentity, gate budgetGate, warnings io.Writer) {
	defer closeRead(acc)
	toBridge := make(chan contracts.Event)
	fromBridge := make(chan contracts.Event)
	driver := newSessionDriver(sessions.messages, sessions.cliMessages, name, gws, toBridge, fromBridge, warnings)
	driver.persists = sessions.persists
	driver.incarnation = identity.incarnation
	driver.agent = identity.agent
	driver.channel = channel
	driver.outboxRoot = sessions.outboxRoot
	driver.outboundMaxBytes = sessions.outboundMaxBytes
	driver.fileSenders = sessions.fileSenders
	driver.participants = participants
	driver.metrics = registry
	driver.coordinator = coord
	driver.sink = sink
	driver.emitTap = emit
	driver.terms = identity.terms
	driver.language = identity.language
	driver.operator = identity.operator
	sessions.attachCompaction(driver, name)
	if gate != nil {
		driver.gate = gate
	}
	sessions.register(name, driver)
	driverCtx, stopDriver := context.WithCancel(ctx)
	driverDone := make(chan struct{})
	defer func() { <-driverDone }()
	defer sessions.unregister(name, driver)
	defer stopDriver()
	go func() {
		defer close(driverDone)
		driver.run(driverCtx)
	}()

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
