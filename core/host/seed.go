package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

const seedConsolidateTimeout = 60 * time.Second

const seedTurnTimeout = 120 * time.Second

const seedJoinGrace = 2 * time.Second

var errSeedWithoutApprovalDesk = errors.New("the seed runtime has no approval desk")

type oneShotSeedRuntime struct {
	messages        *hostMessages
	cliMessages     *clitext.Messages
	warnings        io.Writer
	sessionLanguage func(name string) languageSource
	coordinator     contracts.Coordinator
	publish         func(session string, event contracts.Event)
	record          func(session string, entry state.TranscriptEntry)
	recordUsage     func(sess state.Session, records []usage.Record)
	resume          func(session, token string)
	timeout         time.Duration
	backend         func(context.Context, state.Session) (contracts.Backend, error)
	models          *ModelDiscovery
	sessions        *sessionTable
	desk            *approvalDesk
	localRun        bool
	persists        *persistWrites
}

func (r oneShotSeedRuntime) backendFactory() func(context.Context, state.Session) (contracts.Backend, error) {
	if r.backend != nil {
		return r.backend
	}
	return func(ctx context.Context, sess state.Session) (contracts.Backend, error) {
		return newSeedBackend(ctx, r.messages, sess, nil, r.models)
	}
}

type oneShotSeedRuntimeKey struct{}

func withOneShotSeedRuntime(ctx context.Context, runtime oneShotSeedRuntime) context.Context {
	return context.WithValue(ctx, oneShotSeedRuntimeKey{}, runtime)
}

func oneShotSeedRuntimeFrom(ctx context.Context, messages *hostMessages, cliMessages *clitext.Messages, newOneShot func() *persistWrites) oneShotSeedRuntime {
	runtime, ok := ctx.Value(oneShotSeedRuntimeKey{}).(oneShotSeedRuntime)
	if !ok {
		return oneShotSeedRuntime{messages: messages, cliMessages: cliMessages, persists: newOneShot(), localRun: true}
	}
	return runtime
}

type seedCommandForwarder func(context.Context, string, []string) (string, bool, error)

func runOneShotSeedCommand(ctx context.Context, hostState *state.State, name, task, turnID string, runtime oneShotSeedRuntime, coord contracts.Coordinator, layout RuntimeLayout, instID string, forward seedCommandForwarder) (string, error) {
	if runtime.coordinator == nil {
		runtime.coordinator = coord
	}
	if runtime.coordinator == nil && forward != nil {
		argv := append([]string{nsSession, verbSeed, "--name", name}, cli.FlagArg("task", task)...)
		argv = append(argv, "--turn_id", turnID)
		if runtime.timeout > 0 {
			argv = append(argv, "--timeout", runtime.timeout.String())
		}
		if reply, handled, err := forward(ctx, commandSocketTarget(layout, instID), argv); handled {
			return reply, err
		}
	}
	return runOneShotSeedIDWithRuntime(ctx, hostState, name, task, turnID, runtime)
}

func runOneShotSeedIDWithRuntime(ctx context.Context, st *state.State, name, task, turnID string, runtime oneShotSeedRuntime) (string, error) {
	sess, ok := st.FindSession(name)
	if !ok {
		return "", &NotFoundError{Kind: kindSession, Name: name}
	}
	if sess.Archived {
		return "", errSeedArchivedSession
	}
	orch, mem, err := seedOrchestrator(ctx, runtime.messages, sess)
	if err != nil {
		return "", err
	}
	if mem != nil {
		defer closeRead(mem)
	}
	return runOneShotSeedWithIDRuntime(ctx, sess, task, turnID, orch, runtime)
}

func runOneShotSeedWithIDRuntime(ctx context.Context, sess state.Session, task, turnID string, orch contracts.Orchestrator, runtime oneShotSeedRuntime) (string, error) {
	if runtime.desk == nil && !runtime.localRun {
		return "", errSeedWithoutApprovalDesk
	}
	if orch != nil {
		defer closeRead(orch)
	}
	seedCtx, cancel := context.WithTimeout(ctx, oneShotTurnTimeout(runtime))
	defer cancel()
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, bridgeEventBuffer)
	driver := newSessionDriver(runtime.messages, runtime.cliMessages, sess.Name, nil, toBridge, fromBridge, runtime.warnings)
	oneShot := runtime.persists
	driver.persists = oneShot
	defer awaitOneShotWrites(runtime.messages, oneShot, runtime.warnings, runtime.languageOf(sess))
	driver.incarnation = sess.Incarnation
	driver.agent = sess.Agent
	driver.language = func() i18n.Language { return knownSessionLanguage(sess) }
	gate := attachOneShotTaps(driver, sess, runtime)
	defer gate.shut()
	runtime.sessions.attachCompaction(driver, sess.Name)
	terms := modelTermsFor(runtime.models)
	driver.terms = func() modelTerms { return terms(sess) }
	pumpCtx, pumpCancel := context.WithCancel(seedCtx)
	defer pumpCancel()
	pumpDone := make(chan struct{})
	go func() { defer close(pumpDone); driver.pump(pumpCtx) }()

	bridgeCtx, stopBridge := context.WithCancel(seedCtx)
	defer stopBridge()
	bridgeErr := make(chan error, 1)
	go runOneShotBridge(bridgeCtx, cancel, sess, orch, runtime, toBridge, fromBridge, bridgeErr)

	stopSeedWork := func() {
		pumpCancel()
		if !joinWithin(pumpDone, seedJoinGrace) {
			slog.Warn("seed pump outlived its grace", "session", sess.Name)
		}
		stopBridge()
	}
	reply, ok := driver.SeedAndWaitWithTurnID(seedCtx, task, turnID)
	if !ok {
		timeoutErr := oneShotTimeoutError(bridgeErr)
		stopSeedWork()
		return "", timeoutErr
	}
	driver.awaitTurnRelease(seedCtx)
	stopSeedWork()
	ended, bridgeRunErr := bridgeEndWithin(bridgeErr, seedJoinGrace)
	if !ended {
		slog.Warn("seed bridge outlived its grace", "session", sess.Name)
	}
	if reportsFailedTurn(bridgeRunErr) {
		return "", bridgeRunErr
	}
	if runtime.coordinator != nil {
		driver.coordinator = runtime.coordinator
		driver.activeTurnID = turnID
		if coordination := driver.maybeCoordinate(seedCtx, reply); coordination != nil {
			driver.fanOut(seedCtx, contracts.Event{T: eventwire.EventReply, Done: true, Text: reply, Coordination: coordination})
		}
		driver.activeTurnID = ""
	}
	consolidateOneShot(ctx, orch, gate, sess, runtime.messages, runtime.warnings, runtime.languageOf(sess))
	return reply, nil
}

