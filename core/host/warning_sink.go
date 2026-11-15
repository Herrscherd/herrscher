package host

import (
	"bytes"
	"io"
	"sync"
	"time"
)

const (
	warningQueueLimit  = 256
	warningWriteBudget = 250 * time.Millisecond
)

type pendingWarning struct {
	data    []byte
	written chan struct{}
}

type warningSink struct {
	out     io.Writer
	mu      sync.Mutex
	queue   []pendingWarning
	dropped int
	writing bool
	stalled bool
	idle    chan struct{}
}

func newWarningSink(out io.Writer) *warningSink {
	return &warningSink{out: out}
}

func (s *warningSink) Write(data []byte) (int, error) {
	pending, queued, mustWait := s.enqueue(data)
	if queued && mustWait {
		s.awaitWritten(pending.written)
	}
	return len(data), nil
}

func (s *warningSink) enqueue(data []byte) (pendingWarning, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) >= warningQueueLimit {
		s.dropped++
		return pendingWarning{}, false, false
	}
	pending := pendingWarning{data: bytes.Clone(data), written: make(chan struct{})}
	s.queue = append(s.queue, pending)
	if !s.writing {
		s.writing = true
		s.idle = make(chan struct{})
		go s.drain()
	}
	return pending, true, !s.stalled
}

func (s *warningSink) awaitWritten(written <-chan struct{}) {
	timer := time.NewTimer(warningWriteBudget)
	defer timer.Stop()
	select {
	case <-written:
	case <-timer.C:
		s.markStalledUnless(written)
	}
}

func (s *warningSink) markStalledUnless(written <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-written:
	default:
		s.stalled = true
	}
}

func (s *warningSink) drain() {
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.writing = false
			close(s.idle)
			s.mu.Unlock()
			return
		}
		next := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		logWriteFailure(warningWriter(s.out).Write(next.data))
		close(next.written)
		s.clearStalled()
	}
}

func (s *warningSink) clearStalled() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stalled = false
}

func (s *warningSink) flush(window teardownWindow) bool {
	s.mu.Lock()
	if !s.writing {
		s.mu.Unlock()
		return true
	}
	idle := s.idle
	s.mu.Unlock()
	return window.finished(idle)
}

func (s *warningSink) droppedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}
