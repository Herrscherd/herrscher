package host

import (
	"sync"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const persistWriteGrace = 2 * time.Second

type persistWrites struct {
	group   inflight
	mu      sync.Mutex
	expired chan struct{}
	warned  bool
	onLate  func()
}

func newPersistWrites() *persistWrites {
	return &persistWrites{}
}

func (p *persistWrites) begin() func() {
	if p.pastDeadline() && p.onLate != nil {
		p.onLate()
	}
	return p.group.Begin()
}

func (p *persistWrites) deadline() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.expired == nil {
		expired := make(chan struct{})
		p.expired = expired
		time.AfterFunc(persistWriteGrace, func() { close(expired) })
	}
	return p.expired
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
	end := h.persists.begin()
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
		h.warnOverrun("warn.persist_overrun", persistWriteGrace)
	}
}
