package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/identity"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/skills"
)

// errPrefix marks a backend failure surfaced to the human as reply text.
const errPrefix = "⚠️ "

// interruptNotice is what a turn the human stopped reads as. Cancelling the
// turn context makes the backend return context.Canceled, which used to be
// rendered with errPrefix — so pressing Esc answered "⚠️ context canceled",
// which reads as the tool breaking rather than as the tool obeying.
const interruptNotice = "⏹ interrupted"

// turnController holds the cancel func of the turn currently running so an
// out-of-band interrupt frame (read on the socket while the turn driver is
// blocked in Respond) can cancel it. A nil *turnController is a no-op, so
// callers that never interrupt (tests) can pass nil.
type turnController struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	// stopped records that this turn's cancellation came from an interrupt and
	// not from the parent context going away (a shutdown, a lost bridge). Only
	// the former is something to tell the human about; the latter is the process
	// ending and has nobody left to read it.
	stopped bool
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
	c.stopped = false
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
		c.stopped = true
		c.cancel()
	}
	c.mu.Unlock()
}

// wasInterrupted reports whether the turn now ending was stopped by the human.
func (c *turnController) wasInterrupted() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopped
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
	scanErr := make(chan error, 1)
	go func() {
		defer close(in)
		scanErr <- conn.Scan(func(e contracts.Event) error {
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

	// The process's working directory is the session's worktree: the directory
	// whose git config decides what a commit from this session signs with, and the
	// one the skill roots hang off. Read once, not per turn.
	cwd, _ := os.Getwd()
	// Before the engine reads its roots, not after: a skill rendered afterwards
	// would be missing from the first turn's menu, which is the turn that matters.
	projectLearnedSkills(ctx, orch, cwd)
	eng := newSkillEngine(backend)
	var pin *scopePin
	if o.Scope != nil && !o.ProjectPinned {
		pin = &scopePin{resolve: o.Scope, current: o.LaunchProject, agent: o.MemoryAgent, orch: orch}
	}
	// The human is resolved here, once, rather than per turn: they do not change
	// mid-session, and a turn should not pay three git calls to be told so.
	runHubTurnsCtl(ctx, in, conn, backend, orch, ctrl, eng,
		affordances{roster: o.Roster, caps: o.Capabilities, user: identity.FromDir(cwd)}, pin)
	if err := ctx.Err(); err != nil {
		return err
	}
	return reportControlScan(<-scanErr)
}

func reportControlScan(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return nil
	}
	logger.Warn("hub control socket read failed", "err", err)
	return fmt.Errorf("hub control socket: %w", err)
}

// affordances are the standing blocks a turn's prompt carries beyond memory and
// skills: who this session may delegate to, what its daemon dispatches, and who
// the human is. They travel as one value because they are the same kind of fact
// — something the session has that the model cannot discover by looking — and
// because a turn loop that takes one more parameter per affordance stops being
// readable.
type affordances struct {
	roster contracts.RosterProvider
	caps   string
	user   identity.Identity
}

// scopePin is a session's one-shot answer to "what is this conversation about".
// It sits beside affordances rather than inside it because affordances are
// prompt blocks, and this is not one: it changes where memory is written.
type scopePin struct {
	resolve ScopeResolver
	current string // the project the session launched with ("" = none)
	agent   string // the private root, preserved across a re-rooting
	orch    contracts.Orchestrator
	settled bool
}

// settle answers the project this session should be recorded under, and returns
// "" once it has already answered — a session is asked once, not once per turn.
// A resolver that names nothing leaves the scope alone but still returns the
// launch candidate, so the daemon pins it and no later turn re-opens the
// question. It is best-effort in the orchestrator's sense: an orchestrator that
// cannot be re-rooted still gets the name into the event, so the row is right on
// the next start.
func (p *scopePin) settle(ctx context.Context, prompt string) string {
	if p == nil || p.settled {
		return ""
	}
	p.settled = true
	chosen := p.resolve.Resolve(ctx, prompt)
	if chosen == "" || chosen == p.current {
		return p.current
	}
	if s, ok := p.orch.(interface{ SetScope(contracts.MemoryScope) }); ok {
		scope := contracts.MemoryScope{Project: contracts.ProjectKey(chosen)}
		if p.agent != "" {
			scope.Agent = contracts.AgentKey(p.agent)
		}
		s.SetScope(scope)
	}
	return chosen
}

// runHubTurns serially drains input frames, running one backend turn per
// input/pick. It is split from runHub so it can be unit-tested over an
// in-memory channel + sink without a real socket. FIFO is inherent: the hub
// sends the next input only after it sees this turn's reply{done}, and this
// loop processes one frame at a time anyway.
func runHubTurns(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator) {
	runHubTurnsCtl(ctx, in, sink, backend, orch, nil, nil, affordances{}, nil)
}

// runHubTurnsCtl is runHubTurns with an explicit turnController so an interrupt
// frame read out-of-band can cancel the in-flight turn.
func runHubTurnsCtl(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator, ctrl *turnController, eng *skills.Engine, aff affordances, pin *scopePin) {
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
				runOneTurn(ctx, sink, backend, orch, ev, ctrl, eng, aff, pin)
			}
		}
	}
}

