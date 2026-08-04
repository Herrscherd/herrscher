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

// budgetGate answers the two budget questions the turn loop asks: at a turn
// boundary, whether the session has exhausted its budget (persisting the paused
// reason when it has), and at turn start, how much token headroom the turn has.
// Injected so the turn loop can be tested without a live manager.
type budgetGate interface {
	// CheckAfterTurn returns the paused reason ("" = continue).
	CheckAfterTurn(session string) string
	// TokenHeadroom returns how many tokens the turn may spend before a token cap
	// trips, and whether any token cap applies. Read once per turn: re-deriving it
	// per event would re-read the transcript hundreds of times a turn.
	TokenHeadroom(session string) (uint64, bool)
}

// noBudgetGate is the default when no caps are configured.
type noBudgetGate struct{}

func (noBudgetGate) CheckAfterTurn(string) string        { return "" }
func (noBudgetGate) TokenHeadroom(string) (uint64, bool) { return 0, false }

// sessionDriver owns one session's turn lifecycle: it polls every bound
// gateway's Reader for inbound messages, serializes them through a FIFO, writes
// one input frame to the bridge per turn, and fans the bridge's reply events out
// to all bound gateways. toBridge/fromBridge are the two directions of the
// session's control connection (a *control.Conn in production; channels in
// tests).
type sessionDriver struct {
	name         string
	incarnation  string
	agent        string
	activeTurnID string
	// channel is the session's own channel: the driver polls it and posts to it,
	// so each session uses its own channel rather than the gateway's global
	// default. Empty falls back to the reader's DefaultChannel (legacy/tests).
	channel   string
	gateways  []contracts.GatewaySet
	toBridge  chan<- contracts.Event
	from      <-chan contracts.Event
	queue     chan contracts.Event
	renderers map[string]*gatewayRenderer

	// hangup signals that the current connection ended so an in-flight turn is
	// abandoned and the FIFO resumes on reconnect. It is buffered (1) and written
	// non-blockingly by serveConn, so a disconnect while the driver is idle can
	// never wedge serveConn (and thus the reconnect accept loop).
	hangup chan struct{}

	// attachHosts is the SSRF allowlist for downloading a message's CDN image
	// attachments (nil = none allowed). file:// attachments — the terminal's
	// clipboard paste — bypass it; only https CDN urls are pinned to it.
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

	// persistResume folds a completed turn's backend resume token into durable
	// state (nil = disabled, e.g. tests and the operator CLI path).
	persistResume func(token string)

	// record appends one transcript entry (nil = disabled, e.g. tests and the
	// operator CLI path). Set by RunSession; the daemon is the single writer.
	record func(state.TranscriptEntry)

	// emitTap, when set, receives every event the driver fans out — including on
	// the seed path where gateways is nil. It feeds the daemon's events socket so
	// an external reader (Neublox) sees the live thinking/status/chunk/reply
	// stream. nil = no tap (CLI/tests).
	emitTap func(contracts.Event)

	// gate decides, after each completed turn, whether the session must pause on
	// a budget cap. Defaults to noBudgetGate{} (never trips) so behavior is
	// unchanged until a concrete gate is wired in.
	gate budgetGate
}

func newSessionDriver(name string, gws []contracts.GatewaySet, toBridge chan<- contracts.Event, fromBridge <-chan contracts.Event) *sessionDriver {
	return &sessionDriver{
		name:      name,
		gateways:  gws,
		toBridge:  toBridge,
		from:      fromBridge,
		queue:     make(chan contracts.Event, 64),
		renderers: map[string]*gatewayRenderer{},
		hangup:    make(chan struct{}, 1),
		seen:      map[string]bool{},
		gate:      noBudgetGate{},
	}
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
	if d.record == nil || text == "" {
		return
	}
	d.record(state.TranscriptEntry{
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
	d.queue <- contracts.Event{T: "pick", Value: value}
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
// Persistence of PausedReason is the gate's job (it already wrote it in
// CheckAfterTurn before returning the reason). The backend bridge has no
// consumer for a "paused" frame (checked: nothing reads T=="paused" off
// fromBridge), so this does not also write to d.toBridge.
func (d *sessionDriver) emitPaused(ctx context.Context, reason string) {
	d.fanOut(ctx, contracts.Event{T: "paused", Text: reason})
}

// Seed injects an opening input turn into this session's FIFO. A handoff uses it
// to hand B its task the same way a human message would arrive.
func (d *sessionDriver) Seed(task string) {
	d.queue <- contracts.Event{T: "input", Who: "handoff", Text: task, TurnID: newTurnID()}
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
	d.queue <- contracts.Event{T: "input", Who: "seed", Text: task, TurnID: turnID}
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
func Pick(session, value string) bool {
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
	case d.queue <- contracts.Event{T: "input", Who: in.Author, Text: in.Text, Attachments: atts}:
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
		case ev := <-d.queue:
			// A pick is answered out-of-band by the bridge; only a real input
			// opens a turn (and a progress view) on the bound gateways. Gate
			// only that turn-opening path: an input event that finds the
			// session over budget is refused here, before any turn opens, so
			// a tripped cap actually stops further spend. Picks are not
			// gated — they answer an in-flight or already-rendered turn, not
			// open a new one, so they must not be blocked by a pause that
			// took effect after the pick was queued.
			if ev.T == "input" {
				if reason := d.gate.CheckAfterTurn(d.name); reason != "" {
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
			d.runTurn(ctx, ev)
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
func (d *sessionDriver) runTurn(ctx context.Context, ev contracts.Event) {
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
	// Only a real input turn is budget-watched: a pick answers a turn that is
	// already rendered, and cutting it would leave that turn unanswered.
	if !d.awaitTurn(ctx, ev.T == "input") {
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
// continues. When budgeted, the turn's live token counter is watched against the
// headroom read at turn start and the turn is interrupted the moment it would
// spend past the cap.
func (d *sessionDriver) awaitTurn(ctx context.Context, budgeted bool) bool {
	d.metrics.TurnStarted()
	turnStart := time.Now()
	var headroom uint64
	var capped, cut bool
	if budgeted {
		headroom, capped = d.gate.TokenHeadroom(d.name)
	}
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
				if d.persistResume != nil && e.Resume != "" {
					d.persistResume(e.Resume)
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
				if reason := d.gate.CheckAfterTurn(d.name); reason != "" {
					d.emitPaused(ctx, reason)
					return true // stop this turn; the next dispatch is refused while paused
				}
				return true
			}
			d.fanOut(ctx, e)
			if spent := turnTokens(e); capped && !cut && spent > 0 && spent >= headroom {
				cut = true
				d.Interrupt()
				d.fanOut(ctx, contracts.Event{T: "status", Text: "tour interrompu — plafond de tokens atteint (" +
					strconv.FormatUint(spent, 10) + " tokens sur ce tour, marge " + strconv.FormatUint(headroom, 10) + ")"})
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

func RunSession(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, persistResume func(string), record func(state.TranscriptEntry), gate budgetGate) {
	runSessionIdentified(ctx, name, channel, gws, acc, participants, m, coord, persistResume, record, sessionIdentity{}, gate)
}

func runSessionIdentified(ctx context.Context, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, persistResume func(string), record func(state.TranscriptEntry), identity sessionIdentity, gate budgetGate) {
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
	d.persistResume = persistResume
	d.record = record
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
