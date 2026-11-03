package state

import "github.com/Herrscherd/herrscher/core/internal/schedule"

// SnapshotSchedules returns a copy of the schedules. A copy, not the slice: the
// firing loop iterates it outside the mutex, and lending it the live array
// would race a read against every write.
func (s *State) SnapshotSchedules() []schedule.Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]schedule.Schedule(nil), s.Schedules...)
}

// PutSchedule adds or replaces a schedule, by name.
func (s *State) PutSchedule(sched schedule.Schedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Schedules {
		if s.Schedules[idx].Name == sched.Name {
			s.Schedules[idx] = sched
			return s.saveLocked()
		}
	}
	s.Schedules = append(s.Schedules, sched)
	return s.saveLocked()
}

// RemoveSchedule drops a schedule and reports whether it was there.
func (s *State) RemoveSchedule(name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Schedules {
		if s.Schedules[idx].Name == name {
			s.Schedules = append(s.Schedules[:idx], s.Schedules[idx+1:]...)
			return true, s.saveLocked()
		}
	}
	return false, nil
}

// SetSchedulePaused suspends or resumes a schedule and reports whether it was
// there.
func (s *State) SetSchedulePaused(name string, paused bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Schedules {
		if s.Schedules[idx].Name == name {
			s.Schedules[idx].Paused = paused
			return true, s.saveLocked()
		}
	}
	return false, nil
}

// StampScheduleRun advances LastRun. An unknown name is not an error: the
// schedule may have been removed between the tick and the end of the turn, and
// the firing loop has nothing to hold against an operator who changed their
// mind.
func (s *State) StampScheduleRun(name, stampedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Schedules {
		if s.Schedules[idx].Name == name {
			s.Schedules[idx].LastRun = stampedAt
			return s.saveLocked()
		}
	}
	return nil
}
