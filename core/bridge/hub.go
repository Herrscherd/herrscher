package bridge

import (
	"context"
	"fmt"
	"strings"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/skills"
)

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

// runHub is the pure-runner loop: it dials the hub control socket, reads input
// frames, and drives the backend one turn at a time, emitting events back over
// the same connection. ctx cancellation returns its error.
func runHub(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, o Options) error {
	resp, err := newBackend(o.Channel)
	if err != nil {
		return fmt.Errorf("backend: %w", err)
	}
	defer resp.Close()

	conn, err := control.Dial(o.HubSocket)
	if err != nil {
		return fmt.Errorf("dial hub socket %s: %w", o.HubSocket, err)
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

	eng := newSkillEngine(resp)
	runHubTurnsCtl(ctx, in, conn, resp, orch, ctrl, eng)
	return ctx.Err()
}

// runHubTurns serially drains input frames, running one backend turn per
// input/pick. It is split from runHub so it can be unit-tested over an
// in-memory channel + sink without a real socket. FIFO is inherent: the hub
// sends the next input only after it sees this turn's reply{done}, and this
// loop processes one frame at a time anyway.
func runHubTurns(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, resp contracts.Backend, orch contracts.Orchestrator) {
	runHubTurnsCtl(ctx, in, sink, resp, orch, nil, nil)
}

// runHubTurnsCtl is runHubTurns with an explicit turnController so an interrupt
// frame read out-of-band can cancel the in-flight turn.
func runHubTurnsCtl(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, resp contracts.Backend, orch contracts.Orchestrator, ctrl *turnController, eng *skills.Engine) {
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
				runPick(ctx, sink, resp, ev.Value)
			default: // "input" (and any human-origin frame)
				runOneTurn(ctx, sink, resp, orch, ev, ctrl, eng)
			}
		}
	}
}

// runOneTurn runs a single backend turn for an input frame, streaming chunk/
// status events and a terminal reply{done}. An empty output still emits
// reply{done} so the hub's FIFO can advance.
func runOneTurn(ctx context.Context, sink contracts.EventSink, resp contracts.Backend, orch contracts.Orchestrator, ev contracts.Event, ctrl *turnController, eng *skills.Engine) {
	turnCtx, endTurn := ctrl.begin(ctx)
	defer endTurn()
	if eng != nil {
		eng.Refresh()
	}
	var memCtx string
	if orch != nil {
		memCtx = orch.Context(turnCtx)
	}
	prompt := contracts.Prompt{Content: ev.Text, Context: withSkills(memCtx, eng), Author: ev.Who, Attachments: ev.Attachments}
	var cost float64
	var outTok int
	onEvent := func(be contracts.BackendEvent) {
		switch be.Kind {
		case "usage":
			outTok = be.OutTokens
		case "result":
			cost = be.Cost
			outTok = be.OutTokens
		}
		emitBackendEvent(sink, be, outTok)
	}
	out, err := resp.Respond(turnCtx, prompt, onEvent)
	if err != nil && out == "" {
		out = "⚠️ " + err.Error()
	}
	out = strings.TrimSpace(out)
	if eng != nil {
		eng.Detect(out)
		out = eng.Strip(out)
	}
	if tr, ok := orch.(contracts.TurnReactor); ok {
		out = tr.React(turnCtx, out)
	}
	sink.Emit(contracts.Event{T: "reply", Text: out, Done: true, Cost: cost, Tokens: outTok, Resume: resumeToken(resp)})
	if orch != nil {
		_ = orch.Observe(ctx, prompt, out)
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

// resumeToken reads a backend's opaque resume token when it is ResumeAware, so
// the daemon can persist it for cross-restart --resume. "" when unsupported.
func resumeToken(resp contracts.Backend) string {
	if ra, ok := resp.(contracts.ResumeAware); ok {
		return ra.ResumeToken()
	}
	return ""
}

// runPick answers a routed select-menu pick out-of-band (serialized with turns
// by runHubTurns), emitting whatever the backend produces as a reply{done}.
func runPick(ctx context.Context, sink contracts.EventSink, resp contracts.Backend, value string) {
	inj, ok := resp.(contracts.ChoiceInjector)
	if !ok {
		return
	}
	out, err := inj.InjectChoice(ctx, value)
	if err != nil {
		out = "⚠️ " + err.Error()
	}
	sink.Emit(contracts.Event{T: "reply", Text: strings.TrimSpace(out), Done: true})
}

// emitBackendEvent maps a backend progress event onto the bus vocabulary:
// thinking → thinking, text → chunk, tool → status (dropped when empty), reset
// → reset; usage and result carry no transcript and are dropped. The live
// cumulative output-token count (tokens) rides on every rendered event so a
// gateway can show a growing counter mid-turn. Mirrors the relocated
// runner.emitBackend.
func emitBackendEvent(sink contracts.EventSink, be contracts.BackendEvent, tokens int) {
	switch be.Kind {
	case "thinking":
		sink.Emit(contracts.Event{T: "thinking", Text: be.Detail, Tokens: tokens})
	case "text":
		sink.Emit(contracts.Event{T: "chunk", Text: be.Detail, Tokens: tokens})
	case "tool":
		if text := strings.TrimSpace(be.Tool + " " + be.Detail); text != "" {
			sink.Emit(contracts.Event{T: "status", Text: text, Tokens: tokens})
		}
	case "reset":
		sink.Emit(contracts.Event{T: "reset"})
	}
}
