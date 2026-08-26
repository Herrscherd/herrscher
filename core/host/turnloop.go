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

// pollInterval is how often the driver polls each bound gateway's Read for new
// inbound lines.
var pollInterval = 50 * time.Millisecond

// budgetGate answers, in one call, both budget questions the turn loop asks: the
// reason the session must pause ("" = it may run, and the gate has persisted the
// reason when it is not), and how many tokens the turn about to start may spend
// before a token cap trips (capped false = no token cap). One call rather than
// two because both answers come from the same transcript fold, and the loop asks
// them at the same instants. Injected so the turn loop can be tested without a
// live manager.
type budgetGate interface {
	Check(session string) (reason string, headroom uint64, capped bool)
}

// noBudgetGate is the default when no caps are configured.
type noBudgetGate struct{}

func (noBudgetGate) Check(string) (string, uint64, bool) { return "", 0, false }

// tokenGuard watches one turn's live token counter against the headroom read
// when the turn was dispatched, so a runaway turn is cut mid-flight instead of
// only being caught at a boundary it has already blown past. The zero value
// watches nothing — that is what a pick gets, and what a session with no token
// cap gets.
type tokenGuard struct {
	headroom uint64
	capped   bool
	cut      bool
}

// trips reports whether this event is the one that takes the turn past its
// headroom, and how much the turn has spent. It answers true at most once per
// turn: the cut is already under way after the first, and repeating it would
// flood the view with the same status.
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

// sessionDriver owns one session's turn lifecycle: it polls every bound
// gateway's Reader for inbound messages, serializes them through a FIFO, writes
// one input frame to the bridge per turn, and fans the bridge's reply events out
// to all bound gateways. toBridge/fromBridge are the two directions of the
// session's control connection (a *control.Conn in production; channels in
// tests).
// sessionSink collects the writes a completed turn owes the daemon: the
// backend's resume token, the memory project the session settled on, and the
// transcript entry. Any field may be nil, which disables that write.
type sessionSink struct {
	// Resume folds a completed turn's backend resume token into durable state so
	// a restart resumes the conversation instead of starting it over.
	Resume func(token string)
	// Project records the memory project the bridge settled on for this session,
	// once, on its first prompt.
	Project    func(project string)
	Transcript func(state.TranscriptEntry)
}

type sessionDriver struct {
	name         string
	incarnation  string
	agent        string
	activeTurnID string
	// channel is the session's own channel: the driver polls it and posts to it,
	// so each session uses its own channel rather than the gateway's global
	// default. Empty falls back to the reader's DefaultChannel (legacy/tests).
	channel string
	// saidElsewhere means the turn in flight was typed somewhere none of this
	// session's gateways can see, so fanOut keeps it off them. Decided once when the
	// frame is dequeued rather than per event, because every event of the turn
	// answers the same question. See outsideBoundGateways.
	saidElsewhere bool
	gateways      []contracts.GatewaySet
	toBridge      chan<- contracts.Event
	from          <-chan contracts.Event
	queue         chan queued
	renderers     map[string]*gatewayRenderer

	// hangup signals that the current connection ended so an in-flight turn is
	// abandoned and the FIFO resumes on reconnect. It is buffered (1) and written
	// non-blockingly by serveConn, so a disconnect while the driver is idle can
	// never wedge serveConn (and thus the reconnect accept loop).
	hangup chan struct{}

	// attachHosts is the SSRF allowlist for downloading a message's CDN image
	// attachments (nil = none allowed). file:// attachments — the terminal's
	// clipboard paste — bypass it; only https CDN urls are pinned to it. It is
	// built from the manifests of this session's own gateways, so the component
	// that produced an attachment url is the one that vouched for its host.
	attachHosts map[string]bool

	// participants is the journal path for /session who (empty = disabled). The
	// daemon owns gateway I/O now, so it records authors here as it polls them.
	participants string
	seenMu       sync.Mutex
	seen         map[string]bool
	// pendingReply receives the reply text of the next completed turn. It is
	// set by SeedAndWait before enqueueing a one-shot seed.
	pendingReply chan string

	// metrics records turn lifecycle counters (nil = no recording, e.g. in tests).
	metrics *metrics.Registry

	// coordinator is the Model-O coordination decision point: after a completed
	// turn, maybeCoordinate checks the reply for a done/delegate/handoff trailer
	// and, when present, forwards the request here. nil in the short-lived
	// operator CLI path and in tests that don't exercise coordination, where
	// maybeCoordinate simply no-ops.
	coordinator contracts.Coordinator

	// sink is where a completed turn's durable side effects go. The three travel
	// together because they are the same kind of fact — something about the turn
	// the daemon has to write down — and because a nil field is how a caller with
	// nowhere to write (tests, the short-lived operator CLI path) says so.
	sink sessionSink

	// emitTap, when set, receives every event the driver fans out — including on
	// the seed path where gateways is nil. It feeds the daemon's events socket so
	// an external reader (an attached terminal, Neublox) sees the live
	// thinking/status/chunk/reply stream. A driven session sets it too: without
	// it the socket carries nothing during an ordinary turn and every attached
	// frontend sits on `thinking…` until the process dies. nil = no tap
	// (CLI/tests).
	emitTap func(contracts.Event)

	// gate decides, after each completed turn, whether the session must pause on
	// a budget cap. Defaults to noBudgetGate{} (never trips) so behavior is
	// unchanged until a concrete gate is wired in.
	gate budgetGate
}

