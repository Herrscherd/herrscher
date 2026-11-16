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

type warningAwait func(budget <-chan struct{}) error

type serializedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *serializedWriter) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(data)
}

type warningSink struct {
	out         *serializedWriter
	lostLine    func(lost int) string
	mu          sync.Mutex
	queue       []pendingWarning
	queuedBytes int
	dropped     int
	reported    int
	writing     bool
	stalled     bool
	closed      bool
	direct      bool
	directDone  chan struct{}
	idle        chan struct{}
}

func newWarningSink(out io.Writer, lostLine func(lost int) string) *warningSink {
	return &warningSink{out: &serializedWriter{w: warningWriter(out)}, lostLine: lostLine}
}

func (s *warningSink) Write(data []byte) (int, error) {
	budget, stop := budgetWithin(warningWriteBudget)
	defer stop()
	if err := s.Enqueue(data)(budget); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (s *warningSink) Enqueue(data []byte) warningAwait {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return func(budget <-chan struct{}) error { return s.lateWrite(data, budget) }
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
	return func(budget <-chan struct{}) error {
		s.awaitWritten(pending.written, budget)
		return nil
	}
}

func nothingToAwait(<-chan struct{}) error { return nil }

func (s *warningSink) fits(data []byte) bool {
	return len(data) <= warningEntryBytes && len(s.queue) < warningQueueLimit && s.queuedBytes+len(data) <= warningQueueBytes
}

func (s *warningSink) awaitWritten(written, budget <-chan struct{}) {
	select {
	case <-written:
		return
	default:
	}
	select {
	case <-written:
	case <-budget:
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
			logWriteFailure(s.out.Write([]byte(s.lostLine(lost) + "\n")))
		}
		logWriteFailure(s.out.Write(next.data))
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
	s.closed = true
	lost := s.dropped - s.reported
	s.reported = s.dropped
	s.mu.Unlock()
	if lost > 0 {
		s.announceLost(lost)
	}
}

func (s *warningSink) announceLost(lost int) {
	if err := s.announce(s.lostLine(lost)); err != nil {
		slog.Warn("warnings were lost and their summary could not be written", "lost", lost)
	}
}

func (s *warningSink) announce(text string) error {
	budget, stop := budgetWithin(warningWriteBudget)
	defer stop()
	if !s.claimDirectWithin(budget) {
		s.countDropped()
		slog.Warn("a warning was abandoned because an earlier write stayed blocked", "budget", warningWriteBudget)
		return errWarningAbandoned
	}
	return s.runDirect([]byte(text+"\n"), budget)
}

func (s *warningSink) lateWrite(data []byte, budget <-chan struct{}) error {
	if _, claimed := s.tryClaimDirect(); !claimed {
		s.countDropped()
		slog.Warn("a warning was abandoned because an earlier direct write is still blocked", "budget", warningWriteBudget)
		return errWarningAbandoned
	}
	return s.runDirect(data, budget)
}

func (s *warningSink) claimDirectWithin(budget <-chan struct{}) bool {
	for {
		inFlight, claimed := s.tryClaimDirect()
		if claimed {
			return true
		}
		select {
		case <-inFlight:
		case <-budget:
			return false
		}
	}
}

func (s *warningSink) tryClaimDirect() (<-chan struct{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.direct {
		return s.directDone, false
	}
	s.direct = true
	s.directDone = make(chan struct{})
	return nil, true
}

func (s *warningSink) releaseDirect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.direct = false
	close(s.directDone)
}

func (s *warningSink) runDirect(data []byte, budget <-chan struct{}) error {
	written := make(chan error, 1)
	go func() {
		_, err := s.out.Write(data)
		s.releaseDirect()
		written <- err
	}()
	select {
	case err := <-written:
		return err
	case <-budget:
		s.countDropped()
		slog.Warn("a warning was abandoned because its writer stayed blocked", "budget", warningWriteBudget)
		return errWarningAbandoned
	}
}

func (s *warningSink) countDropped() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropped++
}

func (s *warningSink) droppedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

func budgetWithin(budget time.Duration) (<-chan struct{}, func()) {
	expired := make(chan struct{})
	timer := time.AfterFunc(budget, func() { close(expired) })
	return expired, func() { timer.Stop() }
}
