package host

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/bridge"
	control "github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

var pollInterval = 50 * time.Millisecond

var maxPollBackoff = 30 * time.Second

type budgetGate interface {
	Check(session string) (reason string, headroom uint64, capped bool)
}

type noBudgetGate struct{}

func (noBudgetGate) Check(string) (string, uint64, bool) { return "", 0, false }

type tokenGuard struct {
	headroom uint64
	capped   bool
	cut      bool
}

func (g *tokenGuard) trips(e contracts.Event) (uint64, bool) {
	if !g.capped || g.cut {
		return 0, false
	}
	spent := turnTokens(e)
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
}

type sessionDriver struct {
	name          string
	incarnation   string
	agent         string
	activeTurnID  string
	channel       string
	saidElsewhere bool
	gateways      []contracts.GatewaySet
	toBridge      chan<- contracts.Event
	from          <-chan contracts.Event
	queue         chan queued
	renderers     map[string]*gatewayRenderer

	hangup chan struct{}

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
}

type queued struct {
	ev     contracts.Event
	origin contracts.Conversation
}

func newSessionDriver(name string, gws []contracts.GatewaySet, toBridge chan<- contracts.Event, fromBridge <-chan contracts.Event) *sessionDriver {
	return &sessionDriver{
		name:      name,
		gateways:  gws,
		toBridge:  toBridge,
		from:      fromBridge,
		queue:     make(chan queued, 64),
		renderers: map[string]*gatewayRenderer{},
		hangup:    make(chan struct{}, 1),
		seen:      map[string]bool{},
		gate:      noBudgetGate{},

		attachHosts: attachmentHosts(gws),
	}
}

func attachmentHosts(gws []contracts.GatewaySet) map[string]bool {
	var hosts map[string]bool
	for _, g := range gws {
		if g.Gateway == nil {
			continue
		}
		for _, h := range g.Gateway.Manifest().AttachmentHosts {
			h = strings.ToLower(strings.TrimSpace(h))
			if h == "" {
				continue
			}
			if hosts == nil {
				hosts = map[string]bool{}
			}
			hosts[h] = true
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

func (d *sessionDriver) resolveAttachments(ctx context.Context, m contracts.Message) []string {
	if len(m.Attachments) == 0 {
		return nil
	}
	return bridge.ResolveAttachments(ctx, nil, m, d.name, d.attachHosts)
}

type turnUsage struct {
	InTokens, OutTokens, CacheRead, CacheCreate, DurMs int
	CtxTokens                                          int
}

func (d *sessionDriver) recordEntry(role, text string, cost float64, u turnUsage) {
	if d.sink.Transcript == nil || text == "" {
		return
	}
	d.sink.Transcript(state.TranscriptEntry{
		Ts:          time.Now().UTC().Format(time.RFC3339),
		Role:        role,
		Text:        text,
		Cost:        cost,
		TokensIn:    u.InTokens,
		TokensOut:   u.OutTokens,
		CacheRead:   u.CacheRead,
		CacheCreate: u.CacheCreate,
		DurMs:       u.DurMs,
		CtxTokens:   u.CtxTokens,
	})
}

func (d *sessionDriver) Pick(value string) bool {
	return d.enqueue(queued{ev: contracts.Event{T: "pick", Value: value}})
}

var enqueueTimeout = 2 * time.Second

func (d *sessionDriver) enqueue(q queued) bool {
	select {
	case d.queue <- q:
		return true
	case <-time.After(enqueueTimeout):
		return false
	}
}

func (d *sessionDriver) askApproval(ctx context.Context, prompt, id string) {
	opts := approvalChoices(id)
	for _, gs := range d.gateways {
		ch := d.renderChannel(gs)
		if mr, ok := gs.Reader.(contracts.MenuRouter); ok && ch != "" {
			if _, err := mr.RouteMenu(ctx, ch, "", prompt, d.name, opts); err == nil {
				continue
			}
		}
		if gs.Gateway == nil {
			continue
		}
		conv := contracts.Conversation{Gateway: gs.Gateway.Manifest().Kind, ID: ch}
		logCleanup(contracts.Degrade(gs.Gateway).Menu(ctx, conv, "", prompt+" (answer with `approve allow "+id+"`)", opts))
	}
}

func askApprovalOn(ctx context.Context, session, prompt, id string) {
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return
	}
	d.askApproval(ctx, prompt, id)
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
	return d.enqueue(queued{ev: contracts.Event{T: "input", Who: "handoff", Text: task, TurnID: newTurnID()}})
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
	case d.queue <- queued{ev: contracts.Event{T: "input", Who: "seed", Text: task, TurnID: turnID}}:
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

var sessionRegistry = struct {
	mu sync.Mutex
	m  map[string]*sessionDriver
}{m: map[string]*sessionDriver{}}

func registerDriver(name string, d *sessionDriver) {
	sessionRegistry.mu.Lock()
	sessionRegistry.m[name] = d
	sessionRegistry.mu.Unlock()
}

func unregisterDriver(name string, d *sessionDriver) {
	sessionRegistry.mu.Lock()
	if sessionRegistry.m[name] == d {
		delete(sessionRegistry.m, name)
	}
	sessionRegistry.mu.Unlock()
}

// Pick routes a select-menu value to the named session's driver, returning false
// when no live session by that name is driving.
//
// An approval answer is taken first, and by value rather than by session: it is
// answered by the daemon itself, must not reach the backend as a choice the
// model never posed, and must work for a session whose driver is busy inside the
// very turn that is waiting on it.
func Pick(session, value string) bool {
	if answerApprovalPick(session, value) {
		return true
	}
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return false
	}
	return d.Pick(value)
}

// Interrupt cancels the in-flight turn of the named session, returning false
// when no live session by that name is driving (mirror of Pick).
func Interrupt(session string) bool {
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return false
	}
	d.Interrupt()
	return true
}

// Seed routes an opening task to the named session's driver, returning false when
// no live session by that name is driving (mirror of Pick).
func Seed(session, task string) bool {
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return false
	}
	return d.Seed(task)
}

