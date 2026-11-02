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
	schedulerTick = time.Minute
	fireTimeout   = 5 * time.Minute
)

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

type scheduleRunner interface {
	fireNow(ctx context.Context, name string) error
}

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
	seed func(string, string) bool, now func() time.Time,
) *scheduler {
	return &scheduler{
		store:    store,
		sessions: sessions,
		creator:  creator,
		seed:     seed,
		now:      now,
		inflight: map[string]bool{},
	}
}

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

func (s *scheduler) wait() { s.wg.Wait() }

func (s *scheduler) tick(ctx context.Context) {
	now := s.now()
	for _, sc := range s.store.SnapshotSchedules() {
		if sc.Paused || s.busy(sc.Name) {
			continue
		}
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

func stampFor(sc schedule.Schedule, now time.Time) time.Time {
	if w, ok := schedule.Window(sc, now); ok {
		return w
	}
	return now
}

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

func (s *scheduler) fireNow(ctx context.Context, name string) error {
	for _, sc := range s.store.SnapshotSchedules() {
		if sc.Name != name {
			continue
		}
		if !s.claim(sc.Name) {
			return &scheduleBusyError{Name: name}
		}
		defer s.release(sc.Name)
		fctx, cancel := context.WithTimeout(ctx, fireTimeout)
		defer cancel()
		if !s.fire(fctx, sc, "") {
			return &taskNotTakenError{Schedule: name}
		}
		return nil
	}
	return &NotFoundError{Kind: "schedule", Name: name}
}

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

func (s *scheduler) target(ctx context.Context, sc schedule.Schedule) (string, bool) {
	if sc.Session != "" {
		if s.live(sc.Session) {
			return sc.Session, true
		}
		slog.Warn("schedule: the target session is not there", "schedule", sc.Name, "session", sc.Session)
		return "", false
	}
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

func latePrefix(late time.Duration) string {
	if late <= 0 {
		return ""
	}
	return fmt.Sprintf("[This scheduled turn is %s late: its window passed while the daemon was down.]\n\n", late.Round(time.Minute))
}
