package host

import "sync"

type inflight struct {
	mu    sync.Mutex
	count int
	zero  chan struct{}
}

func (f *inflight) Add(delta int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	previous := f.count
	f.count = max(previous+delta, 0)
	if previous == 0 && f.count > 0 {
		f.zero = make(chan struct{})
	}
	if previous > 0 && f.count == 0 {
		close(f.zero)
	}
}

func (f *inflight) Done() {
	f.Add(-1)
}

func (f *inflight) Wait() {
	<-f.settled()
}

func (f *inflight) settled() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.count == 0 {
		done := make(chan struct{})
		close(done)
		return done
	}
	return f.zero
}

func (h *hub) awaitInFlight() {
	window, cancel := h.newGrace()
	defer cancel()
	if h.awaitInFlightWithin(window, &h.relaunches, &h.acceptFails) {
		h.flushWarningsWithin(window)
	}
}

func (h *hub) awaitInFlightWithin(window teardownWindow, groups ...*inflight) bool {
	for _, group := range groups {
		if !window.finished(group.settled()) {
			h.warnHubOverrun()
			return false
		}
	}
	return true
}

func (h *hub) flushWarningsWithin(window teardownWindow) {
	if h.sink != nil && !h.sink.flush(window) {
		h.warnHubOverrun()
	}
}

func (h *hub) warnHubOverrun() {
	warnLive(h.warnings, h.textLanguage(), "warn.hub_teardown_overrun", map[string]any{graceParam: h.teardownGrace.String()})
}
