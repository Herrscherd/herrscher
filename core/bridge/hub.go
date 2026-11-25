package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/identity"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/skills"
	"github.com/Herrscherd/herrscher/core/timeline"
)

const errPrefix = "⚠️ "

const (
	backendKindText     = "text"
	backendKindThinking = "thinking"
	backendKindTool     = "tool"
	backendKindToolDone = contracts.BackendKindToolDone
	backendKindTodos    = "todos"
	backendKindSubagent = "subagent"
	backendKindReset    = "reset"
	backendKindUsage    = "usage"
	backendKindResult   = "result"
)

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

func runHub(parent context.Context, newBackend BackendFactory, orch contracts.Orchestrator, opts Options) error {
	ctx, endHub := context.WithCancel(parent)
	defer endHub()
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
	approvals := newApprovalChannel(conn.Write, opts.ApprovalWait)
	events := make(chan contracts.Event)
	scanErr := make(chan error, 1)
	go func() {
		defer close(events)
		defer endHub()
		scanErr <- conn.Scan(func(event contracts.Event) error {
			if event.T == eventwire.EventInterrupt {
				ctrl.interrupt()
				return nil
			}
			if approvals.deliver(event) {
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
		affordances{roster: opts.Roster, caps: opts.Capabilities, user: identity.FromDir(ctx, cwd), outbox: outboundSettings{root: opts.OutboxRoot, session: opts.Channel, maxBytes: opts.OutboundMaxBytes}, approvals: approvals}, pin)
	if err := parent.Err(); err != nil {
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
	roster    contracts.RosterProvider
	caps      string
	user      identity.Identity
	outbox    outboundSettings
	approvals *approvalChannel
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
	router := turnRouter{sink: sink, backend: backend, orch: orch, ctrl: ctrl, eng: eng, aff: aff, pin: pin}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			switch event.T {
			case eventwire.EventLanguage:
				lang = languageOf(event.Value)
			case eventwire.EventPick:
				runPick(ctx, sink, backend, event.Value, event.TurnID)
			default:
				if err := router.route(ctx, event, lang); err != nil {
					logTurnFailure(ctx, err)
				}
			}
		}
	}
}

func logTurnFailure(ctx context.Context, err error) {
	if ctx.Err() != nil {
		logger.Info("backend turn ended by hub shutdown", "err", err)
		return
	}
	logger.Warn("backend turn failed", "err", err)
}

type turnUsage struct {
	cost                            float64
	outTok, inTok, cacheRd, cacheCr int
}

func (u *turnUsage) absorb(event contracts.BackendEvent) {
	switch event.Kind {
	case backendKindUsage:
		if timeline.ReportsCount(event.OutTokens) {
			u.outTok += event.OutTokens
		}
		u.setInputSide(event)
	case backendKindResult:
		if timeline.ReportsCount(event.OutTokens) {
			u.outTok = event.OutTokens
		}
		u.setInputSide(event)
		if timeline.ReportsCount(event.Cost) {
			u.cost = event.Cost
		}
	}
}

func (u *turnUsage) setInputSide(event contracts.BackendEvent) {
	if timeline.ReportsCount(event.InTokens) {
		u.inTok = event.InTokens
	}
	if timeline.ReportsCount(event.CacheRead) {
		u.cacheRd = event.CacheRead
	}
	if timeline.ReportsCount(event.CacheCreate) {
		u.cacheCr = event.CacheCreate
	}
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
	if out == "" && !ctrl.wasInterrupted() {
		return errPrefix + err.Error()
	}
	return out
}

func turnOutcome(ctrl *turnController, failure error) contracts.TurnOutcome {
	switch {
	case ctrl.wasInterrupted():
		return contracts.TurnInterrupted
	case failure != nil:
		return contracts.TurnFailed
	}
	return ""
}

func runOneTurn(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, orch contracts.Orchestrator, event contracts.Event, ctrl *turnController, eng *skills.Engine, aff affordances, pin *scopePin, lang i18n.Language) error {
	turnCtx, endTurn := ctrl.begin(ctx)
	defer endTurn()
	turnCtx = aff.approvals.attach(turnCtx, lang)
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
	var calls toolCallGuard
	defer calls.report()
	onEvent := func(event contracts.BackendEvent) {
		event = calls.receive(event)
		relayBackendEvent(sink, &usage, event)
		switch event.Kind {
		case backendKindText:
			streamed = append(streamed, event.Detail)
		case backendKindReset:
			streamed = nil
		}
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
	sink.Emit(contracts.Event{T: eventwire.EventReply, Text: out, Done: true, Outcome: turnOutcome(ctrl, failure), Cost: usage.cost, Tokens: usage.outTok, TokensIn: usage.inTok, CacheRead: usage.cacheRd, CacheCreate: usage.cacheCr, Resume: resumeToken(backend), Project: settledProject})
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

func runPick(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, value, turnID string) {
	inj, ok := choiceInjectorOf(backend)
	if !ok {
		return
	}
	out, err := inj.InjectChoice(ctx, value)
	if err != nil {
		out = errPrefix + err.Error()
	}
	sink.Emit(contracts.Event{T: eventwire.EventReply, Text: strings.TrimSpace(out), Done: true, TurnID: turnID})
}

const maxToolCallNameBytes = 256

var errToolCallNameTooLong = errors.New("bridge: a tool call's id or tool is longer than the limit")

type toolCallGuard struct {
	refused    int
	firstError error
}

func validateToolCall(event contracts.BackendEvent) error {
	if err := event.ValidateToolCall(); err != nil {
		return err
	}
	if event.ToolCall != nil && (len(event.ToolCall.ID) > maxToolCallNameBytes || len(event.ToolCall.Tool) > maxToolCallNameBytes) {
		return errToolCallNameTooLong
	}
	return nil
}

func (g *toolCallGuard) receive(event contracts.BackendEvent) contracts.BackendEvent {
	if err := validateToolCall(event); err != nil {
		if g.refused == 0 {
			g.firstError = err
		}
		g.refused++
		event.ToolCall = nil
		return event
	}
	if event.ToolCall == nil {
		return event
	}
	call := *event.ToolCall
	call.At = time.Now().UTC()
	event.ToolCall = &call
	return event
}

func (g *toolCallGuard) report() {
	if g.refused == 0 {
		return
	}
	logger.Warn("backend tool calls refused in this turn, their events keep their text and tokens", "count", g.refused, "err", g.firstError)
}

func relayBackendEvent(sink contracts.EventSink, usage *turnUsage, event contracts.BackendEvent) {
	usage.absorb(event)
	emitBackendEvent(sink, event, usage.outTok, usage.inTok, usage.cacheRd, usage.cacheCr)
}

func emitBackendEvent(sink contracts.EventSink, event contracts.BackendEvent, tokens, tokensIn, cacheRead, cacheCreate int) {
	switch event.Kind {
	case backendKindThinking:
		sink.Emit(contracts.Event{T: eventwire.EventThinking, Text: event.Detail, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
	case backendKindText:
		sink.Emit(contracts.Event{T: eventwire.EventChunk, Text: event.Detail, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
	case backendKindTool:
		if text := timeline.BoundField(strings.TrimSpace(event.Tool + " " + event.Detail)); text != "" {
			sink.Emit(contracts.Event{T: eventwire.EventStatus, Text: text, ToolCall: event.ToolCall, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
		}
	case backendKindToolDone:
		if event.ToolCall != nil {
			sink.Emit(contracts.Event{T: eventwire.EventToolDone, ToolCall: event.ToolCall, Tokens: tokens, TokensIn: tokensIn, CacheRead: cacheRead, CacheCreate: cacheCreate})
		}
	case backendKindTodos:
		sink.Emit(contracts.Event{T: eventwire.EventTodos, Todos: event.Todos})
	case backendKindSubagent:
		if event.Subagent != nil {
			sink.Emit(contracts.Event{T: eventwire.EventSubagent, Subagent: event.Subagent})
		}
	case backendKindReset:
		sink.Emit(contracts.Event{T: eventwire.EventReset})
	case contracts.BackendKindNotice:
		if event.Notice != nil {
			sink.Emit(contracts.Event{T: eventwire.EventBackendNotice, Value: strconv.FormatUint(uint64(event.Notice.Code), 10)})
		}
	}
}

func languageOf(tag string) i18n.Language {
	lang, err := i18n.ParseLanguage(tag)
	if err != nil {
		return 0
	}
	return lang
}
