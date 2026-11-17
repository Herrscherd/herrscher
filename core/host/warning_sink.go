package host

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

const (
	warningQueueLimit   = 256
	warningQueueBytes   = 256 << 10
	warningEntryBytes   = 4 << 10
	warningWriteBudget  = 250 * time.Millisecond
	warningCommandLimit = 64
)

var (
	errWarningAbandoned = errors.New("the warning writer stayed blocked: the warning was lost")
	errWarningLate      = errors.New("the warning writer stayed blocked: the warning is not confirmed")
)

type warningAwait func(budget <-chan struct{}) error

type warning struct {
	data   []byte
	direct bool
	done   chan struct{}
	err    error
}

type inflightWrite struct {
	entry   *warning
	lost    int
	stalled bool
}

func (w *inflightWrite) isSummary() bool {
	return w.entry == nil
}

type verdict struct {
	err  error
	lost int
}

type bilan struct {
	lost        int
	unconfirmed int
}

type sinkQuiet struct {
	idle    <-chan struct{}
	stalled bool
}

type warningSink struct {
	out          io.Writer
	lostLine     func(lost int) string
	commands     chan func()
	running      atomic.Bool
	queue        []*warning
	queuedBytes  int
	inflight     *inflightWrite
	idle         []chan struct{}
	dropped      int
	reported     int
	closed       bool
	sealed       bool
	finished     chan struct{}
	lastLogged   chan struct{}
	idleWait     time.Duration
	stillRunning func() string
	log          boundedLog
}

type sinkOption func(*warningSink)

func withIdleWait(wait time.Duration) sinkOption {
	return func(s *warningSink) { s.idleWait = wait }
}

func withStillRunningLine(line func() string) sinkOption {
	return func(s *warningSink) { s.stillRunning = line }
}

func newWarningSink(out io.Writer, lostLine func(lost int) string, opts ...sinkOption) *warningSink {
	created := &warningSink{
		out:          warningWriter(out),
		lostLine:     lostLine,
		commands:     make(chan func(), warningCommandLimit),
		finished:     make(chan struct{}),
		idleWait:     warningWriteBudget,
		stillRunning: englishStillRunningLine,
	}
	for _, opt := range opts {
		opt(created)
	}
	return created
}

func (s *warningSink) run() {
	for {
		select {
		case command := <-s.commands:
			command()
		default:
			s.running.Store(false)
			if len(s.commands) == 0 || !s.running.CompareAndSwap(false, true) {
				return
			}
		}
	}
}

func (s *warningSink) dispatch(task func()) {
	s.commands <- task
	if s.running.CompareAndSwap(false, true) {
		go s.run()
	}
}

func ask[T any](sink *warningSink, task func() T) T {
	reply := make(chan T, 1)
	sink.dispatch(func() { reply <- task() })
	return <-reply
}