// queued is one frame waiting its turn, carrying where it came from. The origin
// travels beside the event rather than on it because contracts.Event is the wire
// format the bridge and every gateway read, and none of them has any business
// knowing which channel a turn was typed in — only fanOut does.
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

// attachmentHosts collects the CDN hosts this session's gateways vouch for. A
// gateway is the only thing that knows where its own attachments are served
// from, and it says so in its manifest; the driver reads it off the gateways it
// already holds rather than having a list threaded down from config.
//
// The result is the allowlist every https attachment download is pinned to, so
// an empty one means nothing is downloaded — which is what a session whose
// gateways declare no hosts (the terminal, whose clipboard pastes are file://
// and bypass this entirely) should get.
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

// journal records a message author in the participants journal (idempotent,
// best-effort), so /session who has a source now the bridge no longer journals.
func (d *sessionDriver) journal(authorID string) {
	if d.participants == "" || authorID == "" {
		return
	}
	d.seenMu.Lock()
	first := !d.seen[authorID]
	d.seen[authorID] = true
	d.seenMu.Unlock()
	if first {
		_, _ = state.AppendParticipant(d.participants, authorID)
	}
}

// resolveAttachments turns an inbound message's attachments into local image
// paths for the backend, resolved host-side (the bridge only sees Events). It is
// a no-op fast path when a message carries no attachments — the overwhelmingly
// common case — so an ordinary text turn pays nothing. file:// attachments (a
// terminal clipboard paste) pass through; CDN https attachments download through
// attachHosts (nil = none allowed, the safe default until a gateway wires its
// allowlist).
func (d *sessionDriver) resolveAttachments(ctx context.Context, m contracts.Message) []string {
	if len(m.Attachments) == 0 {
		return nil
	}
	return bridge.ResolveAttachments(ctx, nil, m, d.name, d.attachHosts)
}

// turnUsage carries the per-turn accounting persisted alongside an assistant
// entry. Zero value = a user turn (or a backend that reports no usage).
type turnUsage struct {
	InTokens, OutTokens, CacheRead, CacheCreate, DurMs int
}

// recordEntry appends one transcript turn-side, best-effort. Timestamp is set
// here so both call sites stay one-liners.
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
	})
}

// Pick injects a routed select-menu value into this session's turn queue. The
// bridge answers it out-of-band (serialized with turns) and emits a reply.
func (d *sessionDriver) Pick(value string) {
	d.queue <- queued{ev: contracts.Event{T: "pick", Value: value}}
}

