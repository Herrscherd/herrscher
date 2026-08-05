package bridge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/skills"
)

// errPrefix marks a backend failure surfaced to the human as reply text.
const errPrefix = "⚠️ "

// turnController holds the cancel func of the turn currently running so an
// out-of-band interrupt frame (read on the socket while the turn driver is
// blocked in Respond) can cancel it. A nil *turnController is a no-op, so
// callers that never interrupt (tests) can pass nil.
type turnController struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

// begin derives a cancellable turn context and records its cancel; the returned
// end func clears it (call on turn completion). On a nil controller it is a
// pass-through.
func (c *turnController) begin(parent context.Context) (context.Context, func()) {
	if c == nil {
		return parent, func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	return ctx, func() {
		c.mu.Lock()
		if c.cancel != nil {
			c.cancel()
			c.cancel = nil
		}
		c.mu.Unlock()
	}
}

// interrupt cancels the active turn, if any.
func (c *turnController) interrupt() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
}

// hubDialWindow is how long a bridge waits for its control socket to appear
// before giving up. The daemon starts the supervised bridge and opens the socket
// from two different places — a session create or resume starts the child, and
// the reconcile that follows opens the socket — so a fresh bridge routinely
// reaches its socket first. Without a wait it dies on the spot and comes back
// only through the restart backoff, which is a crash in the log and a session
// that answers nothing for the first second of its life.
const hubDialWindow = 10 * time.Second

// hubDialInterval paces the retries inside that window.
const hubDialInterval = 100 * time.Millisecond

// dialHub connects to the hub control socket, waiting for it to appear. It
// returns as soon as the socket answers, and gives up at hubDialWindow so a
// bridge whose daemon is truly gone still exits and lets the supervisor decide.
func dialHub(ctx context.Context, path string) (*control.Conn, error) {
	deadline := time.Now().Add(hubDialWindow)
	for {
		conn, err := control.Dial(path)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("dial hub socket %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(hubDialInterval):
		}
	}
}

