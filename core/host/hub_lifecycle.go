package host

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

type liveSession struct {
	cancel   context.CancelFunc
	done     <-chan struct{}
	released <-chan struct{}
	release  func()
}

var errHubClosed = errors.New("the daemon is shutting down and starts no session")

func (h *hub) goLive(ctx context.Context, sess state.Session) error {
	h.lifecycle.Lock()
	defer h.releaseLifecycle()
	return h.goLiveLocked(ctx, sess)
}

func (h *hub) goLiveStarted(ctx context.Context, sess state.Session, persist func() error) error {
	retired, err := h.goLiveAndPersist(ctx, sess, persist)
	if retired {
		h.forget(sess.Name)
	}
	return err
}

func (h *hub) goLiveAndPersist(ctx context.Context, sess state.Session, persist func() error) (bool, error) {
	batch, err := h.goLiveAndPersistLocked(ctx, sess, persist)
	batch.await(h)
	return len(batch.retired) > 0, err
}

func (h *hub) goLiveAndPersistLocked(ctx context.Context, sess state.Session, persist func() error) (retiredBatch, error) {
	h.lifecycle.Lock()
	defer h.releaseLifecycle()
	batch := h.newBatch()
	wasLive := h.isLive(sess.Name)
	if err := h.goLiveLocked(ctx, sess); err != nil {
		return batch, err
	}
	if err := persist(); err != nil {
		if !wasLive {
			batch.retired = h.retireLocked(sess.Name, batch.window)
		}
		return batch, err
	}
	retired, err := h.startBridgeLocked(sess, wasLive, h.teardownGrace)
	batch.retired = retired
	return batch, err
}

func (h *hub) goLiveBridged(ctx context.Context, sess state.Session) error {
	batch, err := h.goLiveBridgedDeferred(ctx, sess)
	batch.await(h)
	return err
}

func (h *hub) goLiveBridgedDeferred(ctx context.Context, sess state.Session) (retiredBatch, error) {
	h.lifecycle.Lock()
	defer h.releaseLifecycle()
	batch := h.newBatch()
	retired, err := h.goLiveBridgedLocked(ctx, sess, batch.window)
	h.journalStart(sess.Name, err)
	batch.retired = retired
	return batch, err
}

func (h *hub) goLiveBridgedLocked(ctx context.Context, sess state.Session, window teardownWindow) ([]retirement, error) {
	retired, err := h.goLiveBridgedOnce(ctx, sess, window)
	if errors.Is(err, supervisor.ErrStillStopping) && h.stoppingBridge(sess.Name) == nil {
		again, retryErr := h.goLiveBridgedOnce(ctx, sess, window)
		retired, err = append(retired, again...), retryErr
	}
	if settled := h.stoppingBridge(sess.Name); errors.Is(err, supervisor.ErrStillStopping) && settled != nil {
		h.relaunchWhenSettled(ctx, sess.Name, settled)
	}
	return retired, err
}

func (h *hub) goLiveBridgedOnce(ctx context.Context, sess state.Session, window teardownWindow) ([]retirement, error) {
	wasLive := h.isLive(sess.Name)
	if err := h.goLiveLocked(ctx, sess); err != nil {
		return nil, err
	}
	retired, err := h.startBridgeLocked(sess, wasLive, 0)
	if err != nil {
		return retired, err
	}
	return append(retired, h.retireIfArchivedLocked(sess.Name, window)...), nil
}

func (h *hub) retireIfArchivedLocked(name string, window teardownWindow) []retirement {
	sess, ok := h.st.FindSession(name)
	if !ok || !sess.Archived {
		return nil
	}
	retired := h.retireLocked(name, window)
	h.forget(name)
	return retired
}

func (h *hub) retireLocked(name string, window teardownWindow) []retirement {
	live, dropped := h.dropLocked(name, nil)
	if !dropped {
		return nil
	}
	halted := h.haltBridgeWithin(name, window)
	return []retirement{{name: name, live: live, haltOverran: !halted}}
}

func (h *hub) forgetGoneRefusalsLocked() {
	for name, incarnation := range h.refused {
		if sess, ok := h.st.FindSession(name); !ok || sess.Incarnation != incarnation {
			delete(h.refused, name)
		}
	}
}

func (h *hub) journalStart(name string, err error) {
	if errors.Is(err, errHubClosed) {
		return
	}
	lang := h.sessionLanguageSource(name)()
	incarnation := h.incarnationOf(name)
	refusedIncarnation, known := h.refused[name]
	refused := known && refusedIncarnation == incarnation
	switch {
	case err == nil && refused:
		delete(h.refused, name)
		h.deferWarning(lang, "warn.session_start_recovered", map[string]any{nameParam: name})
	case err != nil && !refused:
		if h.refused == nil {
			h.refused = make(map[string]string)
		}
		h.refused[name] = incarnation
		h.deferWarning(lang, "warn.session_start", map[string]any{nameParam: name, detailParam: err})
	}
}

