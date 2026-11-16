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

var (
	errWarningAbandoned = errors.New("the warning writer stayed blocked: the warning was abandoned")
	errWarningLate      = errors.New("the warning writer stayed blocked: the warning was written late")
)

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
	summarizing bool
	writing     bool
	stalled     bool
	closed      bool
	direct      chan struct{}
	idle        chan struct{}
}

func newWarningSink(out io.Writer, lostLine func(lost int) string) *warningSink {
	return &warningSink{
		out:      &serializedWriter{w: warningWriter(out)},
		lostLine: lostLine,
		direct:   make(chan struct{}, 1),
	}
}

func (s *warningSink) Write(data []byte) (int, error) {
	owned := bytes.Clone(data)
	budget, stop := budgetWithin(warningWriteBudget)
	defer stop()
	if err := s.Enqueue(owned)(budget); err != nil {
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
	pending := pendingWarning{data: data, written: make(chan struct{})}
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
		next, ok := s.takeNext()
		if !ok {
			return
		}
		s.reportLost()
		logWriteFailure(s.out.Write(next.data))
		close(next.written)
		s.clearStalled()
	}
}

func (s *warningSink) takeNext() (pendingWarning, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		s.writing = false
		close(s.idle)
		return pendingWarning{}, false
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	s.queuedBytes -= len(next.data)
	return next, true
}

func (s *warningSink) reportLost() {
	lost, claimed := s.claimSummary()
	if !claimed {
		return
	}
	_, err := s.out.Write([]byte(s.lostLine(lost) + "\n"))
	s.settleSummary(lost, err == nil)
	if err != nil {
		slog.Warn("warnings were lost and their summary could not be written", "lost", s.unreported())
	}
}

func (s *warningSink) claimSummary() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lost := s.dropped - s.reported
	if s.summarizing || lost == 0 {
		return 0, false
	}
	s.summarizing = true
	return lost, true
}

func (s *warningSink) settleSummary(lost int, written bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summarizing = false
	if written {
		s.reported += lost
	}
}

func (s *warningSink) unreported() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped - s.reported
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
	s.mu.Unlock()
	lost, claimed := s.claimSummary()
	if !claimed {
		return
	}
	err := s.announce(s.lostLine(lost))
	written := err == nil || errors.Is(err, errWarningLate)
	s.settleSummary(lost, written)
	if !written {
		slog.Warn("warnings were lost and their summary could not be written", "lost", s.unreported())
	}
}

func (s *warningSink) announce(text string) error {
	budget, stop := budgetWithin(warningWriteBudget)
	defer stop()
	return s.runDirect([]byte(text+"\n"), s.directBudget(budget))
}

func (s *warningSink) lateWrite(data []byte, budget <-chan struct{}) error {
	return s.runDirect(data, s.directBudget(budget))
}

func (s *warningSink) directBudget(budget <-chan struct{}) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stalled {
		expired := make(chan struct{})
		close(expired)
		return expired
	}
	return budget
}

func (s *warningSink) runDirect(data []byte, budget <-chan struct{}) error {
	if !s.acquireDirect(budget) {
		s.countDropped()
		slog.Warn("a warning was abandoned because its writer stayed blocked", "budget", warningWriteBudget)
		return errWarningAbandoned
	}
	written := make(chan error, 1)
	go func() {
		_, err := s.out.Write(data)
		<-s.direct
		written <- err
	}()
	select {
	case err := <-written:
		return err
	case <-budget:
		return errWarningLate
	}
}

func (s *warningSink) acquireDirect(budget <-chan struct{}) bool {
	select {
	case s.direct <- struct{}{}:
		return true
	default:
	}
	select {
	case s.direct <- struct{}{}:
		return true
	case <-budget:
		return false
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