func awaitOneShotWrites(messages *hostMessages, persists *persistWrites, warnings io.Writer, lang languageSource) {
	select {
	case <-persists.group.settled():
	case <-time.After(persists.grace):
		warnLive(messages, warnings, lang, "warn.persist_overrun", map[string]any{graceParam: persists.grace.String()})
	}
}

func oneShotTurnTimeout(runtime oneShotSeedRuntime) time.Duration {
	if runtime.timeout <= 0 {
		return seedTurnTimeout
	}
	return runtime.timeout
}

func attachOneShotTaps(driver *sessionDriver, sess state.Session, runtime oneShotSeedRuntime) *seedGate {
	gate := newSeedGate(runtime.messages, sess.Name, runtime.warnings, runtime.languageOf(sess))
	if runtime.record != nil {
		driver.sink.Transcript = func(entry state.TranscriptEntry) {
			gate.admit(lateTranscriptKey, func() { runtime.record(sess.Name, entry) })
		}
	}
	if runtime.publish != nil {
		driver.emitTap = func(e contracts.Event) {
			gate.admit(lateEventKey, func() { runtime.publish(sess.Name, e) })
		}
	}
	if runtime.recordUsage != nil {
		driver.sink.Usage = func(records []usage.Record) {
			gate.admit(lateUsageKey, func() { runtime.recordUsage(sess, records) })
		}
	}
	if runtime.resume != nil {
		driver.sink.Resume = func(token string) {
			gate.admit(lateResumeKey, func() { runtime.resume(sess.Name, token) })
		}
	}
	return gate
}

func runOneShotBridge(ctx context.Context, cancel context.CancelFunc, sess state.Session, orch contracts.Orchestrator, runtime oneShotSeedRuntime, toBridge, fromBridge chan contracts.Event, bridgeErr chan<- error) {
	if runtime.desk != nil {
		ctx = contracts.WithApprovalAsker(ctx, runtime.desk.asker(sess.Name))
	}
	err := bridge.RunUntilClosed(ctx, func(string) (contracts.Backend, error) {
		return runtime.backendFactory()(ctx, sess)
	}, orch, sess.ChannelID, toBridge, fromBridge)
	bridgeErr <- err
	if cancelsSeed(err) && ctx.Err() == nil {
		cancel()
	}
}

func cancelsSeed(err error) bool {
	return err != nil && !reportsFailedTurn(err)
}

func reportsFailedTurn(err error) bool {
	_, failedTurn := errors.AsType[*bridge.TurnFailedError](err)
	return failedTurn
}

func joinWithin(done <-chan struct{}, grace time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(grace):
		return false
	}
}

func bridgeEndWithin(bridgeErr <-chan error, grace time.Duration) (bool, error) {
	select {
	case err := <-bridgeErr:
		return true, err
	case <-time.After(grace):
		return false, nil
	}
}

func oneShotTimeoutError(bridgeErr <-chan error) error {
	select {
	case err := <-bridgeErr:
		if err != nil {
			return err
		}
	default:
	}
	return errSeedTimeout
}

func (r oneShotSeedRuntime) languageOf(sess state.Session) languageSource {
	if r.sessionLanguage != nil {
		return r.sessionLanguage(sess.Name)
	}
	return func() i18n.Language { return knownSessionLanguage(sess) }
}

func consolidateOneShot(ctx context.Context, orch contracts.Orchestrator, gate *seedGate, sess state.Session, messages *hostMessages, warn io.Writer, lang languageSource) {
	if orch == nil || sess.ConsolidateEvery > 0 {
		return
	}
	gate.shut()
	consCtx, consCancel := context.WithTimeout(ctx, seedConsolidateTimeout)
	started := time.Now()
	err := orch.Consolidate(consCtx)
	consCancel()
	if err != nil {
		warnTo(messages, warn, lang(), "seed.consolidate", map[string]any{nameParam: sess.Name, "Elapsed": time.Since(started).Round(time.Millisecond).String(), detailParam: fmt.Sprint(err)})
	}
}
