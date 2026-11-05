package host

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
)

const (
	schedulerTick = time.Minute
	fireTimeout   = 5 * time.Minute
)

type scheduleStore interface {
	SnapshotSchedules() []schedule.Schedule
	StampScheduleRun(name, at string) error
}

type scheduleCreator interface {
	Create(context.Context, contracts.CreateSession) (contracts.Result, error)
}

type scheduleRunner interface {
	fireNow(ctx context.Context, name string) error
}

type scheduler struct {
	store    scheduleStore
	sessions sessionLister
	creator  scheduleCreator
	seed     func(session, task string) bool
	now      func() time.Time

	mu       sync.Mutex
	inflight map[string]bool
	wg       sync.WaitGroup
}

func newScheduler(store scheduleStore, sessions sessionLister, creator scheduleCreator,
	seed func(string, string) bool, now func() time.Time,
) *scheduler {
	return &scheduler{
		store:    store,
		sessions: sessions,
		creator:  creator,
		seed:     seed,
		now:      now,
		inflight: make(map[string]bool),
	}
}

func (s *scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(schedulerTick)
	defer ticker.Stop()
	caughtUp := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
	schedules := s.store.SnapshotSchedules()
	for idx := range schedules {
		definition := schedules[idx]
		if definition.Paused || s.busy(definition.Name) {
			continue
		}
		if !schedule.Due(definition, now) {
			continue
		}
		if schedule.Stale(definition, now) {
			s.realign(definition, now)
			continue
		}
		s.dispatch(ctx, definition, "", stampFor(definition, now))
	}
}

func stampFor(sc schedule.Schedule, now time.Time) time.Time {
	if w, ok := schedule.Window(sc, now); ok {
		return w
	}
	return now
}

func (s *scheduler) realign(definition schedule.Schedule, now time.Time) {
	slog.Warn("schedule: window passed longer ago than its grace, skipped",
		keySchedule, definition.Name, "grace", definition.Grace)
	if err := s.store.StampScheduleRun(definition.Name, now.UTC().Format(time.RFC3339)); err != nil {
		slog.Warn("schedule: could not restart the cadence", keySchedule, definition.Name, "err", err)
	}
}

func (s *scheduler) catchUp(ctx context.Context) {
	now := s.now()
	schedules := s.store.SnapshotSchedules()
	for idx := range schedules {
		definition := schedules[idx]
		if _, err := schedule.Next(definition, now); err != nil {
			slog.Warn("schedule: unusable cadence, it will never fire", keySchedule, definition.Name, "err", err)
			continue
		}
		fire, late := schedule.CatchUp(definition, now)
		if !fire {
			continue
		}
		s.dispatch(ctx, definition, latePrefix(late), now.Add(-late))
	}
}

func (s *scheduler) fireNow(ctx context.Context, name string) error {
	schedules := s.store.SnapshotSchedules()
	for idx := range schedules {
		definition := schedules[idx]
		if definition.Name != name {
			continue
		}
		return s.fireScheduled(ctx, definition)
	}
	return &NotFoundError{Kind: kindSchedule, Name: name}
}

func (s *scheduler) fireScheduled(ctx context.Context, definition schedule.Schedule) error {
	if !s.claim(definition.Name) {
		return &scheduleBusyError{Name: definition.Name}
	}
	defer s.release(definition.Name)
	fctx, cancel := context.WithTimeout(ctx, fireTimeout)
	defer cancel()
	if !s.fire(fctx, definition, "") {
		return &taskNotTakenError{Schedule: definition.Name}
	}
	return nil
}

func (s *scheduler) dispatch(ctx context.Context, definition schedule.Schedule, prefix string, when time.Time) {
	if !s.claim(definition.Name) {
		slog.Debug("schedule: previous turn still in flight, skipping this window", keySchedule, definition.Name)
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.release(definition.Name)
		fctx, cancel := context.WithTimeout(ctx, fireTimeout)
		defer cancel()
		if !s.fire(fctx, definition, prefix) {
			return
		}
		if err := s.store.StampScheduleRun(definition.Name, when.UTC().Format(time.RFC3339)); err != nil {
			slog.Warn("schedule: could not record the run", keySchedule, definition.Name, "err", err)
		}
	}()
}

func (s *scheduler) fire(ctx context.Context, definition schedule.Schedule, prefix string) bool {
	name, ok := s.target(ctx, definition)
	if !ok {
		return false
	}
	if !s.seed(name, prefix+definition.Task) {
		slog.Warn("schedule: no live session to hand the task to", keySchedule, definition.Name, keySession, name)
		return false
	}
	return true
}

func (s *scheduler) target(ctx context.Context, definition schedule.Schedule) (string, bool) {
	if definition.Session != "" {
		if s.live(definition.Session) {
			return definition.Session, true
		}
		slog.Warn("schedule: the target session is not there", keySchedule, definition.Name, keySession, definition.Session)
		return "", false
	}
	want := schedule.SessionName(definition)
	if s.live(want) {
		return want, true
	}
	if _, err := s.creator.Create(ctx, contracts.CreateSession{
		Name:    want,
		Agent:   definition.Agent,
		Project: definition.Project,
	}); err != nil {
		slog.Warn("schedule: could not open the session", keySchedule, definition.Name, keySession, want, "err", err)
		return "", false
	}
	return want, true
}

func (s *scheduler) live(name string) bool {
	sessions := s.sessions.SnapshotSessions()
	for idx := range sessions {
		sess := sessions[idx]
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
