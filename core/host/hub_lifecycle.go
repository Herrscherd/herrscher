package host

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

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
	defer h.lifecycle.Unlock()
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
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	wasLive := h.isLive(sess.Name)
	if err := h.goLiveLocked(ctx, sess); err != nil {
		return false, err
	}
	if err := persist(); err != nil {
		if wasLive {
			return false, err
		}
		h.dropLocked(sess.Name, nil)
		h.haltBridgeWithinGrace(sess.Name)
		return true, err
	}
	return false, h.startBridgeLocked(sess, wasLive, h.teardownGrace)
}

func (h *hub) goLiveBridged(ctx context.Context, sess state.Session) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	err := h.goLiveBridgedLocked(ctx, sess)
	h.journalStart(sess.Name, err)
	return err
}

func (h *hub) goLiveBridgedLocked(ctx context.Context, sess state.Session) error {
	wasLive := h.isLive(sess.Name)
	err := h.goLiveLocked(ctx, sess)
	if err == nil {
		err = h.startBridgeLocked(sess, wasLive, 0)
	}
	if settled := h.stoppingBridge(sess.Name); errors.Is(err, supervisor.ErrStillStopping) && settled != nil {
		h.relaunchWhenSettled(ctx, sess.Name, settled)
	}
	return err
}

func (h *hub) journalStart(name string, err error) {
	if errors.Is(err, errHubClosed) {
		return
	}
	lang := h.sessionLanguageSource(name)
	_, refused := h.refused[name]
	switch {
	case err == nil && refused:
		delete(h.refused, name)
		warnLive(h.warnings, lang, "warn.session_start_recovered", map[string]any{nameParam: name})
	case err != nil && !refused:
		if h.refused == nil {
			h.refused = make(map[string]struct{})
		}
		h.refused[name] = struct{}{}
		warnLive(h.warnings, lang, "warn.session_start", map[string]any{nameParam: name, detailParam: err})
	}
}

func (h *hub) stoppingBridge(name string) <-chan struct{} {
	if h.stopping == nil {
		return nil
	}
	return h.stopping(name)
}

func (h *hub) relaunchWhenSettled(ctx context.Context, name string, settled <-chan struct{}) {
	h.relaunches.Add(1)
	go func() {
		defer h.relaunches.Done()
		select {
		case <-settled:
		case <-h.relaunchStop:
			return
		}
		sess, ok := h.st.FindSession(name)
		if !ok || sess.Archived {
			return
		}
		_ = h.goLiveBridged(ctx, sess)
	}()
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

func (h *hub) startBridgeLocked(sess state.Session, wasLive bool, grace time.Duration) error {
	if h.startBridge == nil {
		return nil
	}
	err := h.startBridge(sess, grace)
	if err != nil && !wasLive {
		h.dropLocked(sess.Name, nil)
	}
	return err
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
		closeRead(commands)
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
	h.relaunches.Wait()
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
	h.relaunches.Wait()
	return removeSocketFiles(h.runtime, h.instanceID, sessions)
}

func (h *hub) goDead(name string) {
	window, cancel := h.newGrace()
	h.goDeadWithin(name, window)
	cancel()
}

func (h *hub) goDeadWithin(name string, window teardownWindow) {
	h.lifecycle.Lock()
	live, ok := h.dropLocked(name, nil)
	h.haltBridgeWithin(name, window)
	h.lifecycle.Unlock()
	if ok {
		h.awaitTeardown(name, live, window)
	}
	h.forget(name)
}

func (h *hub) haltBridge(name string) {
	if h.stopBridge != nil {
		logCleanup(h.stopBridge(name))
	}
}

func (h *hub) acceptFailed(name string, done <-chan struct{}, cause error) {
	if !h.retireFailed(name, done) {
		return
	}
	h.forget(name)
	warnLive(h.warnings, h.sessionLanguageSource(name), "warn.session_accept_failed", map[string]any{nameParam: name, detailParam: cause})
}

func (h *hub) retireFailed(name string, done <-chan struct{}) bool {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	if _, ok := h.dropLocked(name, done); !ok {
		return false
	}
	h.haltBridgeWithinGrace(name)
	return true
}

type teardownWindow struct {
	expired <-chan struct{}
	ends    <-chan struct{}
}

const teardownLateShare = 3

func (h *hub) newGrace() (teardownWindow, context.CancelFunc) {
	budget, cancelBudget := context.WithTimeout(context.Background(), h.teardownGrace-h.teardownGrace/teardownLateShare)
	ends, cancelEnds := context.WithTimeout(context.Background(), h.teardownGrace)
	return teardownWindow{expired: budget.Done(), ends: ends.Done()}, func() {
		cancelBudget()
		cancelEnds()
	}
}

func (w teardownWindow) finished(done <-chan struct{}) bool {
	if isClosed(done) {
		return true
	}
	if isClosed(w.expired) {
		return finishedBefore(done, w.ends)
	}
	select {
	case <-done:
		return true
	case <-w.expired:
		return isClosed(done)
	}
}

func finishedBefore(done <-chan struct{}, ends <-chan struct{}) bool {
	select {
	case <-done:
		return true
	case <-ends:
		return isClosed(done)
	}
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func (h *hub) haltBridgeWithinGrace(name string) {
	window, cancel := h.newGrace()
	defer cancel()
	h.haltBridgeWithin(name, window)
}

func (h *hub) haltBridgeWithin(name string, window teardownWindow) {
	if h.stopBridge != nil {
		h.withinGrace(name, window, func() { h.haltBridge(name) })
	}
}

func (h *hub) withinGrace(name string, window teardownWindow, step func()) {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		step()
	}()
	if !window.finished(finished) {
		h.warnOverrun(name)
	}
}

func (h *hub) warnOverrun(name string) {
	warnLive(h.warnings, h.sessionLanguageSource(name), "warn.session_teardown_overrun", map[string]any{nameParam: name, graceParam: h.teardownGrace.String()})
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

func (h *hub) reconcile(ctx context.Context) {
	persisted := make(map[string]state.Session)
	sessions := h.st.SnapshotSessions()
	for idx := range sessions {
		s := sessions[idx]
		persisted[s.Name] = s
	}
	h.mu.Lock()
	liveNames := make([]string, 0, len(h.live))
	for n := range h.live {
		liveNames = append(liveNames, n)
	}
	h.mu.Unlock()
	window, cancel := h.newGrace()
	defer cancel()
	for _, name := range liveNames {
		s, ok := persisted[name]
		if !ok || s.Archived {
			h.goDeadWithin(name, window)
		}
	}
	for idx := range persisted {
		saved := persisted[idx]
		if saved.Archived {
			continue
		}
		if errors.Is(h.goLiveBridged(ctx, saved), errHubClosed) {
			break
		}
	}
}