// runHub is the pure-runner loop: it dials the hub control socket, reads input
// frames, and drives the backend one turn at a time, emitting events back over
// the same connection. ctx cancellation returns its error.
func runHub(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, o Options) error {
	backend, err := newBackend(o.Channel)
	if err != nil {
		return fmt.Errorf("backend: %w", err)
	}
	defer backend.Close()

	conn, err := dialHub(ctx, o.HubSocket)
	if err != nil {
		return err
	}
	defer conn.Close()

	// The hub frames inputs as JSON-line Events; surface them on a channel the
	// turn driver consumes. Scan returns when the hub closes the conn (daemon
	// gone or session closed) → the bridge exits and the supervisor decides.
	ctrl := &turnController{}
	in := make(chan contracts.Event)
	go func() {
		defer close(in)
		_ = conn.Scan(func(e contracts.Event) error {
			// An interrupt is handled out-of-band: the turn driver is blocked in
			// Respond and cannot dequeue it, so cancel the active turn directly
			// instead of forwarding it onto the FIFO input channel.
			if e.T == "interrupt" {
				ctrl.interrupt()
				return nil
			}
			select {
			case in <- e:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	eng := newSkillEngine(backend)
	runHubTurnsCtl(ctx, in, conn, backend, orch, ctrl, eng, o.Roster)
	return ctx.Err()
}

// runHubTurns serially drains input frames, running one backend turn per
// input/pick. It is split from runHub so it can be unit-tested over an
// in-memory channel + sink without a real socket. FIFO is inherent: the hub
// sends the next input only after it sees this turn's reply{done}, and this
// loop processes one frame at a time anyway.
func runHubTurns(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator) {
	runHubTurnsCtl(ctx, in, sink, backend, orch, nil, nil, nil)
}

// runHubTurnsCtl is runHubTurns with an explicit turnController so an interrupt
// frame read out-of-band can cancel the in-flight turn.
func runHubTurnsCtl(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator, ctrl *turnController, eng *skills.Engine, roster contracts.RosterProvider) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-in:
			if !ok {
				return
			}
			switch ev.T {
			case "pick":
				runPick(ctx, sink, backend, ev.Value)
			default: // "input" (and any human-origin frame)
				runOneTurn(ctx, sink, backend, orch, ev, ctrl, eng, roster)
			}
		}
	}
}

// runOneTurn runs a single backend turn for an input frame, streaming chunk/
// status events and a terminal reply{done}. An empty output still emits
// reply{done} so the hub's FIFO can advance.
func runOneTurn(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator, ev contracts.Event, ctrl *turnController, eng *skills.Engine, roster contracts.RosterProvider) {
	turnCtx, endTurn := ctrl.begin(ctx)
	defer endTurn()
	if eng != nil {
		eng.Refresh()
	}
	sink = turnEventSink{EventSink: sink, identity: ev}
	var memCtx string
	if orch != nil {
		memCtx = orch.Context(turnCtx)
	}
	prompt := contracts.Prompt{Content: ev.Text, Context: withDelegation(withSkills(memCtx, eng), roster), Author: ev.Who, Attachments: ev.Attachments}
	var cost float64
	var outTok, inTok, cacheRd, cacheCr int
	onEvent := func(be contracts.BackendEvent) {
		switch be.Kind {
		case "usage":
			// A usage event reports ONE assistant message, not the turn so far: an
			// agentic turn emits one per message. Output tokens therefore add up
			// across the turn — without the sum, the live counter would bounce
			// around the last message's size instead of growing, and the host's
			// mid-turn budget guard would never see a runaway turn.
			//
			// Input and cache counts stay last-wins: each message reports its whole
			// prompt, so summing them would count the same context once per message.
			// The latest is the live context size, which is what a counter wants.
			outTok += be.OutTokens
			inTok = be.InTokens
			cacheRd = be.CacheRead
			cacheCr = be.CacheCreate
		case "result":
			// The terminal result carries the turn's authoritative totals, so it
			// replaces the running sum rather than adding to it. This is what lands
			// on the transcript entry the budget gate later folds.
			outTok = be.OutTokens
			inTok = be.InTokens
			cacheRd = be.CacheRead
			cacheCr = be.CacheCreate
			cost = be.Cost
		}
		emitBackendEvent(sink, be, outTok, inTok, cacheRd, cacheCr)
	}
	out, err := backend.Respond(turnCtx, prompt, onEvent)
	if err != nil {
		// Partial output is kept as the reply — work the backend did produce is
		// worth more to the human than an error banner — but the failure itself
		// must not vanish: a truncated reply is indistinguishable from a complete
		// one, so the reason is at least in the operator log.
		if out == "" {
			out = errPrefix + err.Error()
		} else {
			logger.Warn("backend failed after partial output; keeping what it produced", "err", err, "chars", len(out))
		}
	}
	out = strings.TrimSpace(out)
	if eng != nil {
		eng.Detect(out)
		out = eng.Strip(out)
	}
	if tr, ok := orch.(contracts.TurnReactor); ok {
		out = tr.React(turnCtx, out)
	}
	sink.Emit(contracts.Event{T: "reply", Text: out, Done: true, Cost: cost, Tokens: outTok, TokensIn: inTok, CacheRead: cacheRd, CacheCreate: cacheCr, Resume: resumeToken(backend)})
	if orch != nil {
		// Not fatal to the turn (the human already has the reply), but a failed
		// observe means this turn never reached memory — silently forgetting is
		// exactly the kind of thing an operator needs told.
		if err := orch.Observe(ctx, prompt, out); err != nil {
			logger.Warn("memory observe failed; this turn was not recorded to memory", "err", err)
		}
	}
}

// withSkills appends the skill menu and any active-skill expansions to the
// memory context. A nil engine (skills disabled / native backend) returns memCtx
// unchanged.
func withSkills(memCtx string, eng *skills.Engine) string {
	if eng == nil {
		return memCtx
	}
	parts := make([]string, 0, 3)
	if memCtx != "" {
		parts = append(parts, memCtx)
	}
	if menu := eng.Menu(); menu != "" {
		parts = append(parts, menu)
	}
	if exp := eng.Expansions(); exp != "" {
		parts = append(parts, exp)
	}
	return strings.Join(parts, "\n\n")
}

// turnEventSink stamps every event a turn emits with the turn's authoritative
// identity (session incarnation, turn id, agent) taken from the input frame, so
// the hub fan-out and downstream consumers can attribute events unambiguously.
type turnEventSink struct {
	contracts.EventSink
	identity contracts.Event
}

func (s turnEventSink) Emit(e contracts.Event) {
	e.SessionIncarnation = s.identity.SessionIncarnation
	e.TurnID = s.identity.TurnID
	e.Agent = s.identity.Agent
	s.EventSink.Emit(e)
}

// resumeToken reads a backend's opaque resume token when it is ResumeAware, so
// the daemon can persist it for cross-restart --resume. "" when unsupported.
func resumeToken(backend contracts.Backend) string {
	if ra, ok := backend.(contracts.ResumeAware); ok {
		return ra.ResumeToken()
	}
	return ""
}

// runPick answers a routed select-menu pick out-of-band (serialized with turns
// by runHubTurns), emitting whatever the backend produces as a reply{done}.
func runPick(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, value string) {
	inj, ok := backend.(contracts.ChoiceInjector)
	if !ok {
		return
	}
	out, err := inj.InjectChoice(ctx, value)
	if err != nil {
		out = errPrefix + err.Error()
	}
	sink.Emit(contracts.Event{T: "reply", Text: strings.TrimSpace(out), Done: true})
}

// emitBackendEvent maps a backend progress event onto the bus vocabulary:
// thinking → thinking, text → chunk, tool → status (dropped when empty), reset
// → reset; usage and result carry no transcript and are dropped. The live
// cumulative output-token count (tokens) rides on every rendered event so a
// gateway can show a growing counter mid-turn. Mirrors the relocated
// runner.emitBackend.
func emitBackendEvent(sink contracts.EventSink, be contracts.BackendEvent, tokens, tokensIn, cacheRead, cacheCreate int) {
	switch be.Kind {
	case "thinking":
		sink.Emit(contracts.Event{T: "thinking", Text: be.Detail, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
	case "text":
		sink.Emit(contracts.Event{T: "chunk", Text: be.Detail, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
	case "tool":
		if text := strings.TrimSpace(be.Tool + " " + be.Detail); text != "" {
			sink.Emit(contracts.Event{T: "status", Text: text, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
		}
	case "reset":
		sink.Emit(contracts.Event{T: "reset"})
	}
}
