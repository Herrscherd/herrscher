package host

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type lockedScheduleStore struct {
	mu   sync.Mutex
	rows []schedule.Schedule
}

func (s *lockedScheduleStore) SnapshotSchedules() []schedule.Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]schedule.Schedule(nil), s.rows...)
}

func (s *lockedScheduleStore) StampScheduleRun(name, at string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.rows {
		if s.rows[idx].Name == name {
			s.rows[idx].LastRun = at
		}
	}
	return nil
}

type movingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movingClock) Set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

type lockedSeedLog struct {
	mu    sync.Mutex
	seeds []string
}

func (l *lockedSeedLog) seed(session, task string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seeds = append(l.seeds, session+"|"+task)
	return true
}

func (l *lockedSeedLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.seeds)
}

func TestRunFiresOnceWhenTheInjectedClockPassesTheWindow(t *testing.T) {
	store := &lockedScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Cron: "0 9 * * *", Task: "write the digest",
		LastRun: mustStamp(t, "2026-08-24 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	clock := &movingClock{now: mkHostTime(t, "2026-08-25 08:59")}
	var fired lockedSeedLog
	sch := newScheduler(store, sessions, &fakeScheduleCreator{}, fired.seed, clock.Now, time.Millisecond)

	runCtx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { sch.Run(runCtx); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
		sch.wait()
	})

	time.Sleep(20 * time.Millisecond)
	if fired.count() != 0 {
		t.Fatal("the schedule fired before its window")
	}
	clock.Set(mkHostTime(t, "2026-08-25 09:00"))
	deadline := time.Now().Add(5 * time.Second)
	for fired.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fired.count() == 0 {
		t.Fatal("the schedule did not fire once the injected clock passed its window")
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	sch.wait()
	if got := fired.count(); got != 1 {
		t.Fatalf("fired %d times after the injected clock passed its window once, want 1", got)
	}
}