// askApproval posts an approval question to every gateway bound to this
// session. A gateway that routes menus gets a real one, whose click comes back
// through Pick; anything else gets the degrading Menu, which renders text the
// operator answers with `approve allow <id>`.
//
// It does not touch the turn queue: an approval is asked BETWEEN the model's
// tool call and its execution, while the turn is already in flight.
func (d *sessionDriver) askApproval(ctx context.Context, prompt, id string) {
	opts := approvalChoices(id)
	for _, gs := range d.gateways {
		ch := d.renderChannel(gs)
		if mr, ok := gs.Reader.(contracts.MenuRouter); ok && ch != "" {
			if _, err := mr.RouteMenu(ctx, ch, "", prompt, d.name, opts); err == nil {
				continue
			}
			// A gateway that could not post its menu still owes the operator the
			// question: fall through to the text form rather than go silent.
		}
		if gs.Gateway == nil {
			continue
		}
		conv := contracts.Conversation{Gateway: gs.Gateway.Manifest().Kind, ID: ch}
		_ = contracts.Degrade(gs.Gateway).Menu(ctx, conv, "", prompt+" (answer with `approve allow "+id+"`)", opts)
	}
}

// askApprovalOn routes an approval question to the named session's gateways,
// doing nothing when no live session by that name is driving (mirror of Pick).
// A request nobody can see still waits out its timeout and is then refused,
// which is the same answer with a slower clock.
func askApprovalOn(ctx context.Context, session, prompt, id string) {
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return
	}
	d.askApproval(ctx, prompt, id)
}

// interruptSendTimeout bounds how long an out-of-band interrupt send waits for
// the connection writer before it is dropped (no bridge connected / no turn).
const interruptSendTimeout = 2 * time.Second

// Interrupt cancels the session's in-flight turn. Unlike Pick it bypasses the
// FIFO queue — the turn pump is blocked awaiting the current turn's reply, so
// the interrupt frame is written straight to the bridge connection (drained by
// serveConn's writer independently of the pump). The bridge cancels the running
// backend turn, which then emits its terminal reply and the turn ends. The send
// runs detached with a timeout so a call with no bridge connected can't block or
// leak the caller.
func (d *sessionDriver) Interrupt() {
	go func() {
		select {
		case d.toBridge <- contracts.Event{T: "interrupt"}:
		case <-time.After(interruptSendTimeout):
		}
	}()
}

// emitPaused records that the session halted on a budget cap. It fans a paused
// event out through the same path as every other subscriber-visible frame
// (fanOut -> emitTap + gateways), so LIVE subscribers (the app) see the pause
// immediately instead of only picking it up on the next status poll.
// Persistence of PausedReason is the gate's job (it already wrote it in Check
// before returning the reason). The backend bridge has no
// consumer for a "paused" frame (checked: nothing reads T=="paused" off
// fromBridge), so this does not also write to d.toBridge.
func (d *sessionDriver) emitPaused(ctx context.Context, reason string) {
	d.fanOut(ctx, contracts.Event{T: "paused", Text: reason})
}

// Seed injects an opening input turn into this session's FIFO. A handoff uses it
// to hand B its task the same way a human message would arrive.
func (d *sessionDriver) Seed(task string) {
	d.queue <- queued{ev: contracts.Event{T: "input", Who: "handoff", Text: task, TurnID: newTurnID()}}
}

// SeedAndWait injects an opening task and blocks until that turn's reply{done},
// returning its text. ok is false if the turn is abandoned.
func (d *sessionDriver) SeedAndWait(ctx context.Context, task string) (string, bool) {
	return d.SeedAndWaitWithTurnID(ctx, task, newTurnID())
}

// SeedAndWaitWithTurnID is SeedAndWait with a caller-supplied turn identity.
func (d *sessionDriver) SeedAndWaitWithTurnID(ctx context.Context, task, turnID string) (string, bool) {
	reply := make(chan string, 1)
	d.seenMu.Lock()
	d.pendingReply = reply
	d.seenMu.Unlock()
	d.queue <- queued{ev: contracts.Event{T: "input", Who: "seed", Text: task, TurnID: turnID}}
	select {
	case r := <-reply:
		return r, true
	case <-ctx.Done():
		d.seenMu.Lock()
		d.pendingReply = nil
		d.seenMu.Unlock()
		// The reply may have landed at the same instant ctx expired; select picks
		// a ready case at random, so drain once before declaring a timeout. reply
		// is buffered (cap 1) and awaitTurn nils pendingReply under seenMu, so this
		// is race-free: either the reply is already queued, or it never will be.
		select {
		case r := <-reply:
			return r, true
		default:
			return "", false
		}
	}
}

