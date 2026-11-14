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
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/identity"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/skills"
)

const errPrefix = "⚠️ "

const interruptNotice = "⏹ interrupted"

const resetKind = "reset"

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
		conn, err := control.DialContext(ctx, path)
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

func runHub(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, opts Options) error {
	backend, err := newBackend(opts.Channel)
	if err != nil {
		return fmt.Errorf("backend: %w", err)
	}
	defer closeAndWarn(backend)

	conn, err := dialHub(ctx, opts.HubSocket)
	if err != nil {
		return err
	}
	defer closeAndWarn(conn)

	ctrl := &turnController{}
	events := make(chan contracts.Event)
	scanErr := make(chan error, 1)
	go func() {
		defer close(events)
		scanErr <- conn.Scan(func(event contracts.Event) error {
			if event.T == "interrupt" {
				ctrl.interrupt()
				return nil
			}
			select {
			case events <- event:
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
	if opts.Scope != nil && !opts.ProjectPinned {
		pin = &scopePin{resolve: opts.Scope, current: opts.LaunchProject, agent: opts.MemoryAgent, orch: orch}
	}
	runHubTurnsCtl(ctx, events, conn, backend, orch, ctrl, eng,
		affordances{roster: opts.Roster, caps: opts.Capabilities, user: identity.FromDir(ctx, cwd), outbox: outboundSettings{root: opts.OutboxRoot, session: opts.Channel, maxBytes: opts.OutboundMaxBytes}}, pin)
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
	outbox outboundSettings
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
	if scoper, ok := p.orch.(interface{ SetScope(contracts.MemoryScope) }); ok {
		scope := contracts.MemoryScope{Project: contracts.ProjectKey(chosen)}
		if p.agent != "" {
			scope.Agent = contracts.AgentKey(p.agent)
		}
		scoper.SetScope(scope)
	}
	return chosen
}

func runHubTurns(ctx context.Context, in <-chan contracts.Event, sink contracts.EventSink, backend contracts.Backend) {
	runHubTurnsCtl(ctx, in, sink, backend, nil, nil, nil, affordances{}, nil)
}

func runHubTurnsCtl(ctx context.Context, events <-chan contracts.Event, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator, ctrl *turnController, eng *skills.Engine, aff affordances, pin *scopePin) {
	var lang i18n.Language
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			switch event.T {
			case "language":
				lang = languageOf(event.Value)
			case "pick":
				runPick(ctx, sink, backend, event.Value)
			case compactwire.EventRequest:
				runCompact(ctx, sink, backend, ctrl, event)
			case historywire.EventRead:
				runHistoryRead(ctx, sink, backend, ctrl, event)
			case historywire.EventPrune:
				runHistoryPrune(ctx, sink, backend, ctrl, event)
			case "input":
				if err := runOneTurn(ctx, sink, backend, orch, event, ctrl, eng, aff, pin, lang); err != nil {
					logger.Warn("backend turn failed; its reply already went out", "err", err)
				}
			}
		}
	}
}

type turnUsage struct {
	cost                            float64
	outTok, inTok, cacheRd, cacheCr int
}

func (u *turnUsage) absorb(event contracts.BackendEvent) {
	switch event.Kind {
	case "usage":
		u.outTok += event.OutTokens
		u.setInputSide(event)
	case "result":
		u.outTok = event.OutTokens
		u.setInputSide(event)
		u.cost = event.Cost
	}
}

func (u *turnUsage) setInputSide(event contracts.BackendEvent) {
	u.inTok = event.InTokens
	u.cacheRd = event.CacheRead
	u.cacheCr = event.CacheCreate
}

// TurnFailedError reports that the backend ended a turn in error. The gateways
// still receive the reply, which carries the failure; this error is what a caller
// with no gateway, such as a one-shot seed, reports to its operator.
type TurnFailedError struct {
	Err     error
	partial string
}

// Error returns the message of the backend failure that ended the turn.
func (e *TurnFailedError) Error() string { return e.Err.Error() }

// Unwrap returns the backend failure, so errors.Is and errors.AsType reach its cause.
func (e *TurnFailedError) Unwrap() error { return e.Err }

// Notice is the notice the operator reads for the failed turn.
func (e *TurnFailedError) Notice() contracts.Notice { return contracts.NoticeOf(e.Err) }

// PartialOutput is the text the backend streamed in the attempt that failed, since its last reset, empty when it streamed none.
func (e *TurnFailedError) PartialOutput() string { return e.partial }

func settleFailedReply(ctrl *turnController, out string, err error) string {
	switch {
	case ctrl.wasInterrupted():
		if out == "" {
			return interruptNotice
		}
		return strings.TrimSpace(out) + "\n\n" + interruptNotice
	case out == "":
		return errPrefix + err.Error()
	default:
		return out
	}
}

func runOneTurn(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator, event contracts.Event, ctrl *turnController, eng *skills.Engine, aff affordances, pin *scopePin, lang i18n.Language) error {
	turnCtx, endTurn := ctrl.begin(ctx)
	defer endTurn()
	if eng != nil {
		eng.Refresh()
	}
	sink = turnEventSink{EventSink: sink, identity: event}
	settledProject := pin.settle(turnCtx, event.Text)
	var memCtx string
	if orch != nil {
		memCtx = orch.Context(turnCtx)
	}
	turn := beginTurnOutbox(backend, aff.outbox)
	defer turn.discard()
	prompt := contracts.Prompt{Content: event.Text, Context: withIdentity(withCapabilities(withDelegation(withSkills(memCtx, eng), aff.roster), aff.caps), aff.user), Author: event.Who, Attachments: event.Attachments, Language: lang, Outbox: turn.dir}
	var usage turnUsage
	var streamed []string
	onEvent := func(event contracts.BackendEvent) {
		usage.absorb(event)
		switch event.Kind {
		case "text":
			streamed = append(streamed, event.Detail)
		case resetKind:
			streamed = nil
		}
		emitBackendEvent(sink, event, usage.outTok, usage.inTok, usage.cacheRd, usage.cacheCr)
	}
	out, err := backend.Respond(turnCtx, prompt, onEvent)
	var failure error
	if err != nil {
		out = settleFailedReply(ctrl, out, err)
		if !ctrl.wasInterrupted() {
			failure = &TurnFailedError{Err: err, partial: strings.TrimSpace(strings.Join(streamed, ""))}
		}
	}
	turn.emitFiles(ctx, sink)
	out = strings.TrimSpace(out)
	if eng != nil {
		reportSkillUse(ctx, orch, eng.Detect(out))
		out = eng.Strip(out)
	}
	if tr, ok := orch.(contracts.TurnReactor); ok {
		out = tr.React(ctx, out)
	}
	sink.Emit(contracts.Event{T: "reply", Text: out, Done: true, Cost: usage.cost, Tokens: usage.outTok, TokensIn: usage.inTok, CacheRead: usage.cacheRd, CacheCreate: usage.cacheCr, Resume: resumeToken(backend), Project: settledProject})
	if orch != nil {
		if err := orch.Observe(ctx, prompt, out); err != nil {
			logger.Warn("memory observe failed; this turn was not recorded to memory", "err", err)
		}
	}
	return failure
}

func withSkills(memCtx string, eng *skills.Engine) string {
	if eng == nil {
		return memCtx
	}
	parts := make([]string, 0, partsCap)
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

func (s turnEventSink) Emit(event contracts.Event) {
	event.SessionIncarnation = s.identity.SessionIncarnation
	event.TurnID = s.identity.TurnID
	event.Agent = s.identity.Agent
	s.EventSink.Emit(event)
}

func resumeToken(backend contracts.Backend) string {
	if ra, ok := resumeAwareOf(backend); ok {
		return ra.ResumeToken()
	}
	return ""
}

func runPick(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, value string) {
	inj, ok := choiceInjectorOf(backend)
	if !ok {
		return
	}
	out, err := inj.InjectChoice(ctx, value)
	if err != nil {
		out = errPrefix + err.Error()
	}
	sink.Emit(contracts.Event{T: "reply", Text: strings.TrimSpace(out), Done: true})
}

func emitBackendEvent(sink contracts.EventSink, event contracts.BackendEvent, tokens, tokensIn, cacheRead, cacheCreate int) {
	switch event.Kind {
	case "thinking":
		sink.Emit(contracts.Event{T: "thinking", Text: event.Detail, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
	case "text":
		sink.Emit(contracts.Event{T: "chunk", Text: event.Detail, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
	case "tool":
		if text := strings.TrimSpace(event.Tool + " " + event.Detail); text != "" {
			sink.Emit(contracts.Event{T: "status", Text: text, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
		}
	case "todos":
		sink.Emit(contracts.Event{T: "todos", Todos: event.Todos})
	case "subagent":
		if event.Subagent != nil {
			sink.Emit(contracts.Event{T: "subagent", Subagent: event.Subagent})
		}
	case resetKind:
		sink.Emit(contracts.Event{T: resetKind})
	}
}

func languageOf(tag string) i18n.Language {
	lang, err := i18n.ParseLanguage(tag)
	if err != nil {
		return 0
	}
	return lang
}