// Submit injects one inbound message into the named session's turn queue,
// returning false when no live session by that name is driving (mirror of Pick).
// It is the push counterpart of the driver's own poll loop: a gateway that
// receives messages by push calls this instead of being polled.
func Submit(ctx context.Context, session string, in contracts.Inbound) bool {
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return false
	}
	return d.submit(ctx, in)
}

func (d *sessionDriver) run(ctx context.Context) {
	for _, g := range d.gateways {
		if g.Reader != nil {
			go d.poll(ctx, g.Reader)
		}
	}
	d.pump(ctx)
}

func (d *sessionDriver) poll(ctx context.Context, r contracts.ChannelReader) {
	ch := d.channel
	if ch == "" {
		ch = r.DefaultChannel()
	}
	var last string
	log := obs.Stderr(false).With("component", "poll", "session", d.name, "channel", ch)
	if d.channel != "" {
		for delay := time.Second; ; delay = min(2*delay, 30*time.Second) {
			msgs, err := r.Read(ctx, ch, 1, "")
			if err == nil {
				if len(msgs) > 0 {
					last = msgs[len(msgs)-1].ID
				}
				break
			}
			log.Warn("cursor init failed; retrying rather than replaying history", "err", err, "retry_in", delay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}
	delay := pollInterval
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := r.Read(ctx, ch, 100, last)
		if err != nil {
			delay = min(2*delay, maxPollBackoff)
			log.Warn("poll read failed", "err", err, "retry_in", delay)
		} else {
			delay = pollInterval
			for _, m := range msgs {
				if m.AuthorBot {
					continue
				}
				last = m.ID
				if !d.submit(ctx, contracts.Inbound{
					Author:      m.AuthorName,
					AuthorID:    m.AuthorID,
					Text:        m.Content,
					Attachments: m.Attachments,
					MessageID:   contracts.MessageID(m.ID),
				}) {
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (d *sessionDriver) submit(ctx context.Context, in contracts.Inbound) bool {
	d.journal(in.AuthorID)
	atts := d.resolveAttachments(ctx, contracts.Message{
		ID:          string(in.MessageID),
		Content:     in.Text,
		AuthorID:    in.AuthorID,
		AuthorName:  in.Author,
		Attachments: in.Attachments,
	})
	select {
	case d.queue <- queued{
		ev:     contracts.Event{T: "input", Who: in.Author, Text: in.Text, Attachments: atts},
		origin: in.Conversation,
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
		case q := <-d.queue:
			ev := q.ev
			d.saidElsewhere = ev.T == "input" && outsideBoundGateways(q.origin, d.gateways)
			var guard tokenGuard
			if ev.T == "input" {
				reason, headroom, capped := d.gate.Check(d.name)
				if reason != "" {
					d.emitPaused(ctx, reason)
					d.fanOut(ctx, contracts.Event{T: "status", Text: "tour refusé, plafond budget atteint (" + reason + ") ; augmentez le plafond puis renvoyez le message"})
					continue
				}
				guard = tokenGuard{headroom: headroom, capped: capped}
				if ev.TurnID == "" {
					ev.TurnID = newTurnID()
				}
				ev.SessionIncarnation = d.incarnation
				ev.Agent = d.agent
				d.activeTurnID = ev.TurnID
				d.recordEntry("user", ev.Text, 0, turnUsage{})
				d.fanOut(ctx, contracts.Event{T: "human", Who: ev.Who, Text: ev.Text})
			} else {
				ev.SessionIncarnation = ""
				ev.TurnID = ""
				ev.Agent = ""
				d.activeTurnID = ""
			}
			d.runTurn(ctx, ev, guard)
			d.activeTurnID = ""
		}
	}
}

func (d *sessionDriver) runTurn(ctx context.Context, ev contracts.Event, guard tokenGuard) {
	select {
	case <-d.hangup:
	default:
	}
	select {
	case d.toBridge <- ev:
	case <-d.hangup:
		d.abandon(ctx, ev)
		return
	case <-ctx.Done():
		d.abandon(ctx, ev)
		return
	}
	if !d.awaitTurn(ctx, guard) {
		d.abandon(ctx, ev)
	}
}

func (d *sessionDriver) abandon(ctx context.Context, ev contracts.Event) {
	if ev.T != "input" {
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
		case e, ok := <-d.from:
			if !ok {
				d.metrics.TurnAbandoned()
				return false
			}
			if e.T == "reply" && e.Done {
				e.Coordination = d.maybeCoordinate(ctx, e.Text)
				d.fanOut(ctx, e)
				if d.sink.Resume != nil && e.Resume != "" {
					d.sink.Resume(e.Resume)
				}
				if d.sink.Project != nil && e.Project != "" {
					d.sink.Project(e.Project)
				}
				d.recordEntry("assistant", e.Text, e.Cost, turnUsage{
					InTokens:    e.TokensIn,
					OutTokens:   e.Tokens,
					CacheRead:   e.CacheRead,
					CacheCreate: e.CacheCreate,
					DurMs:       int(time.Since(turnStart).Milliseconds()),
					CtxTokens:   contextReading(live, e),
				})
				d.seenMu.Lock()
				if d.pendingReply != nil {
					d.pendingReply <- e.Text
					d.pendingReply = nil
				}
				d.seenMu.Unlock()
				d.metrics.TurnCompleted()
				if reason, _, _ := d.gate.Check(d.name); reason != "" {
					d.emitPaused(ctx, reason)
					return true
				}
				return true
			}
			d.fanOut(ctx, e)
			if n := promptSize(e); n > 0 {
				live = n
			}
			if spent, over := guard.trips(e); over {
				d.Interrupt()
				d.fanOut(ctx, contracts.Event{T: "status", Text: "tour interrompu, plafond de tokens atteint (" +
					strconv.FormatUint(spent, 10) + " tokens sur ce tour, marge " + strconv.FormatUint(guard.headroom, 10) + ")"})
			}
		}
	}
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

func turnTokens(e contracts.Event) uint64 {
	n := 0
	if e.Tokens > 0 {
		n += e.Tokens
	}
	if e.TokensIn > 0 {
		n += e.TokensIn
	}
	return uint64(n)
}

func (d *sessionDriver) maybeCoordinate(ctx context.Context, reply string) *contracts.CoordinationEvent {
	if d.coordinator == nil {
		return nil
	}
	if summary, ok := parseDone(reply); ok {
		if parent, err := d.coordinator.Report(ctx, contracts.ReportRequest{
			FromSession: d.name, Summary: summary,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "report refusé"})
			return &contracts.CoordinationEvent{
				Kind: "report_failed", SourceSession: d.name,
				Summary: sanitizeCoordinationError(err),
			}
		} else {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "rapport livré à " + parent})
			return &contracts.CoordinationEvent{
				Kind: "reported", SourceSession: d.name,
				ParentSession: parent, Summary: summary,
			}
		}
	}
	if toAgent, task, ok := parseDelegate(reply); ok {
		target, err := d.coordinator.Delegate(ctx, contracts.DelegateRequest{
			FromSession: d.name, ToAgent: toAgent, Task: task,
		})
		if err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "delegate refusé"})
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
	if toAgent, tasks, ok := parseFanOut(reply); ok {
		if spawned, err := d.coordinator.FanOut(ctx, contracts.FanOutRequest{
			FromSession: d.name, ToAgent: toAgent, Tasks: tasks,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{
				T:    "status",
				Text: "fanout partiel: " + strconv.Itoa(len(spawned)) + " lancés puis " + err.Error(),
			})
		} else {
			d.fanOut(ctx, contracts.Event{
				T:    "status",
				Text: "cohorte lancée : " + strconv.Itoa(len(spawned)) + " workers (" + strings.Join(spawned, ", ") + ")",
			})
		}
		return nil
	}
	if task, ok := parseRoute(reply); ok {
		if toAgent, session, err := d.coordinator.Route(ctx, contracts.RouteRequest{
			FromSession: d.name, Task: task,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "route refusé: " + err.Error()})
		} else {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "routé vers " + toAgent + " : " + session})
		}
		return nil
	}
	if n, ok := parseSeal(reply); ok {
		if _, err := d.coordinator.Seal(ctx, contracts.SealRequest{
			FromSession: d.name, Expected: n,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "seal refusé: " + err.Error()})
		} else {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "cohorte scellée à " + strconv.Itoa(n)})
		}
		return nil
	}
	if worker, ok := parseMerge(reply); ok {
		if lead, err := d.coordinator.Merge(ctx, contracts.MergeRequest{
			FromSession: d.name, Worker: worker,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "merge refusé: " + err.Error()})
		} else {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "merge traité pour " + lead})
		}
		return nil
	}
	if toAgent, task, ok := parseHandoff(reply); ok {
		if _, err := d.coordinator.Handoff(ctx, contracts.HandoffRequest{
			FromSession: d.name, ToAgent: toAgent, Task: task,
		}); err != nil {
			d.fanOut(ctx, contracts.Event{T: "status", Text: "handoff refusé: " + err.Error()})
		}
	}
	return nil
}

