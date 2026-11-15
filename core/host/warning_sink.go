package host

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"
)

const (
	warningQueueLimit  = 256
	warningQueueBytes  = 256 << 10
	warningEntryBytes  = 4 << 10
	warningWriteBudget = 250 * time.Millisecond
)

var errWarningAbandoned = errors.New("the warning writer stayed blocked: the warning was abandoned")

type pendingWarning struct {
	data    []byte
	written chan struct{}
}

type warningSink struct {
	out         io.Writer
	lostLine    func(lost int) string
	mu          sync.Mutex
	queue       []pendingWarning
	queuedBytes int
	dropped     int
	reported    int
	writing     bool
	stalled     bool
	closed      bool
	idle        chan struct{}
}

func newWarningSink(out io.Writer, lostLine func(lost int) string) *warningSink {
	return &warningSink{out: out, lostLine: lostLine}
}

func (s *warningSink) Write(data []byte) (int, error) {
	if err := s.Enqueue(data)(); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (s *warningSink) Enqueue(data []byte) func() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return func() error { return s.writeNow(data) }
	}
	if !s.fits(data) {
		s.dropped++
		return nothingToAwait
	}
	pending := pendingWarning{data: bytes.Clone(data), written: make(chan struct{})}
	s.queue = append(s.queue, pending)
	s.queuedBytes += len(data)
	if !s.writing {
		s.writing = true
		s.idle = make(chan struct{})
		go s.drain()
	}
	if s.stalled {
		return nothingToAwait
	}
	return func() error {
		s.awaitWritten(pending.written)
		return nil
	}
}

func nothingToAwait() error { return nil }

func (s *warningSink) fits(data []byte) bool {
	return len(data) <= warningEntryBytes && len(s.queue) < warningQueueLimit && s.queuedBytes+len(data) <= warningQueueBytes
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
	s.queuedBytes -= len(next.data)
	lost := s.dropped - s.reported
	s.reported = s.dropped
	return next, lost, true
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
	stalled := s.stalled
	s.mu.Unlock()
	if stalled {
		return waitWithin(idle, warningWriteBudget)
	}
	return window.finished(idle)
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

func (s *warningSink) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

func (s *warningSink) announce(text string) {
	logCleanup(s.writeNow([]byte(text + "\n")))
}

func (s *warningSink) writeNow(data []byte) error {
	written := make(chan error, 1)
	go func() {
		_, err := warningWriter(s.out).Write(data)
		written <- err
	}()
	timer := time.NewTimer(warningWriteBudget)
	defer timer.Stop()
	select {
	case err := <-written:
		return err
	case <-timer.C:
		slog.Warn("a warning was abandoned because its writer stayed blocked", "budget", warningWriteBudget)
		return errWarningAbandoned
	}
}

func (s *warningSink) droppedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}
