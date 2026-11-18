package host

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
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