// sessionRegistry maps live session names to their driver so an out-of-band
// input — a routed select-menu pick — can reach the right session's FIFO. It is
// populated by RunSession for the session's lifetime.
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
	if answerApprovalPick(value) {
		return true
	}
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return false
	}
	d.Pick(value)
	return true
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
	d.Seed(task)
	return true
}

// Submit injects one inbound message into the named session's turn queue,
// returning false when no live session by that name is driving (mirror of Pick).
// It is the push counterpart of the driver's own poll loop: a gateway that
// receives messages by push calls this instead of being polled.
func Submit(session string, in contracts.Inbound) bool {
	sessionRegistry.mu.Lock()
	d := sessionRegistry.m[session]
	sessionRegistry.mu.Unlock()
	if d == nil {
		return false
	}
	return d.submit(context.Background(), in)
}

// run starts the pollers and the turn pump; it blocks until ctx is cancelled.
func (d *sessionDriver) run(ctx context.Context) {
	for _, g := range d.gateways {
		if g.Reader != nil {
			go d.poll(ctx, g.Reader)
		}
	}
	d.pump(ctx)
}

// poll reads one gateway's inbound messages and enqueues them as input frames.
func (d *sessionDriver) poll(ctx context.Context, r contracts.ChannelReader) {
	ch := d.channel
	if ch == "" {
		ch = r.DefaultChannel()
	}
	var last string
	// With a session channel bound, start after the current history so a daemon
	// restart doesn't replay past messages as fresh turns.
	//
	// An empty cursor means "read from the start of the channel", so a failed
	// first read must be retried rather than accepted: a restart that hits the
	// platform's rate limit would otherwise answer every message still in the
	// channel, weeks-old ones included. Only a successful read settles it — an
	// empty channel legitimately leaves the cursor empty, there is nothing to
	// skip past.
	if d.channel != "" {
		log := obs.Stderr(false).With("component", "poll", "session", d.name, "channel", ch)
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
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := r.Read(ctx, ch, 100, last)
		if err == nil {
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
		case <-time.After(pollInterval):
		}
	}
}

// submit records the author, resolves the message's attachments host-side, and
// enqueues one input frame. It is the single body behind both inbound paths: the
// poller (a gateway the core pulls from) and SessionControl.Submit (a gateway
// that pushes). It reports false only when ctx was cancelled while enqueueing,
// which is the poller's signal to stop.
//
// in.Conversation says where the message was typed. A pushing client is the only
// one that has to name it: what the poller reads it read from a channel this
// session is bound to by construction, so leaving it empty there says the same
// thing.
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

