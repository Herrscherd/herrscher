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

const errPrefix = "⚠️ "

const interruptNotice = "⏹ interrupted"

type turnController struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	stopped bool
}

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

func (c *turnController) wasInterrupted() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopped
}

const hubDialWindow = 10 * time.Second

const hubDialInterval = 100 * time.Millisecond

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

func runHub(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, o Options) error {
	backend, err := newBackend(o.Channel)
	if err != nil {
		return fmt.Errorf("backend: %w", err)
	}
	defer closeQuietly(backend)

	conn, err := dialHub(ctx, o.HubSocket)
	if err != nil {
		return err
	}
	defer closeQuietly(conn)

	ctrl := &turnController{}
	in := make(chan contracts.Event)
	scanErr := make(chan error, 1)
	go func() {
		defer close(in)
		scanErr <- conn.Scan(func(e contracts.Event) error {
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

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	projectLearnedSkills(ctx, orch, cwd)
	eng := newSkillEngine(backend)
	var pin *scopePin
	if o.Scope != nil && !o.ProjectPinned {
		pin = &scopePin{resolve: o.Scope, current: o.LaunchProject, agent: o.MemoryAgent, orch: orch}
	}
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

type affordances struct {
	roster contracts.RosterProvider
	caps   string
	user   identity.Identity
}

type scopePin struct {
	resolve ScopeResolver
	current string
	agent   string
	orch    contracts.Orchestrator
	settled bool
}

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

func runHubTurns(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator) {
	runHubTurnsCtl(ctx, in, sink, backend, orch, nil, nil, affordances{}, nil)
}

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
			default:
				runOneTurn(ctx, sink, backend, orch, ev, ctrl, eng, aff, pin)
			}
		}
	}
}

func runOneTurn(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator, ev contracts.Event, ctrl *turnController, eng *skills.Engine, aff affordances, pin *scopePin) {
	turnCtx, endTurn := ctrl.begin(ctx)
	defer endTurn()
	if eng != nil {
		eng.Refresh()
	}
	sink = turnEventSink{EventSink: sink, identity: ev}
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
			outTok += be.OutTokens
			inTok = be.InTokens
			cacheRd = be.CacheRead
			cacheCr = be.CacheCreate
		case "result":
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
		switch {
		case ctrl.wasInterrupted():
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
		reportSkillUse(ctx, orch, eng.Detect(out))
		out = eng.Strip(out)
	}
	if tr, ok := orch.(contracts.TurnReactor); ok {
		out = tr.React(ctx, out)
	}
	sink.Emit(contracts.Event{T: "reply", Text: out, Done: true, Cost: cost, Tokens: outTok, TokensIn: inTok, CacheRead: cacheRd, CacheCreate: cacheCr, Resume: resumeToken(backend), Project: settledProject})
	if orch != nil {
		if err := orch.Observe(ctx, prompt, out); err != nil {
			logger.Warn("memory observe failed; this turn was not recorded to memory", "err", err)
		}
	}
}

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

func resumeToken(backend contracts.Backend) string {
	if ra, ok := backend.(contracts.ResumeAware); ok {
		return ra.ResumeToken()
	}
	return ""
}

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
	case "todos":
		sink.Emit(contracts.Event{T: "todos", Todos: be.Todos})
	case "subagent":
		if be.Subagent != nil {
			sink.Emit(contracts.Event{T: "subagent", Subagent: be.Subagent})
		}
	case "reset":
		sink.Emit(contracts.Event{T: "reset"})
	}
}
