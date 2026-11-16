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
	errWarningAbandoned = errors.New("the warning writer stayed blocked: the warning was lost")
	errWarningLate      = errors.New("the warning writer stayed blocked: the warning is not confirmed")
)

type pendingWarning struct {
	data    []byte
	written chan struct{}
	err     error
}

type warningAwait func(budget <-chan struct{}) error

type serializedWriter struct {
	gate chan struct{}
	w    io.Writer
}

func newSerializedWriter(w io.Writer) *serializedWriter {
	return &serializedWriter{gate: make(chan struct{}, 1), w: w}
}

func (s *serializedWriter) acquire(budget <-chan struct{}) bool {
	select {
	case s.gate <- struct{}{}:
		return true
	default:
	}
	select {
	case s.gate <- struct{}{}:
		return true
	case <-budget:
		return false
	}
}

func (s *serializedWriter) release() {
	<-s.gate
}

type warningSink struct {
	out         *serializedWriter
	lostLine    func(lost int) string
	mu          sync.Mutex
	queue       []*pendingWarning
	queuedBytes int
	dropped     int
	reported    int
	unconfirmed int
	summarizing bool
	writing     bool
	stalled     bool
	closed      bool
	idle        chan struct{}
}

func newWarningSink(out io.Writer, lostLine func(lost int) string) *warningSink {
	return &warningSink{
		out:      newSerializedWriter(warningWriter(out)),
		lostLine: lostLine,
	}
}

func (s *warningSink) Write(data []byte) (int, error) {
	owned := bytes.Clone(data)
	budget, stop := writeBudget()
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
		return warningAbandoned
	}
	pending := &pendingWarning{data: data, written: make(chan struct{})}
	s.queue = append(s.queue, pending)
	s.queuedBytes += len(data)
	if !s.writing {
		s.writing = true
		s.idle = make(chan struct{})
		go s.drain()
	}
	if s.stalled {
		return warningUnconfirmed
	}
	return func(budget <-chan struct{}) error {
		if s.awaitWritten(pending.written, budget) {
			return pending.err
		}
		return errWarningLate
	}
}

func warningAbandoned(<-chan struct{}) error { return errWarningAbandoned }

func warningUnconfirmed(<-chan struct{}) error { return errWarningLate }

func (s *warningSink) fits(data []byte) bool {
	return len(data) <= warningEntryBytes && len(s.queue) < warningQueueLimit && s.queuedBytes+len(data) <= warningQueueBytes
}

func (s *warningSink) awaitWritten(written, budget <-chan struct{}) bool {
	select {
	case <-written:
		return true
	default:
	}
	select {
	case <-written:
		return true
	case <-budget:
		return s.markStalledUnless(written)
	}
}

func (s *warningSink) markStalledUnless(written <-chan struct{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-written:
		return true
	default:
		s.stalled = true
		return false
	}
}

func (s *warningSink) drain() {
	for {
		s.out.acquire(nil)
		next, ok := s.takeNext()
		if !ok {
			s.out.release()
			return
		}
		s.reportLost()
		written, err := s.out.w.Write(next.data)
		s.settleWrite(err)
		logWriteFailure(written, err)
		s.out.release()
		next.err = err
		close(next.written)
		s.clearStalled()
	}
}

func (s *warningSink) takeNext() (*pendingWarning, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		s.writing = false
		close(s.idle)
		return nil, false
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	s.queuedBytes -= len(next.data)
	s.unconfirmed++
	return next, true
}

func (s *warningSink) reserve() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unconfirmed++
}

func (s *warningSink) settleWrite(err error) {
	s.mu.Lock()
	s.unconfirmed--
	if err != nil {
		s.dropped++
	}
	closed := s.closed
	s.mu.Unlock()
	if err != nil && closed {
		s.logUnsettled()
	}
}

func (s *warningSink) abandonWrite() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unconfirmed--
	s.dropped++
}

func (s *warningSink) reportLost() {
	lost, claimed := s.claimSummary()
	if !claimed {
		return
	}
	s.reserve()
	_, err := s.out.w.Write([]byte(s.lostLine(lost) + "\n"))
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
	s.unconfirmed--
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
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.dropped += len(s.queue)
	for _, pending := range s.queue {
		pending.err = errWarningAbandoned
		close(pending.written)
	}
	s.queue = nil
	s.queuedBytes = 0
	s.mu.Unlock()
	budget, stop := writeBudget()
	defer stop()
	s.announceLost(budget)
	s.logUnsettled()
}

func (s *warningSink) announceLost(budget <-chan struct{}) {
	lost, claimed := s.claimSummary()
	if !claimed {
		return
	}
	s.reserve()
	if !s.out.acquire(budget) {
		s.settleSummary(lost, false)
		return
	}
	written := make(chan struct{})
	go func() {
		_, err := s.out.w.Write([]byte(s.lostLine(lost) + "\n"))
		s.out.release()
		s.settleSummary(lost, err == nil)
		close(written)
	}()
	select {
	case <-written:
	case <-budget:
	}
}

func (s *warningSink) logUnsettled() {
	s.mu.Lock()
	lost := s.dropped - s.reported
	unconfirmed := s.unconfirmed
	s.mu.Unlock()
	if lost > 0 || unconfirmed > 0 {
		slog.Warn("warnings were not all confirmed when the sink closed", "lost", lost, "unconfirmed", unconfirmed)
	}
}

func (s *warningSink) announce(text string) error {
	budget, stop := writeBudget()
	defer stop()
	return s.afterClose(s.directWrite([]byte(text+"\n"), s.directBudget(budget)))
}

func (s *warningSink) lateWrite(data []byte, budget <-chan struct{}) error {
	return s.afterClose(s.directWrite(data, s.directBudget(budget)))
}

func (s *warningSink) afterClose(err error) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed && (errors.Is(err, errWarningAbandoned) || errors.Is(err, errWarningLate)) {
		s.logUnsettled()
	}
	return err
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

func (s *warningSink) directWrite(data []byte, budget <-chan struct{}) error {
	s.reserve()
	if !s.out.acquire(budget) {
		s.abandonWrite()
		slog.Warn("a warning was abandoned because its writer stayed blocked", "budget", warningWriteBudget)
		return errWarningAbandoned
	}
	written := make(chan error, 1)
	go func() {
		_, err := s.out.w.Write(data)
		s.out.release()
		s.settleWrite(err)
		written <- err
	}()
	select {
	case err := <-written:
		return err
	case <-budget:
		return errWarningLate
	}
}

func (s *warningSink) droppedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

func writeBudget() (<-chan struct{}, func()) {
	expired := make(chan struct{})
	timer := time.AfterFunc(warningWriteBudget, func() { close(expired) })
	return expired, func() { timer.Stop() }
}