func sanitizeCoordinationError(err error) string {
	const max = 160
	s := strings.Join(strings.Fields(err.Error()), " ")
	if len(s) > max {
		s = s[:max]
	}
	return s
}

func (d *sessionDriver) fanOut(ctx context.Context, e contracts.Event) {
	if d.activeTurnID != "" {
		e.SessionIncarnation = d.incarnation
		e.TurnID = d.activeTurnID
		e.Agent = d.agent
	} else {
		e.SessionIncarnation = ""
		e.TurnID = ""
		e.Agent = ""
	}
	if d.emitTap != nil {
		d.emitTap(e)
	}
	if d.saidElsewhere {
		return
	}
	for i, g := range d.gateways {
		if rs, ok := g.Gateway.(contracts.RoutedEventSink); ok {
			rs.EmitTo(contracts.Conversation{
				Gateway: g.Gateway.Manifest().Kind,
				ID:      d.renderChannel(g),
			}, e)
			continue
		}
		if sink, ok := g.Gateway.(contracts.EventSink); ok {
			sink.Emit(e)
			continue
		}
		key := strconv.Itoa(i) + ":" + g.Gateway.Manifest().Kind
		r := d.renderers[key]
		if r == nil {
			r = newGatewayRenderer(g.Gateway, d.renderChannel(g))
			d.renderers[key] = r
		}
		r.handle(ctx, e)
	}
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

func gatewayChannel(g contracts.GatewaySet) string {
	if g.Reader != nil {
		return g.Reader.DefaultChannel()
	}
	return ""
}

func (d *sessionDriver) renderChannel(g contracts.GatewaySet) string {
	if d.channel != "" {
		return d.channel
	}
	return gatewayChannel(g)
}

type sessionIdentity struct {
	incarnation string
	agent       string
}

func RunSession(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, sink sessionSink, gate budgetGate) {
	runSessionIdentified(ctx, name, channel, gws, acc, participants, m, coord, sink, nil, sessionIdentity{}, gate)
}

func runSessionIdentified(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, sink sessionSink, emit func(contracts.Event), identity sessionIdentity, gate budgetGate) {
	defer closeRead(acc)
	toBridge := make(chan contracts.Event)
	fromBridge := make(chan contracts.Event)
	d := newSessionDriver(name, gws, toBridge, fromBridge)
	d.incarnation = identity.incarnation
	d.agent = identity.agent
	d.channel = channel
	d.participants = participants
	d.metrics = m
	d.coordinator = coord
	d.sink = sink
	d.emitTap = emit
	if gate != nil {
		d.gate = gate
	}
	registerDriver(name, d)
	defer unregisterDriver(name, d)
	go d.run(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case conn, ok := <-acc.Conns():
			if !ok {
				return
			}
			serveConn(ctx, conn, toBridge, fromBridge, d.hangup)
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
		logCleanup(conn.Scan(func(e contracts.Event) error {
			select {
			case fromBridge <- e:
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
		case ev := <-toBridge:
			if cctx.Err() != nil {
				return
			}
			if err := conn.Write(ev); err != nil {
				return
			}
		}
	}
}
