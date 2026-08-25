package host

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	// schedulerTick is the loop's grain. Cron is minute-grained, so looking more
	// often would discover nothing more.
	schedulerTick = time.Minute
	// fireTimeout bounds one firing. It covers opening the session and handing
	// over the task, not the turn itself: the turn then lives its own life in the
	// session's FIFO, with the budget gate for a ceiling.
	fireTimeout = 5 * time.Minute
)

// Small ports the scheduler depends on, each satisfied by an existing host
// component (*state.State, *hub). Kept tiny so the loop is testable with no git,
// no session and no real clock, the way coordinator.go already is.
type scheduleStore interface {
	SnapshotSchedules() []schedule.Schedule
	StampScheduleRun(name, at string) error
}

type scheduleSessions interface {
	SnapshotSessions() []state.Session
}

type scheduleCreator interface {
	Create(context.Context, contracts.CreateSession) (string, error)
}

// scheduleRunner is what `schedule run` needs to reach: a live loop. Only the
// daemon has one, so the verb takes it through a slot it may find empty.
type scheduleRunner interface {
	fireNow(ctx context.Context, name string) error
}

// scheduler wakes sessions on a cadence. It knows how to do nothing else: it
// resolves a target and hands over a task through the two seams that already
// existed. The turn that follows enters the same FIFO a human message does, so
// it crosses the budget gate, memory, skills and the fan-out to the gateways
// without a line of dedicated code.
type scheduler struct {
	store    scheduleStore
	sessions scheduleSessions
	creator  scheduleCreator
	seed     func(session, task string) bool
	now      func() time.Time

	mu       sync.Mutex
	inflight map[string]bool
	wg       sync.WaitGroup
}

func newScheduler(store scheduleStore, sessions scheduleSessions, creator scheduleCreator,
	seed func(string, string) bool, now func() time.Time) *scheduler {
	return &scheduler{
		store:    store,
		sessions: sessions,
		creator:  creator,
		seed:     seed,
		now:      now,
		inflight: map[string]bool{},
	}
}

// Run catches up on what was missed while the daemon was down, then ticks until
// its context is cancelled.
//
// The catch-up waits for the first tick rather than running straight away. The
// sessions restored at boot register their driver from a goroutine of their own,
// so a task handed over in the same breath as `serve` starts would find no
// driver to take it and the missed window would be dropped for good. A minute
// costs nothing on a turn that is late by definition.
func (s *scheduler) Run(ctx context.Context) {
	t := time.NewTicker(schedulerTick)
	defer t.Stop()
	caughtUp := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !caughtUp {
				caughtUp = true
				s.catchUp(ctx)
				continue
			}
			s.tick(ctx)
		}
	}
}

// claim takes a schedule's token, or says no if it is already taken. This is
// the invariant: a schedule never has two turns in flight. A cadence faster
// than a turn therefore degrades to "as often as it can" instead of building a
// bottomless queue.
func (s *scheduler) claim(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[name] {
		return false
	}
	s.inflight[name] = true
	return true
}

func (s *scheduler) release(name string) {
	s.mu.Lock()
	delete(s.inflight, name)
	s.mu.Unlock()
}

func (s *scheduler) busy(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight[name]
}

// wait blocks until the firings in flight have returned. Only tests call it:
// the loop itself never wants to wait on a firing.
func (s *scheduler) wait() { s.wg.Wait() }

func (s *scheduler) tick(ctx context.Context) {
	now := s.now()
	for _, sc := range s.store.SnapshotSchedules() {
		// A schedule with a turn in flight is left entirely alone, staleness
		// included: its window is being served right now, and calling it
		// perished would move the anchor out from under the firing.
		if sc.Paused || s.busy(sc.Name) {
			continue
		}
		// Due comes before Stale, which asks it again: this is the branch every
		// schedule takes on almost every tick, and answering it costs a walk
		// through the cadence.
		if !schedule.Due(sc, now) {
			continue
		}
		if schedule.Stale(sc, now) {
			s.realign(sc, now)
			continue
		}
		s.dispatch(ctx, sc, "", stampFor(sc, now))
	}
}

// stampFor is the instant a firing must record as its LastRun: the window being
// served, not the moment the loop noticed it or finished handing it over.
// Falling back on now is the answer for a schedule whose window cannot be named
// any more, which the callers have already ruled out.
func stampFor(sc schedule.Schedule, now time.Time) time.Time {
	if w, ok := schedule.Window(sc, now); ok {
		return w
	}
	return now
}

// realign forgets a window that passed longer ago than its grace and restarts
// the cadence from now. It is said out loud rather than done quietly: a turn the
// operator was expecting is not happening, and that is the kind of thing you
// want to read in the log rather than deduce from a silence.
func (s *scheduler) realign(sc schedule.Schedule, now time.Time) {
	slog.Warn("schedule: window passed longer ago than its grace, skipped",
		"schedule", sc.Name, "grace", sc.Grace)
	if err := s.store.StampScheduleRun(sc.Name, now.UTC().Format(time.RFC3339)); err != nil {
		slog.Warn("schedule: could not restart the cadence", "schedule", sc.Name, "err", err)
	}
}

