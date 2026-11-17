package host

import (
	"sync"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/service"
)

const persistWriteGrace = service.StopPersistGrace

type persistWrites struct {
	group   inflight
	mu      sync.Mutex
	expired chan struct{}
	timer   *time.Timer
	warned  bool
	onLate  func()
	grace   time.Duration
	parent  *persistWrites
}

func newPersistWrites() *persistWrites {
	return &persistWrites{grace: persistWriteGrace}
}

func (p *persistWrites) child() *persistWrites {
	if p == nil {
		return nil
	}
	return &persistWrites{grace: p.grace, parent: p}
}

func (p *persistWrites) begin() func() {
	if p == nil {
		return func() {}
	}
	if p.pastDeadline() && p.onLate != nil {
		p.onLate()
	}
	end := p.group.Begin()
	if p.parent == nil {
		return end
	}
	endParent := p.parent.begin()
	return func() {
		end()
		endParent()
	}
}

func (p *persistWrites) deadline() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.expired == nil {
		p.expired = make(chan struct{})
		p.timer = time.AfterFunc(p.grace, p.expire)
	}
	return p.expired
}

func (p *persistWrites) expire() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeExpiredLocked()
}

func (p *persistWrites) stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer != nil {
		p.timer.Stop()
	}
	p.closeExpiredLocked()
}

func (p *persistWrites) closeExpiredLocked() {
	if p.expired == nil {
		p.expired = make(chan struct{})
	}
	if !isClosed(p.expired) {
		close(p.expired)
	}
}

func (p *persistWrites) pastDeadline() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.expired != nil && isClosed(p.expired)
}

func (p *persistWrites) claimWarning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.warned {
		return false
	}
	p.warned = true
	return true
}

func (h *hub) appendTranscript(session string, entry state.TranscriptEntry) error {
	return h.appendTranscriptWith(h.persists, session, entry)
}

func (h *hub) appendTranscriptWith(persists *persistWrites, session string, entry state.TranscriptEntry) error {
	end := persists.begin()
	defer end()
	return state.AppendTranscript(state.TranscriptPath(h.partDir, session), entry)
}

func (h *hub) awaitPersists() {
	expired := h.persists.deadline()
	if !h.awaitInFlightWithin(teardownWindow{expired: expired, ends: expired}, &h.persists.group) {
		h.warnPersistOverrunOnce()
	}
}

func (h *hub) warnPersistOverrunOnce() {
	if h.persists.claimWarning() {
		h.warnOverrun("warn.persist_overrun", h.persists.grace)
	}
}