func (h *hub) deferWarning(lang i18n.Language, key string, params map[string]any) {
	line := []byte(newHostMessages().text(lang, key, params) + "\n")
	h.deferred = append(h.deferred, h.sink.Enqueue(line))
}

func (h *hub) releaseLifecycle() {
	pending := h.deferred
	h.deferred = nil
	h.lifecycle.Unlock()
	budget, stop := budgetWithin(warningWriteBudget)
	defer stop()
	for _, await := range pending {
		logCleanup(await(budget))
	}
}

func (h *hub) incarnationOf(name string) string {
	sess, ok := h.st.FindSession(name)
	if !ok {
		return ""
	}
	return sess.Incarnation
}

func (h *hub) stoppingBridge(name string) <-chan struct{} {
	if h.stopping == nil {
		return nil
	}
	return h.stopping(name)
}

func (h *hub) relaunchWhenSettled(ctx context.Context, name string, settled <-chan struct{}) {
	if _, pending := h.relaunching[name]; pending {
		return
	}
	if h.relaunching == nil {
		h.relaunching = make(map[string]struct{})
	}
	h.relaunching[name] = struct{}{}
	end := h.relaunches.Begin()
	go func() {
		defer end()
		select {
		case <-settled:
		case <-h.relaunchStop:
			return
		}
		h.relaunchSettled(ctx, name)
	}()
}

func (h *hub) relaunchSettled(ctx context.Context, name string) {
	batch, err := h.relaunchLocked(ctx, name)
	batch.await(h)
	logCleanup(err)
}

func (h *hub) relaunchLocked(ctx context.Context, name string) (retiredBatch, error) {
	h.lifecycle.Lock()
	defer h.releaseLifecycle()
	batch := h.newBatch()
	delete(h.relaunching, name)
	if h.isLive(name) {
		return batch, nil
	}
	retired, err := h.reviveLocked(ctx, name, batch.window)
	batch.retired = retired
	return batch, err
}

func (h *hub) reviveLocked(ctx context.Context, name string, window teardownWindow) ([]retirement, error) {
	sess, ok := h.st.FindSession(name)
	if !ok || sess.Archived {
		return nil, nil
	}
	retired, err := h.goLiveBridgedLocked(ctx, sess, window)
	h.journalStart(name, err)
	return retired, err
}

func (h *hub) closeLifecycleLocked() {
	if h.closed {
		return
	}
	h.closed = true
	if h.relaunchStop != nil {
		close(h.relaunchStop)
	}
}

func (h *hub) startBridgeLocked(sess state.Session, wasLive bool, grace time.Duration) ([]retirement, error) {
	if h.startBridge == nil {
		return nil, nil
	}
	err := h.startBridge(sess, grace)
	if err == nil || wasLive {
		return nil, err
	}
	live, dropped := h.dropLocked(sess.Name, nil)
	if !dropped {
		return nil, err
	}
	return []retirement{{name: sess.Name, live: live}}, err
}

func (h *hub) isLive(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.live[name]
	return ok
}

func (h *hub) goLiveLocked(ctx context.Context, sess state.Session) error {
	if h.runtime.Dir() == "" {
		return errNoRuntimeLayout
	}
	if h.closed {
		return errHubClosed
	}
	if h.isLive(sess.Name) {
		return nil
	}
	if h.stoppingBridge(sess.Name) != nil {
		return supervisor.ErrStillStopping
	}
	controlPath := control.SocketPath(h.runtime, h.instanceID, sess.Name)
	if err := ensureRuntimeDir(h.runtime, controlPath); err != nil {
		return err
	}
	listener, err := h.listen(context.WithoutCancel(ctx), controlPath)
	if err != nil {
		return wrapHost(err, "session.go_live", map[string]any{"Name": sess.Name})
	}
	acc := control.Serve(context.WithoutCancel(ctx), listener)
	h.noteListened(sess.Name)
	sctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	commands, err := openSocket(sctx, h.runtime, SessionCommandSocketPathIn(h.runtime, h.instanceID, sess.Name))
	if err != nil {
		cancel()
		logCleanup(acc.Close())
		return wrapHost(err, "session.go_live", map[string]any{"Name": sess.Name})
	}
	bound := boundGateways(h.gws, effectiveKinds(h.gws, sess))
	terms := liveModelTerms(h.st, h.models, sess.Name)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSessionIdentified(sctx, h.sessions, sess.Name, sess.ChannelID, bound, acc, state.ParticipantsPath(h.partDir, sess.Name), h.metrics, h.coordinator,
			h.liveSink(ctx, sess),
			eventTap(h.eventPublisher, sess.Name),
			sessionIdentity{incarnation: sess.Incarnation, agent: sess.Agent, terms: terms, language: liveSessionLanguage(h.st, sess.Name), operator: h.textLanguage()}, h.gate, h.warnings)
		if err := acc.Err(); err != nil {
			h.acceptFailed(sess.Name, done, err)
		}
	}()

	released := acceptCommands(sctx, commands, h.sessionCommands(sess.Name), defaultCommandReadTimeout)
	releaseSockets := sync.OnceFunc(func() {
		logCleanup(acc.Close())
		cancel()
		<-released
	})
	h.mu.Lock()
	h.live[sess.Name] = liveSession{cancel: cancel, done: done, released: released, release: releaseSockets}
	h.mu.Unlock()
	return nil
}