func (s *warningSink) Write(data []byte) (int, error) {
	budget, stop := writeBudget()
	defer stop()
	if err := s.submit(bytes.Clone(data), false)(budget); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (s *warningSink) Enqueue(data []byte) warningAwait {
	return s.submit(data, false)
}

func (s *warningSink) submit(data []byte, direct bool) warningAwait {
	entry := &warning{data: data, direct: direct, done: make(chan struct{})}
	admitted := ask(s, func() verdict { return s.admit(entry) })
	s.logNotConfirmed(admitted.lost, 0)
	if admitted.err != nil {
		return func(<-chan struct{}) error { return admitted.err }
	}
	return func(budget <-chan struct{}) error { return s.await(entry, budget) }
}

func (s *warningSink) await(entry *warning, budget <-chan struct{}) error {
	select {
	case <-entry.done:
		return entry.err
	case <-budget:
	}
	gaveUp := ask(s, func() verdict { return s.giveUp(entry) })
	s.logNotConfirmed(gaveUp.lost, 0)
	return gaveUp.err
}

func (s *warningSink) admit(entry *warning) verdict {
	direct := entry.direct || s.closed
	if s.sealed {
		return verdict{err: errWarningAbandoned, lost: s.lose()}
	}
	if len(entry.data) > warningEntryBytes || len(s.queue) >= warningQueueLimit || s.queuedBytes+len(entry.data) > warningQueueBytes {
		return verdict{err: errWarningAbandoned, lost: s.lose()}
	}
	if s.stalledInflight() {
		if direct {
			return verdict{err: errWarningAbandoned, lost: s.lose()}
		}
		s.enqueue(entry)
		return verdict{err: errWarningLate}
	}
	s.enqueue(entry)
	s.pump(false)
	return verdict{}
}

func (s *warningSink) enqueue(entry *warning) {
	s.queue = append(s.queue, entry)
	s.queuedBytes += len(entry.data)
}

func (s *warningSink) giveUp(entry *warning) verdict {
	if settled(entry) {
		return verdict{err: entry.err}
	}
	direct := entry.direct || s.closed
	if s.isInflight(entry) {
		if !direct {
			s.markStalled()
		}
		return verdict{err: errWarningLate}
	}
	if !direct {
		s.markStalled()
		return verdict{err: errWarningLate}
	}
	s.removeQueued(entry)
	lost := s.lose()
	finish(entry, errWarningAbandoned)
	s.pump(false)
	return verdict{err: errWarningAbandoned, lost: lost}
}

func (s *warningSink) removeQueued(entry *warning) {
	s.queue = slices.DeleteFunc(s.queue, func(queued *warning) bool { return queued == entry })
	s.queuedBytes -= len(entry.data)
}

func (s *warningSink) isInflight(entry *warning) bool {
	return s.inflight != nil && s.inflight.entry == entry
}

func (s *warningSink) stalledInflight() bool {
	return s.inflight != nil && s.inflight.stalled
}

func (s *warningSink) markStalled() {
	if s.inflight != nil {
		s.inflight.stalled = true
	}
}

func (s *warningSink) owed() int {
	return s.dropped - s.reported
}

func (s *warningSink) lose() int {
	s.dropped++
	if s.sealed {
		return s.takeDelta()
	}
	return 0
}

func (s *warningSink) takeDelta() int {
	lost := s.owed()
	s.reported = s.dropped
	return lost
}

func (s *warningSink) pump(summarized bool) {
	if s.inflight != nil {
		return
	}
	if !summarized && s.owed() > 0 && (len(s.queue) > 0 || s.closed) {
		s.startSummary()
		return
	}
	if len(s.queue) > 0 {
		next := s.queue[0]
		s.queue = s.queue[1:]
		s.queuedBytes -= len(next.data)
		s.start(&inflightWrite{entry: next}, next.data)
		return
	}
	s.wakeIdle()
}

func (s *warningSink) startSummary() {
	lost := s.owed()
	s.reported += lost
	s.start(&inflightWrite{lost: lost}, []byte(s.lostLine(lost)+"\n"))
}

func (s *warningSink) start(write *inflightWrite, data []byte) {
	s.inflight = write
	logged := make(chan struct{})
	s.lastLogged = logged
	go func() {
		defer close(logged)
		written, err := s.out.Write(data)
		logWriteFailure(written, err)
		lost := ask(s, func() int { return s.settle(write, err) })
		s.logNotConfirmed(lost, 0)
	}()
}

func (s *warningSink) settle(write *inflightWrite, err error) int {
	s.inflight = nil
	if write.isSummary() {
		return s.settleSummary(write, err)
	}
	finish(write.entry, err)
	lost := 0
	if err != nil {
		lost = s.lose()
	}
	s.pump(false)
	return lost
}

func (s *warningSink) settleSummary(write *inflightWrite, err error) int {
	lost := 0
	if err != nil {
		s.reported -= write.lost
		if s.sealed {
			lost = s.takeDelta()
		}
	}
	s.pump(true)
	return lost
}

func (s *warningSink) idleSignal() <-chan struct{} {
	idle := make(chan struct{})
	if s.inflight == nil && len(s.queue) == 0 {
		close(idle)
		return idle
	}
	s.idle = append(s.idle, idle)
	return idle
}

func (s *warningSink) wakeIdle() {
	for _, idle := range s.idle {
		close(idle)
	}
	s.idle = nil
}

func (s *warningSink) quiet() sinkQuiet {
	return ask(s, func() sinkQuiet {
		return sinkQuiet{idle: s.idleSignal(), stalled: s.stalledInflight()}
	})
}

func (s *warningSink) flush(window teardownWindow) bool {
	quiet := s.quiet()
	if quiet.stalled {
		return waitWithin(quiet.idle, warningWriteBudget)
	}
	return window.finished(quiet.idle)
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
	if !ask(s, s.beginClose) {
		<-s.finished
		return
	}
	defer close(s.finished)
	budget, stop := budgetAfter(s.idleWait)
	defer stop()
	s.waitIdle(budget)
	taken := ask(s, s.seal)
	s.logNotConfirmed(taken.lost, taken.unconfirmed)
	if !s.joinLastWrite() {
		note := s.stillRunning()
		s.log.reserve(func() { slog.Warn("a warning write was still running when Close returned", "note", note) })
	}
	logged, stopLogged := writeBudget()
	defer stopLogged()
	s.log.drain(logged)
}

func englishStillRunningLine() string {
	return newHostMessages().text(i18n.English, "warn.write_still_running", nil)
}

func (s *warningSink) joinLastWrite() bool {
	logged := ask(s, func() chan struct{} { return s.lastLogged })
	if logged == nil {
		return true
	}
	budget, stop := writeBudget()
	defer stop()
	return waitLogged(logged, budget)
}

func waitLogged(logged, budget <-chan struct{}) bool {
	select {
	case <-budget:
		return false
	default:
	}
	select {
	case <-logged:
		return true
	case <-budget:
		return false
	}
}

func (s *warningSink) beginClose() bool {
	if s.closed {
		return false
	}
	s.closed = true
	s.pump(false)
	return true
}

func (s *warningSink) waitIdle(budget <-chan struct{}) {
	select {
	case <-ask(s, s.idleSignal):
	case <-budget:
	}
}

func (s *warningSink) seal() bilan {
	for _, entry := range s.queue {
		finish(entry, errWarningAbandoned)
	}
	s.dropped += len(s.queue)
	s.queue = nil
	s.queuedBytes = 0
	taken := bilan{lost: s.owed()}
	if s.inflight != nil {
		taken.unconfirmed = 1
	}
	s.reported = s.dropped
	s.sealed = true
	return taken
}

func (s *warningSink) announce(text string) error {
	budget, stop := writeBudget()
	defer stop()
	return s.submit([]byte(text+"\n"), true)(budget)
}

func settled(entry *warning) bool {
	select {
	case <-entry.done:
		return true
	default:
		return false
	}
}

func finish(entry *warning, err error) {
	entry.err = err
	close(entry.done)
}

func (s *warningSink) logNotConfirmed(lost, unconfirmed int) {
	if lost > 0 || unconfirmed > 0 {
		s.log.reserve(func() {
			slog.Warn("warnings were not all confirmed when the sink closed", "lost", lost, "unconfirmed", unconfirmed)
		})
	}
}

func writeBudget() (<-chan struct{}, func()) {
	return budgetAfter(warningWriteBudget)
}

func budgetAfter(duration time.Duration) (<-chan struct{}, func()) {
	expired := make(chan struct{})
	timer := time.AfterFunc(duration, func() { close(expired) })
	return expired, func() { timer.Stop() }
}
