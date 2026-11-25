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
	record          func(session string, entry state.TranscriptEntry) error
	recordUsage     func(sess state.Session, records []usage.Record)
	resume          func(session, token string)
	timeout         time.Duration
	backend         func(context.Context, state.Session) (contracts.Backend, error)
	models          *ModelDiscovery
	sessions        *sessionTable
	desk            *approvalDesk
	localRun        bool
	persists        *persistWrites
	joinGrace       time.Duration
}

func (r oneShotSeedRuntime) grace() time.Duration {
	if r.joinGrace <= 0 {
		return seedJoinGrace
	}
	return r.joinGrace
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
	if err := requireApprovalDesk(runtime); err != nil {
		return "", err
	}
	orch, mem, err := seedOrchestrator(ctx, runtime.messages, sess)
	if err != nil {
		return "", err
	}
	return runOneShotSeedWithIDRuntime(ctx, sess, task, turnID, orch, runtime, mem)
}

func requireApprovalDesk(runtime oneShotSeedRuntime) error {
	if runtime.desk == nil && !runtime.localRun {
		return errSeedWithoutApprovalDesk
	}
	return nil
}

func runOneShotSeedWithIDRuntime(ctx context.Context, sess state.Session, task, turnID string, orch contracts.Orchestrator, runtime oneShotSeedRuntime, memory ...io.Closer) (string, error) {
	owned := append([]io.Closer{orch}, memory...)
	if err := requireApprovalDesk(runtime); err != nil {
		closeAll(owned)
		return "", err
	}
	bridgeJoined := false
	defer func() {
		if bridgeJoined {
			closeAll(owned)
		}
	}()
	seedCtx, cancel := context.WithTimeout(ctx, oneShotTurnTimeout(runtime))
	defer cancel()
	toBridge := make(chan contracts.Event, 1)
	bridgeIn := make(chan contracts.Event)
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
	runtime.sessions.attachTelemetry(driver)
	terms := modelTermsFor(runtime.models)
	driver.terms = func() modelTerms { return terms(sess) }
	pumpCtx, pumpCancel := context.WithCancel(seedCtx)
	defer pumpCancel()
	pumpDone := make(chan struct{})
	go func() { defer close(pumpDone); driver.pump(pumpCtx) }()

	forwardCtx, stopForward := context.WithCancel(seedCtx)
	defer stopForward()
	go forwardToBridge(forwardCtx, toBridge, bridgeIn)

	bridgeCtx, stopBridge := context.WithCancel(seedCtx)
	defer stopBridge()
	bridgeErr := make(chan error, 1)
	go runOneShotBridge(bridgeCtx, cancel, sess, orch, runtime, bridgeIn, fromBridge, bridgeErr)

	grace := runtime.grace()
	stopSeedWork := func() {
		pumpCancel()
		if !joinWithin(pumpDone, grace) {
			slog.Warn("seed pump outlived its grace", "session", sess.Name)
		}
		stopForward()
	}
	reply, ok := driver.SeedAndWaitWithTurnID(seedCtx, task, turnID)
	if ok {
		driver.awaitTurnRelease(seedCtx)
	}
	stopSeedWork()
	joined, bridgeRunErr := endBridge(seedCtx, bridgeErr, grace, stopBridge)
	if !joined {
		warnLive(runtime.messages, runtime.warnings, runtime.languageOf(sess), "warn.seed_bridge_unjoined", map[string]any{graceParam: grace.String()})
		go closeWhenBridgeEnds(bridgeErr, owned)
		if !ok {
			return "", errSeedTimeout
		}
		return reply, nil
	}
	bridgeJoined = true
	if err := seedReplyError(ok, bridgeRunErr); err != nil {
		return "", err
	}
	if runtime.coordinator != nil {
		coordinateSeedReply(seedCtx, driver, runtime.coordinator, turnID, reply)
	}
	consolidateOneShot(ctx, orch, gate, sess, runtime.messages, runtime.warnings, runtime.languageOf(sess))
	return reply, nil
}

func closeWhenBridgeEnds(bridgeErr <-chan error, owned []io.Closer) {
	logCleanup(<-bridgeErr)
	closeAll(owned)
}

func closeAll(closers []io.Closer) {
	for _, closer := range closers {
		if closer != nil {
			closeRead(closer)
		}
	}
}

func seedReplyError(ok bool, bridgeRunErr error) error {
	if !ok {
		if bridgeRunErr != nil {
			return bridgeRunErr
		}
		return errSeedTimeout
	}
	if reportsFailedTurn(bridgeRunErr) {
		return bridgeRunErr
	}
	return nil
}

func coordinateSeedReply(ctx context.Context, driver *sessionDriver, coordinator contracts.Coordinator, turnID, reply string) {
	driver.coordinator = coordinator
	driver.activeTurnID = turnID
	if coordination := driver.maybeCoordinate(ctx, reply); coordination != nil {
		driver.fanOut(ctx, contracts.Event{T: eventwire.EventReply, Done: true, Text: reply, Coordination: coordination})
	}
	driver.activeTurnID = ""
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
		driver.sink.Transcript = func(entry state.TranscriptEntry) error {
			var err error
			gate.admit(lateTranscriptKey, func() { err = runtime.record(sess.Name, entry) })
			return err
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

func forwardToBridge(ctx context.Context, from <-chan contracts.Event, bridgeIn chan<- contracts.Event) {
	defer close(bridgeIn)
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-from:
			select {
			case bridgeIn <- event:
			case <-ctx.Done():
				return
			}
		}
	}
}

func runOneShotBridge(ctx context.Context, cancel context.CancelFunc, sess state.Session, orch contracts.Orchestrator, runtime oneShotSeedRuntime, toBridge, fromBridge chan contracts.Event, bridgeErr chan<- error) {
	if runtime.desk != nil {
		ctx = contracts.WithApprovalAsker(ctx, runtime.desk.askerFor(seedConsumer, sess.Name))
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

func bridgeEndWithin(ctx context.Context, bridgeErr <-chan error, grace time.Duration) (bool, error) {
	select {
	case err := <-bridgeErr:
		return true, err
	case <-time.After(grace):
		return false, nil
	case <-ctx.Done():
		return false, nil
	}
}

func endBridge(ctx context.Context, bridgeErr <-chan error, grace time.Duration, stop func()) (bool, error) {
	if ended, err := bridgeEndWithin(ctx, bridgeErr, grace); ended {
		return true, err
	}
	stop()
	return bridgeEndWithin(context.WithoutCancel(ctx), bridgeErr, grace)
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