func (h *hub) stopLive() {
	h.lifecycle.Lock()
	h.closeLifecycleLocked()
	h.mu.Lock()
	lives := make(map[string]liveSession, len(h.live))
	maps.Copy(lives, h.live)
	h.mu.Unlock()
	h.lifecycle.Unlock()
	for _, live := range lives {
		live.cancel()
		live.release()
	}
	for name, live := range lives {
		window, cancel := h.newGrace()
		h.awaitTeardown(name, live, window)
		cancel()
	}
	h.awaitInFlight()
}

func (h *hub) noteListened(session string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listenedSessions == nil {
		h.listenedSessions = make(map[string]struct{})
	}
	h.listenedSessions[session] = struct{}{}
}

func (h *hub) closeAndRemoveSockets() error {
	h.lifecycle.Lock()
	h.closeLifecycleLocked()
	h.mu.Lock()
	sessions := slices.Collect(maps.Keys(h.listenedSessions))
	h.mu.Unlock()
	h.lifecycle.Unlock()
	h.awaitInFlight()
	h.closeWarnings()
	return removeSocketFiles(h.runtime, h.instanceID, sessions)
}

func (h *hub) closeWarnings() {
	window, cancel := h.newGrace()
	defer cancel()
	h.flushWarningsWithin(window)
	h.sink.Close()
}

func (h *hub) goDead(name string) {
	h.lifecycle.Lock()
	batch := h.newBatch()
	batch.retired = h.goDeadLocked(name, batch.window)
	h.releaseLifecycle()
	batch.await(h)
}

func (h *hub) goDeadLocked(name string, window teardownWindow) []retirement {
	live, dropped := h.dropLocked(name, nil)
	halted := h.haltBridgeWithin(name, window)
	h.forget(name)
	if !dropped {
		return nil
	}
	return []retirement{{name: name, live: live, haltOverran: !halted}}
}

func (h *hub) haltBridge(name string) {
	if h.stopBridge != nil {
		logCleanup(h.stopBridge(name))
	}
}

func (h *hub) acceptFailed(name string, done <-chan struct{}, cause error) {
	end, retired := h.retireFailed(name, done)
	if !retired {
		return
	}
	defer end()
	h.forget(name)
	warnLive(h.warnings, h.sessionLanguageSource(name), "warn.session_accept_failed", map[string]any{nameParam: name, detailParam: cause})
}

func (h *hub) retireFailed(name string, done <-chan struct{}) (func(), bool) {
	h.lifecycle.Lock()
	defer h.releaseLifecycle()
	if h.closed {
		return nil, false
	}
	if _, ok := h.dropLocked(name, done); !ok {
		return nil, false
	}
	end := h.acceptFails.Begin()
	h.haltBridgeWithinGrace(name)
	return end, true
}

func (h *hub) haltBridgeWithinGrace(name string) {
	window, cancel := h.newGrace()
	defer cancel()
	h.haltBridgeWithin(name, window)
}

func (h *hub) haltBridgeWithin(name string, window teardownWindow) bool {
	if h.stopBridge == nil {
		return true
	}
	return h.withinGrace(name, window, func() { h.haltBridge(name) })
}

func (h *hub) withinGrace(name string, window teardownWindow, step func()) bool {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		step()
	}()
	if window.finished(finished) {
		return true
	}
	h.deferWarning(h.sessionLanguageSource(name)(), "warn.session_teardown_overrun", h.overrunParams(name))
	return false
}

func (h *hub) warnOverrun(name string) {
	warnTo(h.warnings, h.sessionLanguageSource(name)(), "warn.session_teardown_overrun", h.overrunParams(name))
}

func (h *hub) overrunParams(name string) map[string]any {
	return map[string]any{nameParam: name, graceParam: h.teardownGrace.String()}
}

func (h *hub) dropLocked(name string, only <-chan struct{}) (liveSession, bool) {
	h.mu.Lock()
	live, ok := h.live[name]
	if ok && only != nil && live.done != only {
		ok = false
	}
	if ok {
		delete(h.live, name)
	}
	h.mu.Unlock()
	if ok {
		live.cancel()
		live.release()
	}
	return live, ok
}

func (h *hub) forget(name string) {
	if f, ok := h.coordinator.(forgetter); ok {
		f.forget(name)
	}
}

func (h *hub) awaitTeardown(name string, live liveSession, window teardownWindow) {
	for _, finished := range []<-chan struct{}{live.done, live.released} {
		if finished == nil {
			continue
		}
		if !window.finished(finished) {
			h.warnOverrun(name)
			return
		}
	}
}
