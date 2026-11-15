package supervisor

import (
	"context"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// StartWithin is Start for a caller holding a lock it cannot keep past grace: a
// loop of the same name still stopping after grace yields ErrStillStopping and
// nothing is launched.
func (s *Supervisor) StartWithin(sess state.Session, grace time.Duration) error {
	return s.startUntil(sess, time.After(grace))
}

func (s *Supervisor) startUntil(sess state.Session, expired <-chan time.Time) error {
	if !s.startWithin(s.ctx, sess, expired) {
		return ErrStillStopping
	}
	return nil
}

func (s *Supervisor) startWithin(ctx context.Context, sess state.Session, expired <-chan time.Time) bool {
	for {
		s.mu.Lock()
		run, running := s.runs[sess.Name]
		switch {
		case !running:
			cctx, cancel := context.WithCancel(ctx)
			run = &supervisedRun{cancel: cancel, done: make(chan struct{})}
			s.runs[sess.Name] = run
			s.mu.Unlock()
			go func() {
				defer close(run.done)
				s.run(cctx, sess)
			}()
			return true
		case !run.stopping:
			s.mu.Unlock()
			return true
		default:
			s.mu.Unlock()
			select {
			case <-run.done:
			case <-expired:
				return false
			}
			s.mu.Lock()
			if s.runs[sess.Name] == run {
				delete(s.runs, sess.Name)
			}
			s.mu.Unlock()
		}
	}
}

// StopWithin is Stop for a caller holding a lock it cannot keep past grace: a
// loop still running after grace yields ErrStillStopping, and it keeps stopping
// in the background.
func (s *Supervisor) StopWithin(name string, grace time.Duration) error {
	return s.stopUntil(name, time.After(grace))
}

func (s *Supervisor) stopUntil(name string, expired <-chan time.Time) error {
	s.mu.Lock()
	run, ok := s.runs[name]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	run.stopping = true
	run.cancel()
	s.mu.Unlock()

	select {
	case <-run.done:
	case <-expired:
		return ErrStillStopping
	}

	s.mu.Lock()
	if s.runs[name] == run {
		delete(s.runs, name)
	}
	s.mu.Unlock()
	return nil
}

// RestartWithin is Restart for a caller holding a lock it cannot keep past grace:
// the stop and the launch share one grace, and a loop still stopping when it runs
// out yields ErrStillStopping with nothing launched.
func (s *Supervisor) RestartWithin(sess state.Session, grace time.Duration) error {
	expired := time.After(grace)
	if err := s.stopUntil(sess.Name, expired); err != nil {
		return err
	}
	return s.startUntil(sess, expired)
}
