package host

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"
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
	summary bool
	stalled bool
}

type sinkQuiet struct {
	idle    <-chan struct{}
	stalled bool
}

type warningSink struct {
	out         io.Writer
	lostLine    func(lost int) string
	commands    chan func()
	running     atomic.Bool
	queue       []*warning
	queuedBytes int
	inflight    *inflightWrite
	idle        []chan struct{}
	dropped     int
	reported    int
	closed      bool
	sealed      bool
}

func newWarningSink(out io.Writer, lostLine func(lost int) string) *warningSink {
	return &warningSink{
		out:      warningWriter(out),
		lostLine: lostLine,
		commands: make(chan func(), warningCommandLimit),
	}
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

func (s *warningSink) post(task func()) {
	s.dispatch(task)
}

func (s *warningSink) call(task func()) {
	ask(s, func() struct{} {
		task()
		return struct{}{}
	})
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
	if err := ask(s, func() error { return s.admit(entry) }); err != nil {
		return func(<-chan struct{}) error { return err }
	}
	return func(budget <-chan struct{}) error { return s.await(entry, budget) }
}

func (s *warningSink) await(entry *warning, budget <-chan struct{}) error {
	select {
	case <-entry.done:
		return entry.err
	case <-budget:
	}
	return ask(s, func() error { return s.giveUp(entry) })
}

func (s *warningSink) admit(entry *warning) error {
	entry.direct = entry.direct || s.closed
	if len(entry.data) > warningEntryBytes || len(s.queue) >= warningQueueLimit || s.queuedBytes+len(entry.data) > warningQueueBytes {
		s.lose()
		return errWarningAbandoned
	}
	if s.stalledInflight() {
		if entry.direct {
			s.lose()
			return errWarningAbandoned
		}
		s.enqueue(entry)
		return errWarningLate
	}
	s.enqueue(entry)
	s.pump(false)
	return nil
}

func (s *warningSink) enqueue(entry *warning) {
	s.queue = append(s.queue, entry)
	s.queuedBytes += len(entry.data)
}

func (s *warningSink) giveUp(entry *warning) error {
	if settled(entry) {
		return entry.err
	}
	if s.isInflight(entry) {
		if !entry.direct {
			s.markStalled()
		}
		return errWarningLate
	}
	if !entry.direct {
		s.markStalled()
		return errWarningLate
	}
	s.removeQueued(entry)
	s.lose()
	finish(entry, errWarningAbandoned)
	s.pump(false)
	return errWarningAbandoned
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

func (s *warningSink) lose() {
	s.dropped++
	if s.sealed {
		s.announceDelta()
	}
}

func (s *warningSink) announceDelta() {
	lost := s.owed()
	s.reported = s.dropped
	logNotConfirmed(lost, 0)
}

func (s *warningSink) pump(summarized bool) {
	if s.inflight != nil {
		return
	}
	if !summarized && s.owed() > 0 && (len(s.queue) > 0 || s.closed) {
		lost := s.owed()
		s.start(&inflightWrite{lost: lost, summary: true}, []byte(s.lostLine(lost)+"\n"))
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

func (s *warningSink) start(write *inflightWrite, data []byte) {
	s.inflight = write
	go func() {
		written, err := s.out.Write(data)
		logWriteFailure(written, err)
		s.post(func() { s.settle(write, err) })
	}()
}

func (s *warningSink) settle(write *inflightWrite, err error) {
	s.inflight = nil
	if write.summary {
		if err == nil && !s.sealed {
			s.reported += write.lost
		}
		s.pump(true)
		return
	}
	finish(write.entry, err)
	if err != nil {
		s.lose()
	}
	s.pump(false)
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
		return
	}
	budget, stop := writeBudget()
	defer stop()
	s.waitIdle(budget)
	s.call(s.seal)
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

func (s *warningSink) seal() {
	for _, entry := range s.queue {
		finish(entry, errWarningAbandoned)
	}
	s.dropped += len(s.queue)
	s.queue = nil
	s.queuedBytes = 0
	unconfirmed := 0
	if s.inflight != nil {
		unconfirmed = 1
	}
	lost := s.owed()
	s.reported = s.dropped
	s.sealed = true
	logNotConfirmed(lost, unconfirmed)
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

func logNotConfirmed(lost, unconfirmed int) {
	if lost > 0 || unconfirmed > 0 {
		slog.Warn("warnings were not all confirmed when the sink closed", "lost", lost, "unconfirmed", unconfirmed)
	}
}

func writeBudget() (<-chan struct{}, func()) {
	expired := make(chan struct{})
	timer := time.AfterFunc(warningWriteBudget, func() { close(expired) })
	return expired, func() { timer.Stop() }
}