// pump dequeues one input at a time and runs it as a turn — this is the FIFO
// serialization: the next frame is not dequeued until the current turn ends.
func (d *sessionDriver) pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case q := <-d.queue:
			ev := q.ev
			// Where the frame was said decides, for every event this turn produces,
			// which channels render it. A frame that opens no turn of its own (a pick
			// answers one already rendered) names nowhere and so changes nothing.
			d.saidElsewhere = ev.T == "input" && outsideBoundGateways(q.origin, d.gateways)
			// A pick is answered out-of-band by the bridge; only a real input opens a
			// turn (and a progress view) on the bound gateways. Gate and watch only
			// that turn-opening path: an input that finds the session over budget is
			// refused here, before any turn opens, so a tripped cap actually stops
			// further spend. A pick answers a turn already rendered, so it keeps the
			// zero guard — blocking it, or cutting it mid-flight, would leave that
			// turn unanswered.
			var guard tokenGuard
			if ev.T == "input" {
				reason, headroom, capped := d.gate.Check(d.name)
				if reason != "" {
					d.emitPaused(ctx, reason)
					// The dequeued input is refused here, not re-queued: there is
					// no auto-replay path for a budget-pause resume (resume only
					// clears PausedReason; nothing re-injects this turn), so
					// pushing ev back onto d.queue would spin forever — dequeue,
					// still over budget, re-emit, re-queue. Instead, tell the
					// subscriber this specific input was dropped so it is not a
					// silent loss: the user must resend after raising the cap.
					d.fanOut(ctx, contracts.Event{T: "status", Text: "tour refusé — plafond budget atteint (" + reason + ") ; augmentez le plafond puis renvoyez le message"})
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

// runTurn hands one frame to the bridge and fans its events out until the turn's
// reply{done}. A stale hangup left by a disconnect that happened between turns is
// drained first so it cannot abort this turn; the send then races the hangup so a
// disconnect before the frame is handed off abandons the turn instead of blocking.
// A turn that ends without a reply (bridge disconnect or shutdown) emits an
// abstract "abandoned" signal so EventSink gateways can finalize.
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

// abandon fans an abstract "abandoned" signal for a real input turn that ended
// without a reply, so EventSink gateways can finalize their live acknowledgement
// (clear the pending marker, post nothing, whatever they choose). The host stays
// presentation-agnostic: it never picks an emoji or reaction — each gateway
// renders the signal however it wants. Picks carry no turn to abandon.
func (d *sessionDriver) abandon(ctx context.Context, ev contracts.Event) {
	if ev.T != "input" {
		return
	}
	d.fanOut(ctx, contracts.Event{T: "abandoned"})
}

// awaitTurn fans every event for the current turn to all bound gateways and
// returns true on reply{done}, or false when the turn is abandoned (ctx
// cancelled, the bridge closed, or a hangup signals a bridge disconnect). A
// backend "reset" is a mid-turn progress event: it is fanned out and the turn
// continues. guard watches the turn's live token counter and interrupts the turn
// the moment it would spend past its cap.
func (d *sessionDriver) awaitTurn(ctx context.Context, guard tokenGuard) bool {
	d.metrics.TurnStarted()
	turnStart := time.Now()
	for {
		select {
		case <-ctx.Done():
			d.metrics.TurnAbandoned()
			return false
		case <-d.hangup:
			d.metrics.TurnAbandoned()
			return false // bridge connection ended; abandon this turn
		case e, ok := <-d.from:
			if !ok {
				d.metrics.TurnAbandoned()
				return false // bridge connection lost; abandon this turn
			}
			if e.T == "reply" && e.Done {
				e.Coordination = d.maybeCoordinate(ctx, e.Text)
				d.fanOut(ctx, e)
				if d.sink.Resume != nil && e.Resume != "" {
					d.sink.Resume(e.Resume)
				}
				// The project a first prompt settled comes home the same way, and
				// for the same reason: the bridge knows it and only the daemon can
				// write it down.
				if d.sink.Project != nil && e.Project != "" {
					d.sink.Project(e.Project)
				}
				d.recordEntry("assistant", e.Text, e.Cost, turnUsage{
					InTokens:    e.TokensIn,
					OutTokens:   e.Tokens,
					CacheRead:   e.CacheRead,
					CacheCreate: e.CacheCreate,
					DurMs:       int(time.Since(turnStart).Milliseconds()),
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
					return true // stop this turn; the next dispatch is refused while paused
				}
				return true
			}
			d.fanOut(ctx, e)
			if spent, over := guard.trips(e); over {
				d.Interrupt()
				d.fanOut(ctx, contracts.Event{T: "status", Text: "tour interrompu — plafond de tokens atteint (" +
					strconv.FormatUint(spent, 10) + " tokens sur ce tour, marge " + strconv.FormatUint(guard.headroom, 10) + ")"})
			}
		}
	}
}

// turnTokens reads what an event says the running turn has spent so far. The
// bridge accumulates the backend's usage over the turn and stamps the running
// totals onto every rendered event, so this is a total, not a per-event delta —
// the caller compares it to the headroom directly instead of accumulating again.
// A backend that reports no usage (codex, cursor) leaves the counts at zero and
// never trips the guard: for those, caps stay boundary-only.
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

// maybeCoordinate runs the Model-O signal check after a completed turn: inspect
// the reply's trailer and, on a valid marker, hand the decision to the
// Coordinator. A single trailer per turn: done wins over delegate over fanout over route over seal over merge over handoff.
// A malformed marker is ignored; a coordinator refusal (unknown agent, dirty
// source, missing parent, create failure) is surfaced back into the session's
// channel as a status event — never a silent half-coordination.
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
			d.fanOut(ctx, contracts.Event{T: "status",
				Text: "fanout partiel: " + strconv.Itoa(len(spawned)) + " lancés puis " + err.Error()})
		} else {
			d.fanOut(ctx, contracts.Event{T: "status",
				Text: "cohorte lancée : " + strconv.Itoa(len(spawned)) + " workers (" + strings.Join(spawned, ", ") + ")"})
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

// fanOut delivers one turn event to every bound gateway: a gateway implementing
// RoutedEventSink renders routed events with conversation context; a gateway
// implementing EventSink renders the full stream itself (progress, emojis,
// acknowledgements, summary); a gateway that does not gets only the final reply
// posted through the Gateway port, chunked. All rich, platform-specific rendering
// lives in the gateway — the host only emits abstract semantic events.
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

// outsideBoundGateways answers whether a message was typed somewhere none of
// these gateways can see: a client that pushes over the command socket — an
// attached terminal, a frontend on the events socket — names the conversation it
// typed in, and when that is none of the session's own gateways it is already
// reading the whole stream through the event tap. Publishing there too would
// answer in a chat channel where nobody asked anything, which is what the
// operator sees as the agent replying in the place they just left.
//
// The turn is still one turn: it is recorded in the same transcript and the same
// history whatever channel rendered it. Only where it is *said* is scoped.
//
// A turn nobody typed in a conversation (a seed, a handoff, a script, or the
// poller, whose channel is bound by construction) names no origin, and the bound
// channels are then the only place it can be rendered.
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

// gatewayChannel returns the default channel for a gateway set, or "" when it
// has no reader.
func gatewayChannel(g contracts.GatewaySet) string {
	if g.Reader != nil {
		return g.Reader.DefaultChannel()
	}
	return ""
}

// renderChannel is where this session posts: its own bound channel when set,
// else the gateway's default (legacy/tests).
func (d *sessionDriver) renderChannel(g contracts.GatewaySet) string {
	if d.channel != "" {
		return d.channel
	}
	return gatewayChannel(g)
}

// RunSession drives one session against a control Acceptor: it bridges the
// persistent Conn (input frames out, event frames in) to a sessionDriver, and
// re-binds to a fresh Conn whenever the bridge reconnects (after a crash +
// supervisor restart). It blocks until ctx is cancelled. coord is the Model-O
// handoff coordinator (nil in the short-lived operator CLI path, where a
// completed turn's handoff trailer, if any, is simply ignored).
type sessionIdentity struct {
	incarnation string
	agent       string
}

func RunSession(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, sink sessionSink, gate budgetGate) {
	runSessionIdentified(ctx, name, channel, gws, acc, participants, m, coord, sink, nil, sessionIdentity{}, gate)
}

func runSessionIdentified(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, sink sessionSink, emit func(contracts.Event), identity sessionIdentity, gate budgetGate) {
	defer acc.Close() // own the acceptor: close the listener + remove the socket on shutdown
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

// serveConn shuttles frames between the driver and one bridge connection until
// the connection closes or ctx is cancelled. The reader goroutine forwards the
// bridge's events into fromBridge; the writer drains toBridge to the conn.
func serveConn(ctx context.Context, conn *control.Conn, toBridge <-chan contracts.Event, fromBridge chan<- contracts.Event, hangup chan<- struct{}) {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer conn.Close()
	// When this connection ends, tell the driver to abandon any in-flight turn so
	// the next queued input flows to the reconnecting bridge. The send is
	// non-blocking onto a buffered channel: the driver may be idle (no turn to
	// abandon), and blocking here would wedge the reconnect accept loop.
	defer func() {
		select {
		case hangup <- struct{}{}:
		default:
		}
	}()

	go func() {
		_ = conn.Scan(func(e contracts.Event) error {
			select {
			case fromBridge <- e:
				return nil
			case <-cctx.Done():
				return cctx.Err()
			}
		})
		cancel() // connection closed → unblock the writer and return to re-accept
	}()

	for {
		select {
		case <-cctx.Done():
			return
		case ev := <-toBridge:
			if cctx.Err() != nil {
				return // connection already dead; don't write into a dying conn
			}
			if err := conn.Write(ev); err != nil {
				return // write failed → connection dead, go re-accept
			}
		}
	}
}