// runOneTurn runs a single backend turn for an input frame, streaming chunk/
// status events and a terminal reply{done}. An empty output still emits
// reply{done} so the hub's FIFO can advance.
func runOneTurn(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator, ev contracts.Event, ctrl *turnController, eng *skills.Engine, aff affordances, pin *scopePin) {
	turnCtx, endTurn := ctrl.begin(ctx)
	defer endTurn()
	if eng != nil {
		eng.Refresh()
	}
	sink = turnEventSink{EventSink: sink, identity: ev}
	// Settle the memory scope before the context is built, so this very turn is
	// recalled against the project it belongs to rather than the one the
	// directory guessed.
	settledProject := pin.settle(turnCtx, ev.Text)
	var memCtx string
	if orch != nil {
		memCtx = orch.Context(turnCtx)
	}
	prompt := contracts.Prompt{Content: ev.Text, Context: withIdentity(withCapabilities(withDelegation(withSkills(memCtx, eng), aff.roster), aff.caps), aff.user), Author: ev.Who, Attachments: ev.Attachments}
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
		switch {
		case ctrl.wasInterrupted():
			// The human asked for this, so it is not a failure. The partial reply
			// stays and is marked as cut short; with nothing produced yet, the
			// notice is the whole reply.
			if out == "" {
				out = interruptNotice
			} else {
				out = strings.TrimSpace(out) + "\n\n" + interruptNotice
			}
		case out == "":
			out = errPrefix + err.Error()
		default:
			logger.Warn("backend failed after partial output; keeping what it produced", "err", err, "chars", len(out))
		}
	}
	out = strings.TrimSpace(out)
	if eng != nil {
		// The names come from the engine, not from the text, so reporting before
		// React (which rewrites the reply) is about who owns what, not about what
		// survives the rewrite.
		//
		// On the session ctx, not turnCtx, for the same reason Observe below is: an
		// interrupt cancels the turn, and a turn the human cut short is still a turn
		// where a skill was used. Stamping it on turnCtx would make the skill that
		// gets interrupted the skill that ages out.
		reportSkillUse(ctx, orch, eng.Detect(out))
		out = eng.Strip(out)
	}
	if tr, ok := orch.(contracts.TurnReactor); ok {
		// Same: React is where <remember> and <skill> are written down, and losing
		// what the model asked to keep is not part of what an interrupt asked for.
		out = tr.React(ctx, out)
	}
	sink.Emit(contracts.Event{T: "reply", Text: out, Done: true, Cost: cost, Tokens: outTok, TokensIn: inTok, CacheRead: cacheRd, CacheCreate: cacheCr, Resume: resumeToken(backend), Project: settledProject})
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
