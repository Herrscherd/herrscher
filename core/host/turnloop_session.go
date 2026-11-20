package host

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
)

type sessionIdentity struct {
	incarnation string
	agent       string
	terms       func() modelTerms
	language    func() i18n.Language
	operator    languageSource
}

// RunSession drives a session without a declared identity until ctx is done.
func RunSession(ctx context.Context, sessions *sessionTable, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, m *metrics.Registry, coord contracts.Coordinator, sink sessionSink, gate budgetGate, warnings io.Writer) {
	runSessionIdentified(ctx, sessions, name, channel, gws, acc, participants, m, coord, sink, nil, sessionIdentity{}, nil, gate, warnings)
}

func runSessionIdentified(ctx context.Context, sessions *sessionTable, name, channel string, gws []contracts.GatewaySet, acc *control.Acceptor, participants string, registry *metrics.Registry, coord contracts.Coordinator, sink sessionSink, emit func(contracts.Event), identity sessionIdentity, desk *approvalDesk, gate budgetGate, warnings io.Writer) {
	defer closeRead(acc)
	approvals := bridgeApprovals{messages: sessions.messages, lang: identity.operator}
	if desk != nil {
		approvals.asker = desk.asker(name, BridgeApprovalWait())
	}
	if approvals.lang == nil {
		approvals.lang = fixedLanguageSource(i18n.English)
	}
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
			serveConn(ctx, conn, toBridge, fromBridge, driver.hangup, approvals)
		}
	}
}

const (
	hostApprovalRequestUnreadable = "approval.request_unreadable"
	hostApprovalNoDesk            = "approval.no_desk"
)

func serveConn(ctx context.Context, conn *control.Conn, toBridge <-chan contracts.Event, fromBridge chan<- contracts.Event, hangup chan<- struct{}, approvals bridgeApprovals) {
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
			return relayFromBridge(cctx, conn, approvals, fromBridge, event)
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

type bridgeApprovals struct {
	asker    contracts.ApprovalAsker
	messages *hostMessages
	lang     languageSource
}

func (a bridgeApprovals) refusal(requestID, key string, params map[string]any) contracts.ApprovalAnswer {
	return contracts.ApprovalAnswer{ID: requestID, Decision: contracts.DecisionDeny, Reason: a.messages.text(a.lang(), key, params)}
}

func relayFromBridge(ctx context.Context, conn *control.Conn, approvals bridgeApprovals, fromBridge chan<- contracts.Event, event contracts.Event) error {
	if event.T == eventwire.EventApprovalAsk {
		go answerBridgeApproval(ctx, conn, approvals, event.Value)
		return nil
	}
	select {
	case fromBridge <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func answerBridgeApproval(ctx context.Context, conn *control.Conn, approvals bridgeApprovals, value string) {
	var req contracts.ApprovalRequest
	if err := json.Unmarshal([]byte(value), &req); err != nil {
		slog.Warn("approval request from the bridge is unreadable", "err", err)
		if req.ID != "" {
			writeBridgeAnswer(conn, approvals.refusal(req.ID, hostApprovalRequestUnreadable, map[string]any{detailParam: err.Error()}))
		}
		return
	}
	if approvals.asker == nil {
		writeBridgeAnswer(conn, approvals.refusal(req.ID, hostApprovalNoDesk, nil))
		return
	}
	writeBridgeAnswer(conn, approvals.asker(ctx, req))
}

func writeBridgeAnswer(conn *control.Conn, answer contracts.ApprovalAnswer) {
	payload, err := json.Marshal(answer)
	if err != nil {
		slog.Warn("approval answer cannot be encoded", "err", err)
		return
	}
	if err := conn.Write(contracts.Event{T: eventwire.EventApprovalAnswer, Value: string(payload)}); err != nil {
		slog.Debug("approval answer not delivered", "err", err)
	}
}