func (s *scheduler) catchUp(ctx context.Context) {
	now := s.now()
	for _, sc := range s.store.SnapshotSchedules() {
		// A cadence that no longer parses would simply never fire again. It is
		// said here, once at boot, rather than every minute: state.json can be
		// edited by hand, and a schedule that says nothing looks exactly like a
		// schedule with nothing to do yet.
		if _, err := schedule.Next(sc, now); err != nil {
			slog.Warn("schedule: unusable cadence, it will never fire", "schedule", sc.Name, "err", err)
			continue
		}
		fire, late := schedule.CatchUp(sc, now)
		if !fire {
			continue
		}
		s.dispatch(ctx, sc, latePrefix(late), now.Add(-late))
	}
}

// fireNow fires out of band, for the operator who wants to check a task before
// letting it run on its own. It does not stamp LastRun: a try by hand must not
// shift the schedule's cadence. A paused schedule fires too, because a pause
// stops the cadence and not the operator's hand.
//
// Unlike a window, it fires inline and reports what happened. Someone is waiting
// on the answer, and "fired" printed over a session that was never reached is
// worse than no verb at all.
func (s *scheduler) fireNow(ctx context.Context, name string) error {
	for _, sc := range s.store.SnapshotSchedules() {
		if sc.Name != name {
			continue
		}
		if !s.claim(sc.Name) {
			return fmt.Errorf("schedule %q already has a turn in flight", name)
		}
		defer s.release(sc.Name)
		fctx, cancel := context.WithTimeout(ctx, fireTimeout)
		defer cancel()
		if !s.fire(fctx, sc, "") {
			return fmt.Errorf("schedule %q: no session took the task, see the daemon log", name)
		}
		return nil
	}
	return fmt.Errorf("no schedule named %q", name)
}

// dispatch launches a window's firing in the background, if the schedule's token
// is free. In the background so one slow firing does not delay the others' tick.
// The stamp lands inside the goroutine, before the token is released, so the
// next tick can never see a delivered window still due.
//
// at is the window served, and it is passed in rather than read from the clock
// on the way out: a firing takes time, and stamping the moment it ended would
// push every following window a tick further out.
func (s *scheduler) dispatch(ctx context.Context, sc schedule.Schedule, prefix string, at time.Time) {
	if !s.claim(sc.Name) {
		slog.Debug("schedule: previous turn still in flight, skipping this window", "schedule", sc.Name)
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.release(sc.Name)
		fctx, cancel := context.WithTimeout(ctx, fireTimeout)
		defer cancel()
		if !s.fire(fctx, sc, prefix) {
			return
		}
		if err := s.store.StampScheduleRun(sc.Name, at.UTC().Format(time.RFC3339)); err != nil {
			slog.Warn("schedule: could not record the run", "schedule", sc.Name, "err", err)
		}
	}()
}

// fire resolves the target and hands over the task. It reports whether the task
// actually reached a session, which alone decides if LastRun advances: a window
// that delivered nothing must be retried, not recorded as done.
func (s *scheduler) fire(ctx context.Context, sc schedule.Schedule, prefix string) bool {
	name, ok := s.target(ctx, sc)
	if !ok {
		return false
	}
	if !s.seed(name, prefix+sc.Task) {
		slog.Warn("schedule: no live session to hand the task to", "schedule", sc.Name, "session", name)
		return false
	}
	return true
}

// target resolves the session a schedule wakes. The two shapes of target only
// diverge when the session is not there.
func (s *scheduler) target(ctx context.Context, sc schedule.Schedule) (string, bool) {
	if sc.Session != "" {
		// The operator named a precise session. Manufacturing one in its place
		// would be a surprise, so an absent target is a skipped window.
		if s.live(sc.Session) {
			return sc.Session, true
		}
		slog.Warn("schedule: the target session is not there", "schedule", sc.Name, "session", sc.Session)
		return "", false
	}
	// Agent target: the schedule owns one session, named after it and therefore
	// stable from one tick to the next. It is created once and reused after, so a
	// daily schedule does not leave 365 worktrees behind it.
	want := schedule.SessionName(sc)
	if s.live(want) {
		return want, true
	}
	if _, err := s.creator.Create(ctx, contracts.CreateSession{
		Name:    want,
		Agent:   sc.Agent,
		Project: sc.Project,
	}); err != nil {
		slog.Warn("schedule: could not open the session", "schedule", sc.Name, "session", want, "err", err)
		return "", false
	}
	return want, true
}

func (s *scheduler) live(name string) bool {
	for _, sess := range s.sessions.SnapshotSessions() {
		if sess.Name == name && !sess.Archived {
			return true
		}
	}
	return false
}

// latePrefix tells the model this turn arrives after its window, and by how
// much. Without that line, a reply written at 09:45 would claim to be the 09:00
// one.
func latePrefix(late time.Duration) string {
	if late <= 0 {
		return ""
	}
	return fmt.Sprintf("[This scheduled turn is %s late: its window passed while the daemon was down.]\n\n", late.Round(time.Minute))
}
