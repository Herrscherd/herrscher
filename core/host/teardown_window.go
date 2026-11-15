package host

import (
	"context"
	"sync"
)

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

func (h *hub) awaitInFlight() {
	window, cancel := h.newGrace()
	defer cancel()
	h.awaitInFlightWithin(window, &h.relaunches, &h.acceptFails)
}

func (h *hub) awaitInFlightWithin(window teardownWindow, groups ...*sync.WaitGroup) {
	settled := make(chan struct{})
	go func() {
		defer close(settled)
		for _, group := range groups {
			group.Wait()
		}
	}()
	if !window.finished(settled) {
		go h.warnHubOverrun()
	}
}

func (h *hub) warnHubOverrun() {
	warnLive(h.warnings, h.textLanguage(), "warn.hub_teardown_overrun", map[string]any{graceParam: h.teardownGrace.String()})
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

func finishedBefore(done, ends <-chan struct{}) bool {
	select {
	case <-done:
		return true
	case <-ends:
		return isClosed(done)
	}
}

func isClosed(signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	default:
		return false
	}
}
