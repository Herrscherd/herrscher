package host

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	warningQueueLimit  = 256
	warningWriteBudget = 250 * time.Millisecond
)

var (
	errWarningSinkClosed = errors.New("the warning sink is closed: the hub writes no more warnings")
	errWarningsUndrained = errors.New("warnings were still waiting for their writer when the sink closed")
)

type pendingWarning struct {
	data    []byte
	written chan struct{}
}

type warningSink struct {
	out      io.Writer
	lostLine func(lost int) string
	mu       sync.Mutex
	queue    []pendingWarning
	dropped  int
	reported int
	writing  bool
	stalled  bool
	closed   bool
	idle     chan struct{}
}

func newWarningSink(out io.Writer, lostLine func(lost int) string) *warningSink {
	return &warningSink{out: out, lostLine: lostLine}
}

func (s *warningSink) Write(data []byte) (int, error) {
	pending, mustWait, err := s.enqueue(data)
	if err != nil {
		return 0, err
	}
	if mustWait {
		s.awaitWritten(pending.written)
	}
	return len(data), nil
}

func (s *warningSink) enqueue(data []byte) (pendingWarning, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return pendingWarning{}, false, errWarningSinkClosed
	}
	if len(s.queue) >= warningQueueLimit {
		s.dropped++
		return pendingWarning{}, false, nil
	}
	pending := pendingWarning{data: bytes.Clone(data), written: make(chan struct{})}
	s.queue = append(s.queue, pending)
	if !s.writing {
		s.writing = true
		s.idle = make(chan struct{})
		go s.drain()
	}
	return pending, !s.stalled, nil
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
		next, lost, ok := s.takeNext()
		if !ok {
			return
		}
		if lost > 0 {
			logWriteFailure(warningWriter(s.out).Write([]byte(s.lostLine(lost) + "\n")))
		}
		logWriteFailure(warningWriter(s.out).Write(next.data))
		close(next.written)
		s.clearStalled()
	}
}

func (s *warningSink) takeNext() (pendingWarning, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		s.writing = false
		close(s.idle)
		return pendingWarning{}, 0, false
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	lost := s.dropped - s.reported
	s.reported = s.dropped
	return next, lost, true
}

func (s *warningSink) clearStalled() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stalled = false
}

func (s *warningSink) flush(window teardownWindow) (drained, graceExpired bool) {
	s.mu.Lock()
	if !s.writing {
		s.mu.Unlock()
		return true, false
	}
	idle := s.idle
	stalled := s.stalled
	s.mu.Unlock()
	if stalled {
		return waitWithin(idle, warningWriteBudget), false
	}
	if window.finished(idle) {
		return true, false
	}
	return false, true
}

func waitWithin(done <-chan struct{}, budget time.Duration) bool {
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (s *warningSink) Close(window teardownWindow) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if drained, _ := s.flush(window); !drained {
		return errWarningsUndrained
	}
	return nil
}

func (s *warningSink) droppedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}
