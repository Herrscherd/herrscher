package host

import (
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/otelexport"
	"github.com/Herrscherd/herrscher/core/timeline"
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
	Transcript  func(state.TranscriptEntry) error
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
	compacting    atomic.Bool
	interrupts    atomic.Uint64
	historyCalls  atomic.Uint64
	channel       string
	outboxRoot    string
	saidElsewhere bool
	notices       chan noticeDelivery
	relays        chan relayedEvent
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
	calls             timeline.Recorder
	telemetry         otelexport.Exporter
	turnSpan          turnSpan
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
	ev         contracts.Event
	origin     contracts.Conversation
	interrupts uint64
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
		relays:      make(chan relayedEvent, noticeQueueCapacity),
		noticeLog:   obs.Stderr(false),
		gate:        noBudgetGate{},
		persists:    newPersistWrites(),

		enqueueTimeout: defaultEnqueueTimeout,

		attachHosts: attachmentHosts(gws),
	}
}
