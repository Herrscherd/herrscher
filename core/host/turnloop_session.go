package host

import (
	"context"
	"io"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
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
